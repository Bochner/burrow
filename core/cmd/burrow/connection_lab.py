"""Disposable pinned OpenSSH container; actual Burrow -> Hovel -> module path."""
import base64
import hashlib
import http.client
import fcntl
import json
import os
from pathlib import Path
import signal
import pty
import select
import shlex
import sqlite3
import socket
import struct
import subprocess
import sys
import tempfile
import termios
import time

binary, wheel, image_file, screen_check = [str(Path(p).resolve()) for p in sys.argv[1:]]
image = Path(image_file).read_text().strip()
def interrupted(signum, _frame):
    raise SystemExit(f"acceptance interrupted by signal {signum}")
for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGALRM):
    signal.signal(signum, interrupted)
signal.alarm(600)

def command(*args, env=None, ok=True):
    p = subprocess.run(list(map(str, args)), env=env, capture_output=True, text=True, timeout=60)
    assert (p.returncode == 0) == ok, (args[:3], p.stdout, p.stderr)
    return p.stdout

def wait(check):
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        result = check()
        if result:
            return result
        time.sleep(.1)
    raise AssertionError("transition timed out")

with tempfile.TemporaryDirectory(prefix="bs-") as scratch:
    root = Path(scratch)
    env = {k: v for k, v in os.environ.items() if not k.startswith(("HOVEL_", "SSH_"))}
    env.update(HOME=scratch, XDG_CACHE_HOME=str(root / "cache"), XDG_CONFIG_HOME=str(root / "config"), NO_COLOR="1")
    daemons = []
    children = []
    container = None
    key = root / "client key"
    command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key)
    try:
        container = command("docker", "run", "-d", "--rm", "--publish", "127.0.0.1::2222",
                            "--env", "USER_NAME=tester", "--env", "PASSWORD_ACCESS=false",
                            "--env", "PUBLIC_KEY_FILE=/client.pub", "--mount",
                            f"type=bind,src={key}.pub,dst=/client.pub,readonly", image).strip()
        port = int(command("docker", "port", container, "2222/tcp").strip().rsplit(":", 1)[1])
        def ready():
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=1) as s:
                    return s.recv(128).startswith(b"SSH-")
            except OSError:
                return False
        wait(ready)
        hostkey = command("docker", "exec", container, "cat", "/config/ssh_host_keys/ssh_host_ed25519_key.pub").split()
        fingerprint = "SHA256:" + base64.b64encode(hashlib.sha256(base64.b64decode(hostkey[1])).digest()).decode().rstrip("=")
        def burrow(w, *args, ok=True):
            out = command(binary, "--workspace", w, *args, env=env, ok=ok)
            return json.loads(out) if ok else out
        w = root / "w"
        info = burrow(w, "--hovel-package", wheel, "status")
        daemons.append(info["pid"])
        options = ["--key", str(key), "--port", str(port), "--trust", fingerprint, "--yes"]
        def state_is(w, name, expected):
            s = burrow(w, "inspect", name)
            return s if s["state"] == expected else None
        # Unknown-host refusal happens before authentication; explicit reconnect
        # approves a key obtained independently from the controlled server.
        plain = ["--key", str(key), "--port", str(port), "--yes"]
        burrow(w, "connect", "unknown", "127.0.0.1", "tester", *plain)
        refused = wait(lambda: state_is(w, "unknown", "lost"))
        assert "unknown host" in refused["detail"] and refused["masterPID"] == 0
        burrow(w, "reconnect", "unknown", "127.0.0.1", "tester", *options)
        wait(lambda: state_is(w, "unknown", "connected"))
        burrow(w, "close", "unknown", "--yes")
        review = burrow(w, "connect", "gateway", "127.0.0.1", "tester", *options[:-1])
        assert "review" in review and not (w / "burrow/gateway").exists()
        first = burrow(w, "connect", "gateway", "127.0.0.1", "tester", *options)
        def connected():
            state = burrow(w, "inspect", "gateway")
            assert state["state"] != "lost", state
            return state if state["state"] == "connected" else None
        first = wait(connected)
        assert Path(first["socket"]).is_socket()
        assert Path(f'/proc/{first["masterPID"]}').exists()
        assert b"-N\0" in Path(f'/proc/{first["masterPID"]}/cmdline').read_bytes()
        assert burrow(w, "--offline", "status")["pid"] == info["pid"]
        assert burrow(w, "inspect", "gateway")["masterPID"] == first["masterPID"]
        burrow(w, "connect", "gateway", "127.0.0.1", "tester", *options, ok=False)
        assert burrow(w, "inspect", "gateway")["socketInode"] == first["socketInode"]
        # Refusals leave symlinks, unsafe permissions and substituted roots intact.
        alias = w / "burrow/alias"
        alias.symlink_to(Path(first["socket"]).parent, target_is_directory=True)
        burrow(w, "connect", "alias", "127.0.0.1", "tester", *options, ok=False)
        assert alias.is_symlink()
        alias.unlink()
        runtime = w / "burrow"
        runtime.chmod(0o755)
        burrow(w, "connect", "unsafe", "127.0.0.1", "tester", *options, ok=False)
        assert runtime.stat().st_mode & 0o777 == 0o755 and not (runtime / "unsafe").exists()
        runtime.chmod(0o700)
        reservation = Path(first["socket"]).parent
        reservation.chmod(0o755)
        burrow(w, "close", "gateway", "--yes", ok=False)
        assert reservation.stat().st_mode & 0o777 == 0o755
        reservation.chmod(0o700)
        moved = runtime / "moved"
        reservation.rename(moved)
        reservation.mkdir(mode=0o700)
        marker = reservation / "unknown"
        marker.write_text("leave me")
        burrow(w, "close", "gateway", "--yes", ok=False)
        assert marker.read_text() == "leave me"
        marker.unlink()
        reservation.rmdir()
        moved.rename(reservation)
        assert burrow(w, "inspect", "gateway")["state"] == "connected"
        # The same public operation works in another verified workspace.
        other = root / "other"
        daemons.append(burrow(other, "--offline", "status")["pid"])
        burrow(other, "connect", "gateway", "127.0.0.1", "tester", *options)
        wait(lambda: burrow(other, "inspect", "gateway")["state"] == "connected")
        assert burrow(other, "inspect", "gateway")["socket"] != first["socket"]
        # Kill the actual master; read-only inspection must never authenticate.
        os.kill(first["masterPID"], signal.SIGKILL)
        wait(lambda: burrow(w, "inspect", "gateway")["state"] == "lost")
        assert burrow(other, "inspect", "gateway")["state"] == "connected"
        burrow(w, "reconnect", "gateway", "127.0.0.1", "tester", *options)
        second = wait(connected)
        assert second["masterPID"] != first["masterPID"]
        # A previously approved host key must be remembered, and reconnect must
        # not need a fresh trust override after closing its runtime directory.
        burrow(w, "close", "gateway", "--yes")
        without_trust = ["--key", str(key), "--port", str(port), "--yes"]
        burrow(w, "connect", "gateway", "127.0.0.1", "tester", *without_trust)
        first = wait(connected)
        # Workspace trust refuses changed keys, even with a fresh --trust flag.
        trust_file = w / "burrow-known_hosts"
        trusted = trust_file.read_bytes()
        trust_file.write_text(f"[127.0.0.1]:{port} " + key.with_suffix(".pub").read_text())
        burrow(w, "connect", "changed", "127.0.0.1", "tester", *options)
        changed = wait(lambda: state_is(w, "changed", "lost"))
        assert "host key" in changed["detail"] and "refusing authentication" in changed["detail"], changed
        burrow(w, "close", "changed", "--yes")
        trust_file.write_bytes(trusted)
        # Failed public-key authentication and cancellation of slow discovery.
        wrong = root / "wrong"
        command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", wrong)
        burrow(w, "connect", "denied", "127.0.0.1", "tester", "--key", wrong, "--port", port, "--yes")
        denied = wait(lambda: state_is(w, "denied", "lost"))
        assert "authentication failed" in denied["detail"]
        burrow(w, "close", "denied", "--yes")
        with socket.socket() as stalled:
            stalled.bind(("127.0.0.1", 0))
            stalled.listen(16)
            burrow(w, "connect", "cancelled", "127.0.0.1", "tester", "--key", key,
                   "--port", stalled.getsockname()[1], "--yes")
            burrow(w, "close", "cancelled", "--yes")
            assert not (w / "burrow/cancelled").exists()
        # Agent-only authentication with an isolated agent, never the user's.
        agent_socket = root / "agent.sock"
        agent = subprocess.Popen(["ssh-agent", "-D", "-a", str(agent_socket)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        children.append(agent)
        wait(agent_socket.exists)
        command("ssh-add", key, env=env | {"SSH_AUTH_SOCK": str(agent_socket)})
        burrow(w, "connect", "agent", "127.0.0.1", "tester", "--agent", agent_socket, "--port", port, "--yes")
        wait(lambda: state_is(w, "agent", "connected"))
        burrow(w, "close", "agent", "--yes")
        # Synthetic credential material is never persisted by the production
        # command path, even when an encrypted key cannot authenticate in batch.
        canary = "burrow-synthetic-credential-" + os.urandom(16).hex()
        encrypted = root / "encrypted"
        command("ssh-keygen", "-q", "-t", "ed25519", "-N", canary, "-f", encrypted)
        burrow(w, "connect", "encrypted", "127.0.0.1", "tester", "--key", encrypted, "--port", port, "--yes")
        wait(lambda: state_is(w, "encrypted", "lost"))
        burrow(w, "close", "encrypted", "--yes")
        for process in (first["masterPID"], first["ownerPID"], info["pid"]):
            assert canary.encode() not in Path(f"/proc/{process}/cmdline").read_bytes()
        for path in w.rglob("*"):
            if path.is_file():
                assert canary.encode() not in path.read_bytes(), path
                assert b"BEGIN OPENSSH PRIVATE KEY" not in path.read_bytes(), path
        # Actual TUI commands, terminal restoration, narrow rendering and quit
        # retention. The screen oracle is the existing pinned VT emulator.
        for i in range(7):
            burrow(w, "connect", f"row{i}", "127.0.0.1", "tester", *plain)
            wait(lambda: state_is(w, f"row{i}", "connected"))
        outer, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
        before = termios.tcgetattr(slave)
        def controlling():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)
        tui = subprocess.Popen([binary, "--workspace", str(w), "--offline", "tui"],
                               env=env | {"TERM": "xterm-256color"}, stdin=slave, stdout=slave, stderr=slave,
                               preexec_fn=controlling)
        output = bytearray()
        dimensions = ["80", "24"]
        def screen_contains(needle):
            if select.select([outer], [], [], .1)[0]:
                output.extend(os.read(outer, 65536))
            rendered = subprocess.run([screen_check, *dimensions], input=bytes(output), capture_output=True)
            assert rendered.returncode == 0, rendered.stderr
            return needle in rendered.stdout
        try:
            wait(lambda: screen_contains(b"gateway"))
            wait(lambda: screen_contains(b"of 8"))
            # The same named connection in another workspace stays independent
            # when selected through production navigation, with drafts retained.
            os.write(outer, b"ins\x1bn")
            wait(lambda: screen_contains(b"Exact destination"))
            os.write(outer, str(other).encode() + b"\r")
            wait(lambda: screen_contains(str(other).encode()))
            wait(lambda: screen_contains(b"gateway"))
            assert burrow(other, "inspect", "gateway")["socket"] != first["socket"]
            os.write(outer, b"\x1bw")
            wait(lambda: screen_contains(b"[Esc close]"))
            os.write(outer, b"\x1b[A\r")
            wait(lambda: screen_contains(b"COMPLETION"))
            os.write(outer, b"\x15")
            os.write(outer, b"\x1b[1;3B" * 8)  # Alt+Down scrolls connection inventory
            wait(lambda: screen_contains(b"row6"))
            os.write(outer, b"\x1b[1;3A" * 8)
            os.write(outer, b"ins\t")
            wait(lambda: screen_contains(b"inspect gateway"))
            os.write(outer, b"\r")
            wait(lambda: screen_contains(b'"name"'))
            os.write(outer, b"\x1b[6~" * 3)
            wait(lambda: screen_contains(b'"socket"'))
            os.write(outer, b"\x1b[6~" * 5)  # PgDn reveals the remaining inspect fields
            wait(lambda: screen_contains(b"socketInode"))
            os.write(outer, b"close gateway\r")
            wait(lambda: screen_contains(b"review"))
            assert burrow(w, "inspect", "gateway")["state"] == "connected"
            os.write(outer, b"ins")
            wait(lambda: screen_contains(b"COMPLETION"))
            os.write(outer, b"\x1b[B" * 7)
            wait(lambda: screen_contains("› inspect row6".encode()))
            os.write(outer, b"\x15")  # Clear the completion draft before the next command.
            ui_command = shlex.join(["connect", "terminal", "127.0.0.1", "tester", "--key", str(key), "--port", str(port), "--yes"])
            os.write(outer, ui_command.encode() + b"\r")
            wait(lambda: screen_contains(b'"terminal"'))
            wait(lambda: state_is(w, "terminal", "connected"))
            os.write(outer, b"close terminal --yes\r")
            wait(lambda: screen_contains(b'"closed"'))
            assert all(s["name"] != "terminal" for s in burrow(w, "connections"))
            # Repaint after resize starts a new screen; do not replay old 80-column
            # cursor coordinates into an emulator that was only ever 40 columns.
            while select.select([outer], [], [], 0)[0]:
                os.read(outer, 65536)
            output.clear()
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 16, 40, 0, 0))
            dimensions = ["40", "16"]
            os.kill(tui.pid, signal.SIGWINCH)
            wait(lambda: screen_contains(b"gateway"))
            os.write(outer, b"\x03")
            wait(lambda: screen_contains(b"Quit Burrow?"))
            os.write(outer, b"\t\r")
            assert tui.wait(timeout=5) == 0
            assert termios.tcgetattr(slave) == before
            assert b"38;2;" not in output
        finally:
            if tui.poll() is None:
                tui.kill()
                tui.wait()
            os.close(outer)
            os.close(slave)
        assert burrow(w, "inspect", "gateway")["masterPID"] == first["masterPID"]
        for i in range(7):
            burrow(w, "close", f"row{i}", "--yes")
        # Non-secret diagnostics cannot fill Hovel's retained notification queue.
        def rpc(method, data):
            conn = http.client.HTTPConnection("localhost", timeout=15)
            conn.sock = socket.socket(socket.AF_UNIX)
            conn.sock.settimeout(15)
            conn.sock.connect(str(w / "hoveld.sock"))
            conn.request("POST", "/hovel.daemon.v1.DaemonService/" + method,
                         json.dumps(data), {"Content-Type": "application/json"})
            response = conn.getresponse()
            body = response.read()
            status = response.status
            conn.close()
            return status, json.loads(body)
        for _ in range(265):
            code, result = rpc("RunSessionCommand", {"SessionID": first["session"], "Request": {"command": "connection-status"}})
            assert code == 200 and json.loads(result["stdout"])["state"] == "connected", result
        # Cleanup failures preserve unknown contents and the control session.
        evidence = w / "operator-evidence"
        evidence.write_text("retain evidence")
        leftover = Path(first["socket"]).parent / "unknown-file"
        leftover.write_text("not owned by connection")
        burrow(w, "close", "gateway", "--yes", ok=False)
        assert leftover.read_text() == "not owned by connection" and evidence.read_text() == "retain evidence"
        leftover.unlink()  # the harness owns this injected conflict
        burrow(w, "close", "gateway", "--yes")
        burrow(other, "close", "gateway", "--yes")
        assert not Path(first["socket"]).parent.exists()
        assert evidence.read_text() == "retain evidence" and trust_file.read_bytes() == trusted
        # Hovel's dangerous-operation allowance is required before module launch.
        hovel = root / "cache/burrow/hovel/0.4.2/hovel"
        prefix = [hovel, "run", "--workspace", w, "--daemon-endpoint", w / "hoveld.sock",
                  "--op", "burrow", "--chain", "refused", "--"]
        config = dict(workspace=str(w), name="unconfirmed", host="127.0.0.1", user="tester", port=port,
                      key=str(key), knownHosts=str(trust_file))
        for args in [("chain", "create", "refused"), ("chain", "add", "burrow-connection@0.1.0"),
                     ("target", "add", "ssh://127.0.0.1"), ("chain", "config", "set", "connection", json.dumps(config))]:
            command(*prefix, *args, env=env)
        denied = subprocess.run(list(map(str, [*prefix, "throw", "--now", "--json"])), env=env, capture_output=True, text=True)
        assert denied.returncode != 0 and "dangerous" in (denied.stdout + denied.stderr).lower()
        assert not (w / "burrow/unconfirmed").exists()
        # Module loss must not adopt any remaining reservation after relaunch.
        burrow(w, "connect", "ownerloss", "127.0.0.1", "tester", *plain)
        lost = wait(lambda: state_is(w, "ownerloss", "connected"))
        os.kill(lost["ownerPID"], signal.SIGKILL)
        def ended(pid):
            try:
                return Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()[0] == "Z"
            except (FileNotFoundError, ProcessLookupError):
                return True
        wait(lambda: ended(lost["masterPID"]))
        reported = burrow(w, "inspect", "ownerloss")
        assert reported["state"] == "lost" and reported["socket"] == lost["socket"]
        assert burrow(w, "--offline", "status")["pid"] == info["pid"]
        burrow(w, "connect", "ownerloss", "127.0.0.1", "tester", *plain, ok=False)
        burrow(w, "close", "ownerloss", "--yes", ok=False)
        assert Path(lost["socket"]).parent.exists()
        # An unrelated surviving master is refused without touching its process.
        unknown_dir = w / "burrow/stranger"
        unknown_dir.mkdir(mode=0o700)
        unknown_socket = unknown_dir / "master"
        stranger = subprocess.Popen(["ssh", "-F", "/dev/null", "-M", "-N", "-S", str(unknown_socket),
                                     "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o",
                                     f"UserKnownHostsFile={trust_file}", "-i", str(key), "-p", str(port), "tester@127.0.0.1"],
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        children.append(stranger)
        wait(unknown_socket.exists)
        inode = unknown_socket.stat().st_ino
        burrow(w, "connect", "stranger", "127.0.0.1", "tester", *plain, ok=False)
        assert stranger.poll() is None and unknown_socket.stat().st_ino == inode
        # The Hovel throw evidence includes real plans and confirmations.
        with sqlite3.connect(w / "workspace.db") as db:
            plans = [json.loads(r[0]) for r in db.execute("select plan_json from throw_plans")]
            assert plans and all(p["confirmationId"] for p in plans)
            assert db.execute("select count(*) from throw_confirmations").fetchone()[0] >= len(plans)
        # Daemon loss cannot silently recreate a daemon, master or trust record.
        os.kill(info["pid"], signal.SIGKILL)
        burrow(w, "--offline", "status", ok=False)
        burrow(w, "connect", "afterloss", "127.0.0.1", "tester", *plain, ok=False)
        assert not (w / "burrow/afterloss").exists() and evidence.read_text() == "retain evidence"
        print("PASS production key/agent auth, trust, cancellation, ownership, cross-workspace isolation, loss/reconnect, bounded logs and truthful close", flush=True)
    finally:
        for child in children:
            child.terminate()
            child.wait(timeout=10)
        for pid in daemons:
            try:
                os.kill(pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
        if container:
            command("docker", "rm", "-f", container)
