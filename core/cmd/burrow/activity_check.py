"""Workspace activity through the real follower and pinned Hovel daemon."""
import json
import fcntl
import http.client
import os
from pathlib import Path
import select
import signal
import socket
import struct
import subprocess
import sys
import tempfile
import time
import pty
import termios

binary, wheel, decoder = [str(Path(p).resolve()) for p in sys.argv[1:]]
with tempfile.TemporaryDirectory(prefix="ba-") as scratch:
    root = Path(scratch)
    env = dict(os.environ, HOME=scratch, XDG_CACHE_HOME=str(root / "cache"),
               XDG_DATA_HOME=str(root / "data"), XDG_CONFIG_HOME=str(root / "config"))
    workspace = root / "w"
    viewers = []
    daemon = None

    def rpc(method, value):
        client = http.client.HTTPConnection("localhost", timeout=10)
        client.sock = socket.socket(socket.AF_UNIX)
        client.sock.connect(str(workspace / "hoveld.sock"))
        try:
            client.request("POST", "/hovel.daemon.v1.DaemonService/" + method,
                           json.dumps(value), {"Content-Type": "application/json"})
            response = client.getresponse()
            data = response.read()
            assert response.status == 200, (method, response.status, data)
            return json.loads(data)
        finally:
            client.close()

    def cli(*args):
        p = subprocess.run([binary, "--workspace", str(workspace), *args],
                           env=env, capture_output=True, text=True, timeout=40)
        assert p.returncode == 0, (args, p.stdout, p.stderr)
        return json.loads(p.stdout)

    def follower():
        p = subprocess.Popen([binary, "--workspace", str(workspace), "follow", "--json"],
                             env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        view = {"process": p, "pending": b"", "events": []}
        viewers.append(view)
        return view

    def wait(view, predicate, timeout=20):
        p = view["process"]
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            for event in view["events"]:
                if predicate(event):
                    return event
            if select.select([p.stdout], [], [], .1)[0]:
                chunk = os.read(p.stdout.fileno(), 65536)
                assert chunk, (p.poll(), p.stderr.read().decode())
                view["pending"] += chunk
                lines = view["pending"].split(b"\n")
                view["pending"] = lines.pop()
                view["events"].extend(json.loads(line) for line in lines)
        raise AssertionError(view["events"])

    try:
        missing = subprocess.run([binary, "--workspace", str(workspace), "follow", "--json"],
                                 env=env, capture_output=True, timeout=10)
        assert missing.returncode != 0 and not list(root.iterdir())
        daemon = cli("--hovel-package", wheel, "workspace", "open")["pid"]
        a, b = follower(), follower()
        for view in (a, b):
            wait(view, lambda e: e["kind"] == "ready")
        cli("profile", "create", "watched", "192.0.2.10", "--user", "alice")
        for view in (a, b):
            event = wait(view, lambda e: "profile create watched" in e.get("message", ""))
            assert event["workspacePath"] == str(workspace) and event["source"]
        a["process"].send_signal(signal.SIGINT)
        assert a["process"].wait(timeout=5) == 0
        assert cli("workspace", "inspect")["pid"] == daemon
        cli("profile", "create", "still-watching", "192.0.2.11", "--user", "alice")
        wait(b, lambda e: "profile create still-watching" in e.get("message", ""))
        # Read real operation evidence, including unavailable/partial storage.
        refused = subprocess.run([binary, "--workspace", str(workspace), "run", "launch", "missing", "--yes"],
                                 env=env, capture_output=True, timeout=20)
        assert refused.returncode != 0
        wait(b, lambda e: e["kind"] == "failed" and e["source"] == "burrow/audit")
        notes = workspace / "burrow-logs" / "operations.log"
        original = notes.read_bytes()
        saved = notes.with_suffix(".saved")
        notes.rename(saved)
        wait(b, lambda e: e["kind"] == "gap" and e["source"] == "burrow/audit")
        saved.rename(notes)
        wait(b, lambda e: e["kind"] == "resumed" and e["source"] == "burrow/audit")
        with notes.open("ab") as out:
            out.write(b"partial record")
        c = follower()
        wait(c, lambda e: e["kind"] == "gap" and "incomplete" in e["message"])
        notes.write_bytes(original)
        wait(c, lambda e: e["kind"] == "resumed" and e["source"] == "burrow/audit")
        c["process"].send_signal(signal.SIGINT)
        assert c["process"].wait(timeout=5) == 0
        # A blocked consumer must still be able to quit the viewer.
        stalled = follower()
        wait(stalled, lambda e: e["kind"] == "ready")
        rpc("AppendLog", {"Operation": "burrow", "Chain": "profiles", "Entries": [
            {"Source": "backpressure", "Message": "x" * 4096} for _ in range(64)]})
        time.sleep(.5)
        stalled["process"].send_signal(signal.SIGINT)
        assert stalled["process"].wait(timeout=5) == 0
        # Pause only the observer, then overflow the real Hovel log broker.
        b["process"].send_signal(signal.SIGSTOP)
        try:
            for batch in range(9):
                rpc("AppendLog", {"Operation": "burrow", "Chain": "profiles", "Entries": [
                    {"Source": "flood", "Kind": "event", "Message": "retained-tail-" + str(batch),
                     "Fields": {"detail": "x" * 300}} for _ in range(512)]})
        finally:
            b["process"].send_signal(signal.SIGCONT)
        wait(b, lambda e: e["kind"] == "gap" and "no longer available" in e["message"])
        wait(b, lambda e: e["message"] == "retained-tail-8")
        # A stopped daemon resumes under the same identity and cursors.
        b["events"].clear()
        os.kill(daemon, signal.SIGSTOP)
        try:
            wait(b, lambda e: e["kind"] == "gap" and e["source"] == "hovel")
        finally:
            os.kill(daemon, signal.SIGCONT)
        wait(b, lambda e: e["kind"] == "resumed" and e["source"] == "hovel")
        # A new daemon incarnation resets only observer cursors.
        os.kill(daemon, signal.SIGTERM)
        deadline = time.monotonic() + 10
        while (workspace / "hoveld.sock").exists() and time.monotonic() < deadline:
            time.sleep(.1)
        # Recovery is an explicit operator action in this disposable workspace;
        # the follower must never remove a stale ownership receipt itself.
        try:
            os.kill(daemon, 0)
            raise AssertionError("old daemon still exists")
        except ProcessLookupError:
            pass
        (workspace / "burrow-launch.json").unlink()
        (workspace / "burrow").rmdir()  # Must be empty: no retained resources here.
        (workspace / "burrow-launch.log").rename(workspace / "previous-launch.log")
        daemon = cli("--hovel-package", wheel, "workspace", "open")["pid"]
        wait(b, lambda e: e["kind"] == "gap" and "daemon changed" in e["message"])
        for plain in (False, True):
            for width, height in ((80, 24), (120, 30), (160, 40), (200, 50)):
                outer, slave = pty.openpty()
                before = termios.tcgetattr(slave)
                fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
                terminal_env = env | {"TERM": "xterm-256color", "COLORTERM": "truecolor", "NO_COLOR": "1" if plain else ""}
                p = subprocess.Popen([binary, "--workspace", str(workspace), "follow"], env=terminal_env,
                                     stdin=slave, stdout=slave, stderr=slave)
                output = bytearray()

                def terminal_wait(needle):
                    deadline = time.monotonic() + 10
                    while time.monotonic() < deadline:
                        if select.select([outer], [], [], .1)[0]:
                            output.extend(os.read(outer, 65536))
                        if needle in output:
                            return
                        assert p.poll() is None, output
                    raise AssertionError(output)

                try:
                    terminal_wait(b"BURROW FOLLOW")
                    if directory := os.environ.get("TEST_UNDECLARED_OUTPUTS_DIR"):
                        Path(directory, f"activity-empty-{width}x{height}-{plain}.ansi").write_bytes(output)
                    rpc("AppendLog", {"Operation": "burrow", "Chain": "profiles", "Entries": [
                        {"Source": "safety", "Kind": "event", "Level": "error", "Message": "terminal-safety\u001b]52;c;hostile\u0007",
                         "Fields": {"password": "SECRET-FEED-CANARY", "host": "192.0.2.11", "user": "alice", "port": "2222"}}]})
                    terminal_wait(b"192.0.2.11")
                    assert b"SECRET-FEED-CANARY" not in output and b"\x1b]52;c;hostile" not in output, output
                    assert b"\\u001b]52;c;hostile\\u0007" in output, output
                    assert (b"\x1b[38;2;" in output) != plain, output
                    assert not any(control in output for control in (b"?1049", b"?1000", b"?1002", b"?2004")), output
                    assert termios.tcgetattr(slave) == before
                    rows = json.loads(subprocess.run([decoder, str(width), str(height), "--cells"],
                                      input=output, capture_output=True, check=True).stdout)
                    for value, color in (("192.0.2.11", "#f5c2e7"), ("alice", "#a6e3a1"), ("2222", "#f9e2af"),
                                         ("ERROR", "#f38ba8"), ("host", "#89b4fa")):
                        matches = []
                        for row in rows:
                            line = "".join(c["text"] or " " for c in row)
                            start = line.find(value)
                            if start >= 0:
                                matches.append(row[start:start+len(value)])
                        assert matches and any(all(c["color"] == ("" if plain else color) for c in cells) for cells in matches), (value, matches)
                    if directory := os.environ.get("TEST_UNDECLARED_OUTPUTS_DIR"):
                        Path(directory, f"activity-{width}x{height}-{plain}.ansi").write_bytes(output)
                        Path(directory, f"activity-{width}x{height}-{plain}.txt").write_text(
                            "\n".join("".join(c["text"] or " " for c in row).rstrip() for row in rows))
                finally:
                    p.send_signal(signal.SIGINT)
                    assert p.wait(timeout=5) == 0
                    os.close(outer)
                    os.close(slave)
        safe_event = wait(b, lambda e: "terminal-safety" in e["message"])
        assert "SECRET-FEED-CANARY" not in json.dumps(safe_event), safe_event
        print("PASS real activity command, two independent followers, viewer-only cancellation")
    finally:
        for view in viewers:
            p = view["process"]
            if p.poll() is None:
                p.terminate()
                p.wait(timeout=10)
        if daemon:
            try:
                os.kill(daemon, signal.SIGTERM)
            except ProcessLookupError:
                pass
