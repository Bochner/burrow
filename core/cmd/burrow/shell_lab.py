"""Real SSH shell through the production command and controlling terminal seams."""
import fcntl
import os
from pathlib import Path
import pty
import select
import signal
import struct
import subprocess
import termios
import time


def shell_checks(binary, workspace, env, decoder, burrow, first, options):
    outer, slave = pty.openpty()
    before = termios.tcgetattr(slave)
    dimensions = [160, 40]
    output = bytearray()
    canary = "local-shell-canary-" + os.urandom(12).hex()

    def controlling():
        os.setsid()
        fcntl.ioctl(0, termios.TIOCSCTTY, 0)

    def send(data):
        os.write(outer, data if isinstance(data, bytes) else data.encode())

    def view():
        if select.select([outer], [], [], .05)[0]:
            output.extend(os.read(outer, 65536))
        return subprocess.run([decoder, *map(str, dimensions)], input=output,
                              capture_output=True, timeout=3, check=True).stdout.decode()

    def wait(needle):
        until = time.monotonic() + 15
        while time.monotonic() < until:
            screen = view()
            if needle in screen:
                return screen
            assert frontend.poll() is None, (frontend.returncode, screen)
        raise AssertionError((needle, screen))

    def command(text, expected):
        send(text + "\r")
        return wait(expected)

    def child():
        for task in Path(f"/proc/{frontend.pid}/task").iterdir():
            for pid in (task / "children").read_text().split():
                try:
                    if Path(f"/proc/{pid}/exe").resolve() == Path("/usr/bin/ssh").resolve():
                        return int(pid)
                except FileNotFoundError:
                    pass
        raise AssertionError("no frontend-owned SSH client")

    def same_master():
        current = burrow(workspace, "inspect", "gateway")
        assert current["state"] == "connected"
        assert (current["masterPID"], current["socketInode"]) == (first["masterPID"], first["socketInode"])

    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))
    frontend = subprocess.Popen([binary, "--workspace", str(workspace), "shell", "gateway"],
                                env=env, stdin=slave, stdout=slave, stderr=slave,
                                preexec_fn=controlling)
    try:
        wait("local / not recorded")
        command("printf 'INITIAL='; stty size", "INITIAL=35 98")
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 0, 0, 0))
        os.kill(frontend.pid, signal.SIGWINCH)
        # A zero-width outer terminal cannot render a warning; the portable
        # frame check asserts refusal text. Restore it before reading output.
        time.sleep(.1)
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))
        os.kill(frontend.pid, signal.SIGWINCH)
        command("printf 'RETAINED='; stty size", "RETAINED=35 98")
        command("printf '%s\\n' " + canary, canary)
        client = child()
        # The local SSH client is a session leader with its own controlling PTY.
        fields = Path(f"/proc/{client}/stat").read_text().rsplit(")", 1)[1].split()
        assert int(fields[3]) == client and int(fields[4]) != 0, fields
        command("sleep 60", "sleep 60")
        send(b"\x03")
        command("printf 'INTERRUPT_%s\\n' OK", "INTERRUPT_OK")
        # Child terminal controls cannot invoke clipboard/title operations outside its VT.
        command("printf '\\033]52;c;U0VDUkVU\\007SAFE_%s\\n' OUTPUT", "SAFE_OUTPUT")
        assert b"\x1b]52;" not in output
        # Raw NUL and Ctrl+C are delivered, even when the remote terminal is raw.
        command("stty raw -echo; printf 'RAW_%s' READY; dd bs=1 count=2 2>/dev/null | od -An -tx1; stty sane", "RAW_READY")
        send(b"\x00\x03")
        wait("00 03")
        command("printf '\\033[?1049h\\033[2J\\033[HREMOTE_%s' SCREEN", "REMOTE_SCREEN")
        send(b"\x1d")
        wait("ACTIVE SSH CONNECTIONS")
        assert Path(f"/proc/{client}").exists(), "background key closed shell"
        command("shell-close", "Local SSH shell closed")
        assert not Path(f"/proc/{client}").exists(), "shell client not reaped"
        assert "ACTIVE SSH CONNECTIONS" in view()
        same_master()
        command("shell gateway", "local / not recorded")
        command("exit", "Local SSH shell exited")
        same_master()
        command("shell gateway", "local / not recorded")
        client = child()
        while select.select([outer], [], [], 0)[0]:
            os.read(outer, 65536)
        output.clear()
        dimensions[:] = [120, 30]
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 120, 0, 0))
        os.kill(frontend.pid, signal.SIGWINCH)
        wait("local / not recorded")
        command("printf 'RESIZED='; stty size", "RESIZED=25 68")
        send(b"\x1bb")  # Management, with the frontend-local client still alive.
        wait("ACTIVE SSH CONNECTIONS")
        send(b"\x03")
        wait("Keep running")
        send(b"\r")
        assert frontend.wait(timeout=10) == 0
        assert not Path(f"/proc/{client}").exists(), "quit leaked shell client"
        assert termios.tcgetattr(slave) == before
        view()
        assert b"\x1b[?1049l" in output and b"\x1b[?25h" in output
        same_master()
    finally:
        if frontend.poll() is None:
            frontend.terminate()
            frontend.wait(timeout=10)
        os.close(outer)
        os.close(slave)
    # Selected connection close and transport loss unwind active remote PTYs;
    # a sibling connection and saved collection/evidence survive both paths.
    saved = (workspace / "burrow-profiles.json").read_bytes()
    for loss in ("close", "master", "frontend"):
        name = "shell-" + loss
        burrow(workspace, "connect", name, "127.0.0.1", "tester", *options)
        until = time.monotonic() + 15
        while True:
            state = burrow(workspace, "inspect", name)
            if state["state"] == "connected":
                break
            assert time.monotonic() < until, state
            time.sleep(.1)
        outer, slave = pty.openpty()
        dimensions[:] = [160, 40]
        output.clear()
        before = termios.tcgetattr(slave)
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))
        frontend = subprocess.Popen([binary, "--workspace", str(workspace), "shell", name],
                                    env=env, stdin=slave, stdout=slave, stderr=slave,
                                    preexec_fn=controlling)
        try:
            wait("local / not recorded")
            client = child()
            command("printf '\\033[?1049h\\033[2J\\033[HLOSS_%s' SCREEN", "LOSS_SCREEN")
            if loss == "frontend":
                frontend.terminate()
                frontend.wait(timeout=10)
            else:
                if loss == "close":
                    burrow(workspace, "close", name, "--yes")
                else:
                    os.kill(state["masterPID"], signal.SIGKILL)
                wait("SSH shell ended")
                assert "ACTIVE SSH CONNECTIONS" in view()
                command("shell " + name, "REFUSED")
                assert not Path(f"/proc/{client}").exists()
                send(b"\x03")
                wait("Keep running")
                send(b"\r")
                assert frontend.wait(timeout=10) == 0
            assert not Path(f"/proc/{client}").exists()
            assert termios.tcgetattr(slave) == before
            view()
            assert b"\x1b[?1049l" in output and b"\x1b[?25h" in output
            same_master()
            assert (workspace / "burrow-profiles.json").read_bytes() == saved
        finally:
            if frontend.poll() is None:
                frontend.terminate()
                frontend.wait(timeout=10)
            os.close(outer)
            os.close(slave)
            if loss != "close":
                burrow(workspace, "close", name, "--yes")
    for path in workspace.rglob("*"):
        if path.is_file():
            assert canary.encode() not in path.read_bytes(), path
    print("PASS real SSH shell input/NUL/Ctrl-C, resize, close/exit/reopen, loss, VT isolation, secret exclusion and quit/reaping/restoration", flush=True)
    # Human restart uses a real terminal, explicit confirmation and Hovel close.
    evidence = workspace / "restart-evidence.txt"
    evidence.write_text("preserve this workspace evidence\n")
    daemon = burrow(workspace, "status")["pid"]
    for reply in ("cancel", "restart", "--yes"):
        if reply == "--yes":
            burrow(workspace, "connect", "gateway", "127.0.0.1", "tester", *options)
            deadline = time.monotonic() + 15
            while burrow(workspace, "inspect", "gateway")["state"] != "connected":
                assert time.monotonic() < deadline, "restart fixture did not connect"
                time.sleep(.1)
        outer, slave = pty.openpty()
        dimensions[:] = [160, 40]
        output.clear()
        before = termios.tcgetattr(slave)
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))
        frontend = subprocess.Popen([binary, "--workspace", str(workspace), "restart", *(["--yes"] if reply == "--yes" else [])],
                                    env=env | ({"NO_COLOR": "", "COLORTERM": "truecolor"} if reply == "cancel" else {}), stdin=slave, stdout=slave, stderr=slave,
                                    preexec_fn=controlling)
        try:
            if reply != "--yes":
                wait("Type restart to confirm")
                if reply == "cancel":
                    assert b"38;2;166;227;161" in output and b"38;2;180;190;254" in output, "restart recap lost state/name colors"
                else:
                    assert b"\x1b[" not in output, "NO_COLOR restart recap emitted ANSI"
                same_master()
                send(reply + "\n")
            if reply == "cancel":
                assert frontend.wait(timeout=10) != 0
                same_master()
            else:
                wait("ACTIVE SSH CONNECTIONS")
                if reply == "--yes":
                    assert b"Type restart to confirm" not in output
                assert burrow(workspace, "connections") == []
                assert not Path(f'/proc/{first["masterPID"]}').exists()
                frontend.terminate()
                frontend.wait(timeout=10)
            assert termios.tcgetattr(slave) == before
            assert (workspace / "burrow-profiles.json").read_bytes() == saved
            assert evidence.read_text() == "preserve this workspace evidence\n"
            assert burrow(workspace, "status")["pid"] == daemon
        finally:
            if frontend.poll() is None:
                frontend.terminate()
                frontend.wait(timeout=10)
            os.close(outer)
            os.close(slave)
    burrow(workspace, "connect", "gateway", "127.0.0.1", "tester", *options)
    until = time.monotonic() + 15
    while True:
        current = burrow(workspace, "inspect", "gateway")
        if current["state"] == "connected":
            break
        assert time.monotonic() < until, current
        time.sleep(.1)
    assert current["generation"] != first["generation"]
    print("PASS restart cancellation, manager replacement, daemon/evidence preservation and explicit reconnect", flush=True)
    return current
