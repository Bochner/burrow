"""Throwaway local terminal probe using only Hovel's public Unix-socket HTTP API."""
import base64
import http.client
import json
import os
import select
import signal
import socket
import sys
import termios
import tty

socket_path, session = sys.argv[1:]


def rpc(method, payload):
    connection = http.client.HTTPConnection("localhost", timeout=3)
    connection.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    connection.sock.settimeout(3)
    try:
        connection.sock.connect(socket_path)
        connection.request("POST", "/hovel.daemon.v1.DaemonService/" + method,
                           json.dumps(payload), {"Content-Type": "application/json"})
        response = connection.getresponse()
        body = response.read(1024 * 1024 + 1)
        if response.status != 200 or len(body) > 1024 * 1024:
            raise RuntimeError(f"daemon response rejected: HTTP {response.status}")
        return json.loads(body)
    finally:
        connection.close()


assert any(s["ID"] == session for s in rpc("ListSessions", {}).get("Sessions", []))
with open("/dev/tty", "r+b", buffering=0) as terminal:
    fd = terminal.fileno()
    state = termios.tcgetattr(fd)
    dirty = True
    last = "Ready"
    reason = "detached"

    def resize(signum, frame):
        global dirty
        dirty = True

    signal.signal(signal.SIGWINCH, resize)
    try:
        tty.setraw(fd)
        terminal.write(b"\x1b[?1049h\x1b[?25l")
        while True:
            if dirty:
                size = os.get_terminal_size(fd)
                # Escape all daemon-controlled bytes; this probes management UI, not shell emulation.
                screen = (f"\x1b[H\x1b[2JBurrow local frontend PROTOTYPE {size.columns}x{size.lines}\r\n"
                          f"Hovel session: {json.dumps(session)}\r\n"
                          "x: send inert input | Ctrl-]: detach | Ctrl-C: interrupt UI | q: close session\r\n"
                          f"{last}\r\n")
                terminal.write(screen.encode())
                dirty = False
            if select.select([fd], [], [], 0.05)[0]:
                key = os.read(fd, 1)
                if key in (b"", b"\x1d", b"\x03"):
                    reason = "interrupted" if key == b"\x03" else "detached"
                    break
                if key == b"q":
                    rpc("CloseSession", {"SessionID": session})
                    reason = "closed"
                    break
                if key == b"x":
                    rpc("WriteSession", {"SessionID": session, "Data": base64.b64encode(key).decode()})
            chunk = rpc("ReadSession", {"SessionID": session, "TimeoutMs": 20})
            if chunk.get("Data"):
                data = base64.b64decode(chunk["Data"], validate=True)
                # ponytail: show only last 256 bytes; a real UI needs an explicitly bounded transcript.
                last = "Received: " + json.dumps(data[-256:].decode(errors="replace"))
                dirty = True
            if chunk.get("Closed"):
                reason = "session ended"
                break
    finally:
        try:
            terminal.write(b"\x1b[?25h\x1b[?1049l")
        finally:
            termios.tcsetattr(fd, termios.TCSADRAIN, state)
    terminal.write(f"Frontend {reason}\r\n".encode())
