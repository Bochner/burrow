"""Production command acceptance; isolated real Hovel, never operator state."""
import concurrent.futures
import fcntl
import json
import hashlib
import os
from pathlib import Path
import pty
import select
import signal
import socket
import struct
import subprocess
import sys
import tarfile
import tempfile
import termios
import time

signal.alarm(180)
binary, wheel, package, screen_check = [str(Path(p).resolve()) for p in sys.argv[1:]]
with tempfile.TemporaryDirectory(prefix="br-") as scratch:
    root = Path(scratch)
    env = {k: v for k, v in os.environ.items() if not k.startswith("HOVEL_")}
    env.update(HOME=scratch, XDG_DATA_HOME=str(root / "data"), XDG_CACHE_HOME=str(root / "cache"), XDG_CONFIG_HOME=str(root / "config"), NO_COLOR="1", TERM="xterm-256color")
    processes = set()

    def run(w, *options, ok=True, use_env=None):
        result = subprocess.run([binary, "--workspace", str(w), *options, "status"], env=use_env or env, capture_output=True, text=True, timeout=50)
        assert (result.returncode == 0) == ok, (result.stdout, result.stderr)
        if ok:
            data = json.loads(result.stdout)
            assert data["workspacePath"] == str(w), data
            processes.add(data["pid"])
            return data
        assert result.stderr and not result.stdout, result
        return result.stderr

    def private(path, text):
        path.write_text(text)
        path.chmod(0o600)

    try:
        # Run the actual packaged binary, not a parallel test implementation.
        unpack = root / "package"
        with tarfile.open(package) as archive:
            archive.extractall(unpack, filter="data")
        binary = str(unpack / "burrow")
        no_workspace = subprocess.run([binary, "--help"], env=env, capture_output=True, text=True)
        assert no_workspace.returncode == 0
        assert "Required:" in no_workspace.stderr
        assert not (root / "cache").exists()
        corrupt = root / "bad.whl"
        corrupt.write_bytes(b"synthetic-secret-canary-invalid-wheel")
        w = root / "bad"
        assert "verification failed" in run(w, "--hovel-package", str(corrupt), ok=False)
        assert not (w / "burrow").exists()
        assert not any(p.is_file() for p in (root / "cache").rglob("hovel"))
        assert "unavailable" in run(w, "--hovel-package", str(root / "missing"), ok=False)
        assert "not cached" in run(w, "--offline", ok=False)
        network_env = env | {"XDG_CACHE_HOME": str(root / "no-network-cache"), "HTTPS_PROXY": "http://127.0.0.1:1", "HTTP_PROXY": "http://127.0.0.1:1", "NO_PROXY": ""}
        assert "download unavailable" in run(root / "no-network", ok=False, use_env=network_env)
        # Simultaneous first launch must result in one daemon and one receipt.
        w = root / "w"
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            results = list(pool.map(lambda _: run(w, "--hovel-package", wheel), range(4)))
        assert len({r["pid"] for r in results}) == 1
        info = results[0]
        receipt = w / "burrow-launch.json"
        assert receipt.stat().st_mode & 0o777 == 0o600
        assert (w / "burrow").stat().st_mode & 0o777 == 0o700
        assert run(w, "--offline")["pid"] == info["pid"]
        other = run(root / "other", "--offline")
        assert other["pid"] != info["pid"]
        # Retain non-secret workspace evidence across successful and refused opens.
        evidence = w / "operator-evidence.txt"
        evidence.write_text("keep this evidence")
        original = receipt.read_text()
        for key, value in [("pid", os.getpid()), ("boot", "other-boot"), ("ticks", "0"), ("sha256", "0" * 64), ("workspace", str(root / "other")), ("startedAt", "yesterday")]:
            changed = json.loads(original)
            changed[key] = value
            private(receipt, json.dumps(changed))
            assert "refused" in run(w, "--offline", ok=False)
            assert receipt.read_text() == json.dumps(changed)
        for bad in ["{", "{}", original + "{}", "x" * 8193]:
            private(receipt, bad)
            run(w, "--offline", ok=False)
        private(receipt, original)
        receipt.chmod(0o644)
        run(w, "--offline", ok=False)
        assert receipt.stat().st_mode & 0o777 == 0o644
        receipt.chmod(0o600)
        saved = w / "saved-receipt"
        receipt.rename(saved)
        receipt.symlink_to(saved)
        run(w, "--offline", ok=False)
        assert receipt.is_symlink()
        receipt.unlink()
        saved.rename(receipt)
        runtime = w / "burrow"
        runtime.rename(w / "runtime-original")
        runtime.mkdir(mode=0o700)
        run(w, "--offline", ok=False)
        runtime.rmdir()
        runtime.symlink_to(w / "runtime-original", target_is_directory=True)
        run(w, "--offline", ok=False)
        runtime.unlink()
        (w / "runtime-original").rename(runtime)
        runtime.chmod(0o755)
        run(w, "--offline", ok=False)
        assert runtime.stat().st_mode & 0o777 == 0o755
        runtime.chmod(0o700)
        pending = w / "burrow-launch.json.pending"
        private(pending, "interrupted")
        run(w, "--offline", ok=False)
        assert pending.read_text() == "interrupted"
        pending.unlink()
        # Kill the real launcher during its visible publication window.
        interrupted = root / "interrupted"
        launcher = subprocess.Popen([binary, "--workspace", str(interrupted), "--offline", "status"], env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        deadline = time.monotonic() + 10
        while not (interrupted / "burrow-launch.json.pending").exists():
            assert launcher.poll() is None, launcher.communicate()
            assert time.monotonic() < deadline
            time.sleep(.001)
        launcher.kill()
        launcher.wait(timeout=5)
        time.sleep(.15)
        daemon_lock = interrupted / "daemon.lock"
        if daemon_lock.exists():
            processes.add(int(daemon_lock.read_text().strip().split(":")[1]))
        assert not (interrupted / "burrow-launch.json").exists()
        run(interrupted, "--offline", ok=False)
        assert (interrupted / "burrow-launch.json.pending").exists()
        # No new receipt may legitimize any unknown pre-existing reservation.
        for name in ["burrow", "hoveld.sock", "daemon.lock", "daemon.json", "burrow-launch.json.pending"]:
            unknown = root / ("u" + str(len(list(root.iterdir()))))
            unknown.mkdir(mode=0o700)
            item = unknown / name
            if name == "burrow":
                item.mkdir(mode=0o700)
                (item / "gateway").mkdir()
            else:
                private(item, "unknown")
            run(unknown, "--offline", ok=False)
            assert item.exists() and not (unknown / "burrow-launch.json").exists()
        unsafe = root / "unsafe"
        unsafe.mkdir(mode=0o777)
        unsafe.chmod(0o777)
        run(unsafe / "workspace", "--offline", ok=False)
        assert not (unsafe / "workspace").exists()
        readonly = root / "readonly"
        readonly.mkdir(mode=0o500)
        run(readonly / "workspace", "--offline", ok=False)
        assert not (readonly / "workspace").exists()
        readonly.chmod(0o700)
        link = root / "link"
        link.symlink_to(w, target_is_directory=True)
        run(link, "--offline", ok=False)
        long = root / ("x" * 100)
        assert "shorter workspace" in run(long, "--offline", ok=False)
        assert not long.exists()
        cache = root / "cache/burrow/hovel/0.4.2"
        executable = cache / "hovel"
        backup = cache / "original"
        executable.rename(backup)
        private(executable, "corrupt")
        executable.chmod(0o700)
        run(root / "invalid-exe", "--offline", ok=False)
        assert executable.read_text() == "corrupt"
        executable.unlink()
        backup.rename(executable)
        private(cache / "hovel.pending", "interrupted install")
        run(root / "incomplete-install", "--offline", ok=False)
        assert (cache / "hovel.pending").read_text() == "interrupted install"
        (cache / "hovel.pending").unlink()
        # A replacement endpoint peer must not receive public requests at all.
        endpoint = w / "hoveld.sock"
        endpoint.rename(w / "real.sock")
        with socket.socket(socket.AF_UNIX) as stranger:
            stranger.bind(str(endpoint))
            endpoint.chmod(0o600)
            stranger.listen()
            run(w, "--offline", ok=False)
            connection, _ = stranger.accept()
            assert connection.recv(4096) == b"", "sent RPC to unverified peer"
            connection.close()
        endpoint.unlink()
        (w / "real.sock").rename(endpoint)
        assert run(w, "--offline")["pid"] == info["pid"]
        assert evidence.read_text() == "keep this evidence"

        # Install and execute the production module through Hovel's ordinary chain.
        def hovel(*args, operator=False):
            prefix = [str(executable), "run", "--workspace", str(w), "--daemon-endpoint", str(endpoint)]
            if operator:
                prefix += ["--op", "setup-check", "--chain", "setup-check"]
            result = subprocess.run(prefix + ["--", *args], env=env, capture_output=True, text=True, timeout=20)
            assert result.returncode == 0, result.stdout + result.stderr
            return result.stdout
        installed = json.loads(hovel("module", "installed", "--json"))["modules"]
        assert [m["name"] for m in installed if m["name"].startswith("burrow")] == ["burrow"], installed
        module = next(m for m in installed if m["name"] == "burrow")
        source = Path(module["source"])
        assert module["linked"] and module["installed"], module
        assert hashlib.sha256((source / "burrow").read_bytes()).digest() == hashlib.sha256(Path(binary).read_bytes()).digest()
        lock = w / "module-lock.yaml"
        registered = lock.read_bytes()
        installed_at = lock.stat().st_mtime_ns
        assert run(w, "--offline")["pid"] == info["pid"]
        assert lock.read_bytes() == registered and lock.stat().st_mtime_ns == installed_at, "matching build was reinstalled"
        # Matching module ID/version is not proof of build provenance.
        alternate = root / "other-build"
        alternate.mkdir(mode=0o700)
        (alternate / "hovel-module.yaml").write_bytes((source / "hovel-module.yaml").read_bytes())
        (alternate / "burrow").write_bytes(Path(binary).read_bytes() + b"different-build")
        (alternate / "burrow").chmod(0o700)
        hovel("module", "install", "--link", str(alternate), "--replace", "--no-scripts")
        assert run(w, "--offline")["pid"] == info["pid"]
        assert json.loads(hovel("module", "installed", "--json"))["modules"][0]["source"] == str(source)
        # Actual installer write failure must refuse setup, retaining the daemon.
        hovel("module", "uninstall", "burrow@0.1.0")
        lock.chmod(0o400)
        try:
            assert "installation failed" in run(w, "--offline", ok=False)
        finally:
            lock.chmod(0o600)
        assert run(w, "--offline")["pid"] == info["pid"]
        assert evidence.read_text() == "keep this evidence"
        hovel("op", "create", "setup-check")
        hovel("chain", "create", "setup-check", operator=True)
        hovel("chain", "add", "burrow@0.1.0", operator=True)
        hovel("target", "add", "local://workspace", operator=True)
        hovel("chain", "config", "set", "workspace", str(w), operator=True)
        result = json.loads(hovel("throw", "--now", "--allow-dangerous", "--json", operator=True))["results"][0]
        assert result["state"] == "succeeded", result
        assert str(w) in json.dumps(result), result

        # Real public SDK framing, including the shared verified status operation.
        with subprocess.Popen([binary, "module"], env=env, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE) as module:
            def rpc(method, params=None, error=None):
                body = json.dumps(dict(jsonrpc="2.0", id=1, method=method, params=params or {})).encode()
                module.stdin.write(f"Content-Length: {len(body)}\r\n\r\n".encode() + body)
                module.stdin.flush()
                while True:
                    header = module.stdout.readline()
                    assert header.startswith(b"Content-Length: "), header
                    assert module.stdout.readline() == b"\r\n"
                    response = json.loads(module.stdout.read(int(header.split(b":")[1])))
                    if "id" in response:
                        if error:
                            assert error in json.dumps(response.get("error")), response
                            return
                        assert "error" not in response, response
                        return response["result"]
            assert "burrow" in json.dumps(rpc("handshake"))
            assert "workspace" in json.dumps(rpc("schema"))
            assert "connection" in json.dumps(rpc("schema"))
            result = rpc("execute", dict(runId="setup-check", moduleId="burrow@0.1.0", target="local", chainConfig={"workspace": str(w)}))
            assert str(w) in json.dumps(result), result
            # Ambiguous actions and cross-workspace connection inputs never launch.
            params = dict(runId="refused", moduleId="burrow@0.1.0", target="local",
                          chainConfig={"workspace": str(w), "command": "profiles", "connection": "{}"})
            rpc("execute", params, error="mutually exclusive")
            params["chainConfig"] = {"workspace": str(w), "connection": json.dumps({"workspace": str(root / "other")})}
            rpc("execute", params, error="legacy per-connection submission retired")
            rpc("shutdown")
            assert module.wait(timeout=5) == 0
            assert module.stdout.read() == b""

        # Real PTY input, draft-preserving help, safe paste, resizing and quit.
        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))
        before = termios.tcgetattr(slave)
        def controlling():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)
        terminal = subprocess.Popen([binary, "--workspace", str(w), "--offline", "tui"], env=env, stdin=slave, stdout=slave, stderr=slave, preexec_fn=controlling)
        output = bytearray()
        screen_dimensions = ["160", "40"]
        def read_until(needle, absent=b""):
            deadline = time.monotonic() + 8
            fresh = bytearray()
            while time.monotonic() < deadline:
                if select.select([master], [], [], .1)[0]:
                    data = os.read(master, 65536)
                    fresh.extend(data)
                    output.extend(data)
                    screen = subprocess.run([screen_check, *screen_dimensions], input=bytes(output), capture_output=True, timeout=3, check=True).stdout
                    if needle in screen and (not absent or absent not in screen):
                        return screen
            raise AssertionError((needle, subprocess.run([screen_check, *screen_dimensions], input=bytes(output), capture_output=True, timeout=3).stdout, bytes(fresh)))
        try:
            read_until(b"SAVED CONNECTION")
            # Real command palette keeps the draft isolated and filters actions.
            os.write(master, b"\x10")  # Ctrl+P
            read_until(b"Type to filter")
            os.write(master, b"meta")
            read_until(b"Metadata")
            os.write(master, b"\r")
            read_until(b"SELECTED CONNECTION")
            os.write(master, b"\x1b")
            time.sleep(.2)
            # Real New form launches through the shared verified Open operation.
            created = root / "data" / "burrow" / "workspaces" / "nav-created"
            os.write(master, b"\x1bn")  # Alt+N
            read_until(b"Workspace name")
            assert not created.exists(), "opening New mutated the destination"
            os.write(master, b"nav-created\r")
            # The submitted path is also visible in the pending New form.
            # Wait for activation before typing into the workspace's prompt.
            read_until(b"nav-created", absent=b"NEW WORKSPACE")
            assert (created / "burrow-launch.json").is_file(), "New did not launch the exact default destination"
            created_info = run(created, "--offline")
            assert created_info["pid"] != info["pid"]
            os.write(master, b"draft-created\x1bw")  # preserve draft, open drawer
            read_until(b"[Esc close]")
            os.write(master, b"\x1b[A\r")
            read_until(str(w).encode())
            # Mouse opens midpoint-anchored New at the 160x40 geometry.
            os.write(master, b"\x1b[<0;3;21M\x1b[<0;3;21m")
            read_until(b"Workspace name")
            os.write(master, b"\x1b")
            time.sleep(.2)
            os.write(master, b"\x1bw")
            read_until(b"[Esc close]")
            os.write(master, b"\x1b[B\r")
            read_until(b"draft-created")
            os.write(master, b"\x1bw")
            read_until(b"[Esc close]")
            os.write(master, b"\x1b[A\r")
            read_until(str(w).encode())
            os.write(master, b"sta\x1bOP")  # draft, F1
            read_until(b"Burrow Help")
            os.write(master, b"\x1b")
            time.sleep(.3)
            os.write(master, b"\t")
            read_until(b"status")
            os.write(master, b"\r")
            read_until(b"Verified daemon PID")
            os.write(master, b"\x1b[200~quit\nstatus\x1b[201~")
            time.sleep(.1)
            assert terminal.poll() is None, "paste executed"
            os.write(master, b"\x15")  # discard pasted draft
            for height, width in [(8, 30), (2, 8), (50, 200), (40, 160)]:
                fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
                os.kill(terminal.pid, signal.SIGWINCH)
                time.sleep(.1)
                assert terminal.poll() is None
            os.write(master, b"\x03")
            read_until(b"Keep working")
            os.write(master, b"\t\r")
            terminal.wait(timeout=5)
            assert terminal.returncode == 0
            assert termios.tcgetattr(slave) == before
            assert b"LOCAL FIXTURE" not in output and b"gateway" not in output
            assert b"38;2;" not in output and b"48;2;" not in output
        finally:
            if terminal.poll() is None:
                terminal.kill()
                terminal.wait()
            os.close(master)
            os.close(slave)
        assert run(w, "--offline")["pid"] == info["pid"]
        # A smaller color terminal keeps the application visible and recovers.
        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 120, 0, 0))
        before = termios.tcgetattr(slave)
        color_env = env | {"COLORTERM": "truecolor"}
        color_env.pop("NO_COLOR")
        terminal = subprocess.Popen([binary, "--workspace", str(w), "--offline", "tui"], env=color_env, stdin=slave, stdout=subprocess.PIPE, stderr=slave, preexec_fn=controlling)
        output = bytearray()
        screen_dimensions = ["120", "30"]
        try:
            read_until(b"SAVED CONNECTIONS")
            assert b"38;2;180;190;254" in output, "Catppuccin lavender missing"
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))
            screen_dimensions = ["160", "40"]
            os.kill(terminal.pid, signal.SIGWINCH)
            read_until(f"PID {info['pid']}".encode())
            os.write(master, b"\x10")
            read_until(b"Type to filter")
            assert b"\x1b[1 q" in output, "native blinking block cursor was not requested"
            os.write(master, b"\x1b")
            time.sleep(.2)
            # A failed refresh must invalidate the visible daemon identity.
            os.kill(info["pid"], signal.SIGTERM)
            processes.remove(info["pid"])
            time.sleep(.2)
            os.write(master, b"status\r")
            screen = read_until(b"UNVERIFIED")
            assert info["health"].encode() not in screen, screen
            assert str(w).encode() in screen, screen
            os.write(master, b"\x03")
            read_until(b"Keep working")
            os.write(master, b"\t\r")
            # Keep consuming terminal repaint bytes while the renderer shuts down.
            deadline = time.monotonic() + 5
            while terminal.poll() is None and time.monotonic() < deadline:
                if select.select([master], [], [], .1)[0]:
                    output.extend(os.read(master, 65536))
            assert terminal.wait(timeout=5) == 0
            assert terminal.stdout.read() == b"", "TUI leaked into captured stdout"
            assert termios.tcgetattr(slave) == before
        finally:
            if terminal.poll() is None:
                terminal.kill()
                terminal.wait()
            os.close(master)
            os.close(slave)
        # --demo needs neither a workspace nor a Hovel cache, and makes no resources.
        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))
        demo_cache = root / "unused-demo-cache"
        terminal = subprocess.Popen([binary, "--demo"], env=env | {"XDG_CACHE_HOME": str(demo_cache)}, stdin=slave, stdout=slave, stderr=slave, preexec_fn=controlling)
        output = bytearray()
        screen_dimensions = ["160", "40"]
        try:
            screen = read_until(b"production")
            assert b"DEMO" in screen and b"5432" in screen, screen
            os.write(master, b"\x03")
            read_until(b"Keep working")
            os.write(master, b"\t\r")
            assert terminal.wait(timeout=5) == 0
            assert not demo_cache.exists(), "demo installed Hovel"
        finally:
            if terminal.poll() is None:
                terminal.kill()
                terminal.wait()
            os.close(master)
            os.close(slave)
        # Daemon loss remains an explicit refusal, never an automatic restart.
        run(w, "--offline", ok=False)
        assert receipt.read_text() == original and evidence.read_text() == "keep this evidence"
        canary = b"synthetic-secret-canary-invalid-wheel"
        for path in w.rglob("*"):
            if path.is_file():
                assert canary not in path.read_bytes(), path
        print("PASS packaged production setup: concurrent/offline launch, refusal without mutation, public SDK, real PTY, retention and loss")
    finally:
        for pid in processes:
            try:
                os.kill(pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
