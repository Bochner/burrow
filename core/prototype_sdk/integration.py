"""Disposable Linux integration proof against an Aspect-built, pinned Hovel binary."""
import fcntl
import pty
import select
import struct
import termios
import json
import os
from pathlib import Path
import signal
import sqlite3
import subprocess
import sys
import tarfile
import tempfile
import time

archive, frontend, hovel = map(lambda p: str(Path(p).resolve()), sys.argv[1:])


def wait_for(check):
    for _ in range(200):
        if check():
            return
        time.sleep(0.05)
    raise AssertionError("timed out waiting for lifecycle transition")


for linked in (True, False):
    with tempfile.TemporaryDirectory(prefix="burrow-sdk-proof-") as scratch:
        root = Path(scratch)
        workspace = root / "workspace"
        package = root / "package"
        package.mkdir()
        with tarfile.open(archive) as tar:
            tar.extractall(package, filter="data")
        env = os.environ | {"XDG_CONFIG_HOME": str(root / "config"), "XDG_DATA_HOME": str(root / "data")}
        daemon = None
        pids = []

        def cli(*args, operator=False, input=None):
            prefix = [hovel, "run", "--workspace", str(workspace)]
            if operator:
                prefix += ["--op", "proof", "--chain", "proof"]
            result = subprocess.run(prefix + ["--", *args], cwd=root, env=env,
                                    input=input, text=True, capture_output=True, timeout=20)
            assert result.returncode == 0, (args, result.stdout, result.stderr)
            return result.stdout

        try:
            with (root / "daemon.log").open("w") as log:
                daemon = subprocess.Popen([hovel, "daemon", "serve", "--workspace", str(workspace)],
                                          cwd=root, env=env, stdout=log, stderr=log)
            wait_for(lambda: (workspace / "hoveld.sock").exists() or daemon.poll() is not None)
            assert daemon.poll() is None, (root / "daemon.log").read_text()
            print(cli("module", "check", "--warnings-as-errors", str(package)), flush=True)
            args = ["--link", str(package)] if linked else [archive]
            print(cli("module", "install", *args), flush=True)
            assert "burrow-sdk-prototype" in cli("module", "installed")
            cli("op", "create", "proof")
            cli("chain", "create", "proof", operator=True)
            cli("chain", "add", "burrow-sdk-prototype@0.0.0", operator=True)
            cli("target", "add", "mock://inert", operator=True)
            payload = json.loads(cli("throw", "--now", "--json", operator=True))
            result = payload["results"][0]
            assert result["state"] == "succeeded", result
            session = result["sessions"][0]["id"]
            pid = int(result["summary"].split("pid=")[1])
            pids.append(pid)
            assert Path(f"/proc/{pid}").exists()
            assert session in cli("session", "list", operator=True)
            # Real controlling terminal, unlike the earlier input-EOF proof.
            for attempt in range(2):
                master, slave = pty.openpty()
                original = termios.tcgetattr(slave)
                fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))

                def controlling_terminal():
                    os.setsid()
                    fcntl.ioctl(0, termios.TIOCSCTTY, 0)

                attachment = None
                output = bytearray()
                try:
                    attachment = subprocess.Popen(
                        [hovel, "session", "connect", session, "--no-history", "--workspace", str(workspace)],
                        cwd=root, env=env, stdin=slave, stdout=slave, stderr=slave,
                        preexec_fn=controlling_terminal)

                    def read_until(marker):
                        deadline = time.monotonic() + 10
                        while marker not in output:
                            assert time.monotonic() < deadline, bytes(output)
                            if select.select([master], [], [], 0.1)[0]:
                                output.extend(os.read(master, 65536))

                    read_until(b"Connected to session")
                    os.write(master, b"x")
                    read_until(b"byte=78 size=0x0")
                    assert termios.tcgetattr(slave) != original
                    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
                    os.kill(attachment.pid, signal.SIGWINCH)
                    os.write(master, b"y")
                    read_until(b"byte=79 size=0x0")
                    os.write(master, b"\x03")
                    read_until(b"byte=03 size=0x0")
                    assert attachment.poll() is None
                    os.write(master, b"a" if attempt == 0 else b"r")
                    read_until(b"\x1b[?25l" if attempt == 0 else b"\x1b[?1049l")
                    os.write(master, b"\x1d")
                    read_until(b"Detached from session")
                    assert attachment.wait(timeout=10) == 0
                    while select.select([master], [], [], 0.1)[0]:
                        output.extend(os.read(master, 65536))
                    assert termios.tcgetattr(slave) == original
                    assert b"byte=1d" not in output
                    if attempt == 0:
                        assert b"\x1b[?1049l" not in output and b"\x1b[?25h" not in output
                    assert Path(f"/proc/{pid}").exists()
                    print(f"REAL PTY attempt={attempt + 1}: {bytes(output)!r}", flush=True)
                finally:
                    if attachment and attachment.poll() is None:
                        attachment.kill()
                        attachment.wait(timeout=5)
                    os.close(master)
                    os.close(slave)
            # Separate local frontend: geometry and cleanup stay on the operator terminal.
            for key, reason in ((b"\x1d", "detached"), (b"\x03", "interrupted"), (b"q", "closed")):
                master, slave = pty.openpty()
                original = termios.tcgetattr(slave)
                fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
                attachment = None
                output = bytearray()
                try:
                    attachment = subprocess.Popen(
                        [sys.executable, frontend, str(workspace / "hoveld.sock"), session],
                        cwd=root, env=env, stdin=slave, stdout=slave, stderr=slave,
                        preexec_fn=controlling_terminal)
                    read_until(b"PROTOTYPE 80x24")
                    assert termios.tcgetattr(slave) != original
                    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
                    os.kill(attachment.pid, signal.SIGWINCH)
                    read_until(b"PROTOTYPE 120x40")
                    os.write(master, b"x")
                    read_until(b"byte=78 size=0x0")
                    os.write(master, key)
                    read_until(f"Frontend {reason}".encode())
                    assert attachment.wait(timeout=10) == 0
                    while select.select([master], [], [], 0.1)[0]:
                        output.extend(os.read(master, 65536))
                    assert termios.tcgetattr(slave) == original
                    assert b"\x1b[?25h\x1b[?1049l" in output
                    if reason != "closed":
                        assert Path(f"/proc/{pid}").exists()
                        assert session in cli("session", "list", operator=True)
                    print(f"PASS LOCAL FRONTEND: 80x24 -> 120x40, public daemon input/output, {reason}, screen/cursor/termios restored", flush=True)
                finally:
                    if attachment and attachment.poll() is None:
                        attachment.kill()
                        attachment.wait(timeout=5)
                    os.close(master)
                    os.close(slave)
            wait_for(lambda: not Path(f"/proc/{pid}").exists())
            pids.remove(pid)
            # A second execution remains active while the daemon shuts down.
            payload = json.loads(cli("throw", "--now", "--json", operator=True))
            pid = int(payload["results"][0]["summary"].split("pid=")[1])
            pids.append(pid)
            with sqlite3.connect(workspace / "workspace.db") as db:
                plans = [json.loads(row[0]) for row in db.execute("select plan_json from throw_plans")]
            assert plans and all(p["confirmationId"] for p in plans), plans
            daemon.send_signal(signal.SIGTERM)
            assert daemon.wait(timeout=10) == 0
            wait_for(lambda: not Path(f"/proc/{pid}").exists())
            pids.remove(pid)
            print(f"PASS {'linked' if linked else 'archive'}: discovery/schema, confirmed execution, real raw input/Ctrl-C/Ctrl-] detach/reattach, termios restoration, close/shutdown cleanup; OBSERVED GAPS: geometry stays 0x0; detach emits no screen/cursor reset", flush=True)
        finally:
            if daemon and daemon.poll() is None:
                daemon.terminate()
                try:
                    daemon.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    daemon.kill()
                    daemon.wait(timeout=5)
            for pid in pids:
                try:
                    os.kill(pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
