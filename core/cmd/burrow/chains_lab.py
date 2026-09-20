"""Confirmed chain traffic through exact production-owned forwarding identities."""
import hashlib
import concurrent.futures
import http.server
import json
import socket
import os
import signal
import subprocess
import tarfile
import tempfile
import time
from pathlib import Path
import threading


def chain_checks(burrow, workspace, connection, hovel, env, hv, connect_options, binary, decoder):
    body = b"burrow chain traffic\n"
    requests = []
    active = []
    release = threading.Event()
    canary = "CHAIN-RESPONSE-NOT-A-CREDENTIAL"

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            requests.append(self.path)
            if self.path == "/hold":
                active.append(threading.get_ident())
                release.wait(6)
            if self.path == "/slow":
                active.append(threading.get_ident())
                time.sleep(7)
            data = canary.encode() if self.path == "/secret" else body
            if self.path == "/large":
                data = b"x" * ((1 << 20) + 1)
            self.send_response(302 if self.path == "/redirect" else 200)
            self.send_header("Content-Length", str(len(data)))
            self.send_header("Location", "/must-not-follow")
            self.send_header("X-Secret-Canary", canary)
            self.end_headers()
            try:
                self.wfile.write(data)
            except (BrokenPipeError, ConnectionResetError):
                pass

        def log_message(self, *_):
            pass

    def port():
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            return str(sock.getsockname()[1])

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    try:
        name = connection["name"]
        reverse = burrow(workspace, "tunnel", "create", name, "reverse", "0", "127.0.0.1", str(server.server_port), "--yes")
        remote_port = reverse["listen"].rsplit(":", 1)[1]
        local = burrow(workspace, "tunnel", "create", name, "forward", "127.0.0.2:" + port(), "localhost", remote_port, "--yes")
        selection = burrow(workspace, "chain", "select", name)
        assert {t["id"] for t in selection["tunnels"]} == {local["id"], reverse["id"]}, selection
        url = "http://localhost:" + remote_port + "/check"
        review = burrow(workspace, "chain", "http", name, local["id"], url)
        assert "review" in review and not requests, "review sent target traffic"
        result = burrow(workspace, "chain", "http", name, local["id"], url, "--review", review["digest"], "--yes")
        assert result["statusCode"] == 200 and result["bytes"] == len(body), result
        assert result["sha256"] == hashlib.sha256(body).hexdigest(), result
        assert requests == ["/check"], requests
        assert burrow(workspace, "inspect", name)["masterPID"] == connection["masterPID"]
        artifacts = json.loads(hv("artifact", "list", "--json"))
        saved = [a for a in artifacts if a["runId"] == result["runID"]]
        assert len(saved) == 1, saved
        evidence = json.loads((workspace / saved[0]["path"]).read_text())
        assert evidence["sha256"] == result["sha256"] and evidence["selection"]["id"] == local["id"], evidence
        storage = workspace / Path(saved[0]["path"]).parts[0]
        mode = storage.stat().st_mode & 0o777
        storage.chmod(0)
        try:
            burrow(workspace, "chain", "http", name, local["id"], url, "--yes", ok=False)
        finally:
            storage.chmod(mode)
        assert burrow(workspace, "inspect", name)["masterPID"] == connection["masterPID"]
        proxy = burrow(workspace, "proxy", "create", name, port(), "--yes")
        for tunnel, target in [(reverse, "http://127.0.0.1:" + str(server.server_port) + "/reverse"),
                               (proxy, "http://localhost:" + remote_port + "/socks")]:
            exported = burrow(workspace, "chain", "export", name, tunnel["id"], target)
            chain = workspace / "selected-tunnel.chain.json"
            chain.write_text(json.dumps(exported))
            before = len(requests)
            def throw(*flags):
                return subprocess.run([str(hovel), "throw", str(chain), "--workspace", str(workspace),
                                       "--daemon-endpoint", str(workspace / "hoveld.sock"), "--json", *flags],
                                      env=env, capture_output=True, text=True, stdin=subprocess.DEVNULL, timeout=30)
            for flags in [("--allow-dangerous",), ("--now",)]:
                rejected = throw(*flags)
                assert rejected.returncode != 0 and len(requests) == before, rejected
            completed = throw("--allow-dangerous", "--now")
            assert completed.returncode == 0, completed
            record = json.loads(completed.stdout)["results"][0]
            assert record["state"] == "succeeded", record
            consumed = json.loads(record["summary"])
            assert consumed["selection"] == tunnel and consumed["sha256"] == result["sha256"], consumed
            assert len(requests) == before + 1
        print("PASS confirmed saved-chain local/reverse/SOCKS hostname traffic and rejection", flush=True)
        chain.write_text(json.dumps(burrow(workspace, "chain", "export", name, local["id"], url.replace("/check", "/hold"))))
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            futures = [pool.submit(throw, "--allow-dangerous", "--now")]
            deadline = time.monotonic() + 5
            # Overlap admitted consumers, without racing the pinned Hovel
            # CLIs' SQLite initialization (which can refuse with SQLITE_BUSY).
            while not active and time.monotonic() < deadline:
                time.sleep(.02)
            futures.append(pool.submit(throw, "--allow-dangerous", "--now"))
            while len(active) < 2 and time.monotonic() < deadline:
                time.sleep(.02)
            release.set()
            assert len(active) == 2, ("consumers did not overlap", active, [f.result(timeout=20) for f in futures])
            for future in futures:
                completed = future.result(timeout=20)
                assert completed.returncode == 0 and json.loads(completed.stdout)["results"][0]["state"] == "succeeded", completed
        release.clear()
        active.clear()
        proc = subprocess.Popen([str(hovel), "throw", str(chain), "--workspace", str(workspace),
                                 "--daemon-endpoint", str(workspace / "hoveld.sock"), "--json", "--allow-dangerous", "--now"],
                                env=env, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            deadline = time.monotonic() + 5
            while not active and time.monotonic() < deadline:
                assert proc.poll() is None, proc.communicate()
                time.sleep(.02)
            assert active, "disconnected caller never reached consumer"
            proc.terminate()
            proc.communicate(timeout=5)
        finally:
            release.set()
            if proc.poll() is None:
                proc.kill()
                proc.communicate()
        assert burrow(workspace, "chain", "http", name, local["id"], url, "--yes")["state"] == "succeeded"
        for path in ("/redirect", "/secret"):
            before = len(requests)
            result = burrow(workspace, "chain", "http", name, local["id"], url.replace("/check", path), "--yes")
            assert len(requests) == before + 1 and result["state"] == "succeeded", result
            if path == "/redirect":
                assert result["statusCode"] == 302 and "/must-not-follow" not in requests
        burrow(workspace, "chain", "http", name, local["id"], url.replace("/check", "/large"), "--yes", ok=False)
        records = (workspace / "burrow-logs/operations.log").read_text().split("End record\n\n")
        assert any(" -- chain http\n" in record and "  Status: failed\n" in record and "/large" in record for record in records), "failed HTTP exchange has no failed audit outcome"
        for path in workspace.rglob("*"):
            if path.is_file():
                assert canary.encode() not in path.read_bytes(), path
        # The existing listener is never substituted after same-port recreation.
        stale = burrow(workspace, "chain", "export", name, local["id"], url)
        review = burrow(workspace, "chain", "http", name, local["id"], url)
        burrow(workspace, "tunnel", "remove", local["id"], "--yes")
        replacement = burrow(workspace, "tunnel", "create", name, "forward", local["listen"], "localhost", remote_port, "--yes")
        before = len(requests)
        chain.write_text(json.dumps(stale))
        rejected = throw("--allow-dangerous", "--now")
        assert json.loads(rejected.stdout)["results"][0]["state"] == "failed" and len(requests) == before, rejected
        burrow(workspace, "chain", "http", name, replacement["id"], url, "--review", review["digest"], "--yes", ok=False)
        assert burrow(workspace, "chain", "http", name, proxy["id"], url, "--yes")["state"] == "succeeded"
        burrow(workspace, "chain", "select", "missing", ok=False)
        missing = json.loads(json.dumps(stale))
        request = json.loads(missing["spec"]["config"]["request"])
        request["tunnel"]["id"] = ""
        raw = json.dumps(request)
        missing["spec"]["config"].update(request=raw, review=hashlib.sha256(raw.encode()).hexdigest())
        chain.write_text(json.dumps(missing))
        rejected = throw("--allow-dangerous", "--now")
        assert json.loads(rejected.stdout)["results"][0]["state"] == "failed", rejected
        assert burrow(workspace, "inspect", name)["masterPID"] == connection["masterPID"]
        chain_ui(binary, env, decoder, workspace, name, replacement["id"], url, requests)
        print("PASS concurrent consumers, frontend disconnect, bounded capture, secret exclusion and stale creation refusal", flush=True)
        chain.write_text(json.dumps(burrow(workspace, "chain", "export", name, replacement["id"], url.replace("/check", "/slow"))))
        active.clear()
        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
            consumer = pool.submit(throw, "--allow-dangerous", "--now")
            deadline = time.monotonic() + 15
            while not active and time.monotonic() < deadline:
                time.sleep(.02)
            assert active, "slow consumer never started"
            started = time.monotonic()
            removed = burrow(workspace, "tunnel", "remove", replacement["id"], "--yes")
            assert removed["state"] == "removed" and time.monotonic() - started > 5, removed
            completed = consumer.result(timeout=15)
            assert json.loads(completed.stdout)["results"][0]["state"] == "succeeded", completed
        assert burrow(workspace, "chain", "http", name, proxy["id"], url, "--yes")["state"] == "succeeded"
        print("PASS removal waits for a slow consumer, then removes only its listener", flush=True)
        burrow(workspace, "proxy", "remove", name, "--yes")
        burrow(workspace, "tunnel", "remove", reverse["id"], "--yes")
        created_chain = burrow(workspace, "chain", "connect", "chain-created", connection["host"], connection["user"],
                               *[arg for arg in connect_options if arg != "--yes"])
        chain.write_text(json.dumps(created_chain))
        before = burrow(workspace, "connections")
        rejected = throw("--allow-dangerous")
        assert rejected.returncode != 0 and burrow(workspace, "connections") == before, rejected
        completed = throw("--allow-dangerous", "--now")
        assert completed.returncode == 0, completed
        record = json.loads(completed.stdout)["results"][0]
        assert record["state"] == "succeeded", record
        created = json.loads(record["summary"])["connection"]
        assert created["name"] == "chain-created" and created["state"] == "connected", created
        assert burrow(workspace, "inspect", "chain-created")["creation"] == created["creation"]
        # Replaying the explicit creation refuses a live name, never reconnects.
        replay = throw("--allow-dangerous", "--now")
        assert json.loads(replay.stdout)["results"][0]["state"] == "failed", replay
        forward = burrow(workspace, "tunnel", "create", "chain-created", "reverse", "0", "127.0.0.1", str(server.server_port), "--yes")
        target = "http://127.0.0.1:" + str(server.server_port) + "/loss"
        lost = burrow(workspace, "chain", "export", "chain-created", forward["id"], target)
        os.kill(created["masterPID"], signal.SIGKILL)
        chain.write_text(json.dumps(lost))
        before = len(requests)
        rejected = throw("--allow-dangerous", "--now")
        assert json.loads(rejected.stdout)["results"][0]["state"] == "failed" and len(requests) == before, rejected
        burrow(workspace, "close", "chain-created", "--yes")
        rejected = throw("--allow-dangerous", "--now")
        assert json.loads(rejected.stdout)["results"][0]["state"] == "failed" and len(requests) == before, rejected
        assert json.loads((workspace / saved[0]["path"]).read_text()) == evidence, "teardown changed collected evidence"
        print("PASS confirmed selected local HTTP traffic and Hovel evidence", flush=True)
    finally:
        server.shutdown()
        server.server_close()


def dropbear_check(burrow, gateway_workspace, workspace, connection, hovel, env, container, command, key, packages):
    # Only known files from declared archives; never run APK scripts or extract paths.
    members = [[("usr/sbin/dropbear", "dropbear"), ("usr/bin/dropbearkey", "dropbearkey")],
               [("usr/lib/libutmps.so.0.1.3.3", "libutmps.so.0.1")],
               [("usr/lib/libskarnet.so.2.15.0.0", "libskarnet.so.2.15")]]
    with tempfile.TemporaryDirectory() as tmp:
        for package, selected in zip(packages, members):
            with tarfile.open(package, "r:gz", ignore_zeros=True) as archive:
                for source, name in selected:
                    path = Path(tmp) / name
                    path.write_bytes(archive.extractfile(source).read())
                    path.chmod(0o755)
        command("docker", "cp", tmp + "/.", container + ":/tmp/burrow-dropbear")
    command("docker", "exec", container, "chown", "-R", "tester:tester", "/tmp/burrow-dropbear")
    command("docker", "exec", "-u", "tester", "--env", "LD_LIBRARY_PATH=/tmp/burrow-dropbear", container,
            "/tmp/burrow-dropbear/dropbearkey", "-t", "ed25519", "-f", "/tmp/burrow-dropbear/host-key")
    command("docker", "exec", "-d", "-u", "tester", "--env", "LD_LIBRARY_PATH=/tmp/burrow-dropbear", container,
            "/tmp/burrow-dropbear/dropbear", "-F", "-E", "-s", "-w", "-j", "-k", "-r", "/tmp/burrow-dropbear/host-key",
            "-P", "/tmp/burrow-dropbear/server.pid", "-p", "127.0.0.1:2223")
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    tunnel = burrow(gateway_workspace, "tunnel", "create", connection["name"], "forward", str(port), "127.0.0.1", "2223", "--yes")
    with socket.create_connection(("127.0.0.1", port), timeout=5) as sock:
        assert b"dropbear" in sock.recv(128).lower(), "fixture is not Dropbear"
    exported = burrow(workspace, "chain", "connect", "dropbear", "127.0.0.1", "tester", "--port", str(port), "--key", str(key))
    chain = workspace / "dropbear.chain.json"
    chain.write_text(json.dumps(exported))
    completed = subprocess.run([str(hovel), "throw", str(chain), "--workspace", str(workspace),
                                "--daemon-endpoint", str(workspace / "hoveld.sock"), "--json", "--allow-dangerous", "--now"],
                               env=env, capture_output=True, text=True, stdin=subprocess.DEVNULL, timeout=45)
    assert completed.returncode == 0, completed
    record = json.loads(completed.stdout)["results"][0]
    assert record["state"] == "succeeded", record
    live = burrow(workspace, "inspect", "dropbear")
    assert live["state"] == "connected" and live["creation"] == json.loads(record["summary"])["connection"]["creation"], live
    execution = burrow(workspace, "run", "now", "dropbear", "--yes", "--", "/bin/sh", "-c", "printf dropbear-connection")
    assert execution["remoteExit"] == 0, execution
    burrow(workspace, "close", "dropbear", "--yes")
    with socket.create_connection(("127.0.0.1", port), timeout=5) as sock:
        assert b"dropbear" in sock.recv(128).lower(), "connection close removed the pre-existing server"
    wrong_key = workspace / "wrong-key"
    command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(wrong_key))
    chain.write_text(json.dumps(burrow(workspace, "chain", "connect", "wrong-key", "127.0.0.1", "tester", "--port", str(port), "--key", str(wrong_key))))
    failed = subprocess.run([str(hovel), "throw", str(chain), "--workspace", str(workspace),
                             "--daemon-endpoint", str(workspace / "hoveld.sock"), "--json", "--allow-dangerous", "--now"],
                            env=env, capture_output=True, text=True, stdin=subprocess.DEVNULL, timeout=45)
    assert json.loads(failed.stdout)["results"][0]["state"] == "failed", failed
    assert not burrow(workspace, "connections"), "failed authentication left a connection reserved"
    burrow(gateway_workspace, "tunnel", "remove", tunnel["id"], "--yes")
    command("docker", "exec", container, "sh", "-c", "kill $(cat /tmp/burrow-dropbear/server.pid)")
    print("PASS confirmed chain connects to existing Dropbear; Burrow owns the live connection, server survives close", flush=True)


def chain_ui(binary, env, decoder, workspace, name, tunnel, url, requests):
    import fcntl
    import pty
    import select
    import struct
    import termios

    for plain in (False, True):
        outer, slave = pty.openpty()
        before = termios.tcgetattr(slave)
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))
        def controlling():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)
        viewer_env = dict(env)
        if plain:
            viewer_env["NO_COLOR"] = "1"
        else:
            viewer_env.pop("NO_COLOR", None)
        frontend = subprocess.Popen([binary, "--workspace", str(workspace), "tui"], env=viewer_env,
                                    stdin=slave, stdout=slave, stderr=slave, preexec_fn=controlling)
        output = bytearray()
        def wait(needle):
            deadline = time.monotonic() + 20
            while time.monotonic() < deadline:
                if select.select([outer], [], [], .05)[0]:
                    output.extend(os.read(outer, 65536))
                screen = subprocess.run([decoder, "160", "40"], input=output, capture_output=True, check=True).stdout.decode()
                if needle in screen:
                    return screen
                assert frontend.poll() is None, screen
            raise AssertionError((needle, screen))
        try:
            wait(name)
            before_requests = len(requests)
            entry = ("chain http " + name + " " + tunnel + " " + url + "\r").encode()
            os.write(outer, entry)
            screen = wait("Proceed?")
            assert tunnel in screen and url in screen, screen
            assert len(requests) == before_requests, "TUI review sent target traffic"
            os.write(outer, b"\x1b")
            wait("COMMAND OUTPUT")
            assert len(requests) == before_requests, "TUI cancellation sent target traffic"
            os.write(outer, entry)
            wait("Proceed?")
            os.write(outer, b"\t\r")
            wait('"runID"')
            assert len(requests) == before_requests + 1, "confirmed TUI did not consume selection"
            if directory := os.environ.get("TEST_UNDECLARED_OUTPUTS_DIR"):
                Path(directory, f"chain-http-{'plain' if plain else 'color'}.ansi").write_bytes(output)
            os.write(outer, b"quit\r")
            wait("Keep running")
            os.write(outer, b"\r")
            deadline = time.monotonic() + 10
            while frontend.poll() is None and time.monotonic() < deadline:
                if select.select([outer], [], [], .05)[0]:
                    output.extend(os.read(outer, 65536))
            assert frontend.poll() is not None, "frontend did not exit"
            assert frontend.returncode == 0 and termios.tcgetattr(slave) == before
        finally:
            if frontend.poll() is None:
                frontend.kill()
                frontend.wait(timeout=5)
            os.close(outer)
            os.close(slave)
    print("PASS production TUI selected HTTP review, cancel, confirm, no-color and terminal restoration", flush=True)


def connection_chain_ui(binary, env, decoder, workspace, burrow, port, key, container, hovel):
    """Real Hovel handoff, private TUI/CLI authentication, and retained ownership."""
    import fcntl
    import pty
    import select
    import shlex
    import struct
    import termios

    secret = "chain-auth-" + os.urandom(16).hex()
    subprocess.run(["docker", "exec", "-i", container, "chpasswd"],
                   input=("tester:" + secret + "\n").encode(), check=True, capture_output=True)

    def no_leaks(output, original_pid=None):
        assert secret.encode() not in output, "password echoed in terminal"
        for path in workspace.rglob("*"):
            if path.is_file():
                assert secret.encode() not in path.read_bytes(), ("password persisted", path)
        for path in Path("/proc").glob("[0-9]*/cmdline"):
            try:
                if path.parent.name != str(original_pid):
                    assert secret.encode() not in path.read_bytes(), "password in child process arguments"
                assert secret.encode() not in path.with_name("environ").read_bytes(), "password in process environment"
            except (FileNotFoundError, PermissionError, ProcessLookupError):
                pass

    for mode in ("key", "password", "cancel", "cli", "inline"):
        name = "handoff-" + mode
        outer, slave = pty.openpty()
        before = termios.tcgetattr(slave)
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))
        def controlling():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)
        viewer_env = dict(env)
        if mode == "key":
            viewer_env.pop("NO_COLOR", None)
        args = ["chain", "connect", name, "127.0.0.1", "--user", "tester", "--port", str(port)]
        args += ["--key", str(key)] if mode == "key" else ["--password"]
        if mode == "inline": args += [secret]
        public_start = 0
        exported = workspace / "cli-password.chain.json"
        stdout = exported.open("wb") if mode == "cli" else None
        frontend = subprocess.Popen([binary, "--workspace", str(workspace), *(args if mode == "cli" else ["tui"])],
                                    env=viewer_env, stdin=slave, stdout=stdout or slave, stderr=slave, preexec_fn=controlling)
        output = bytearray()
        throw = None
        def wait(needle):
            deadline = time.monotonic() + 25
            while time.monotonic() < deadline:
                if select.select([outer], [], [], .05)[0]:
                    output.extend(os.read(outer, 65536))
                screen = subprocess.run([decoder, "160", "40"], input=output, capture_output=True, check=True).stdout.decode()
                if needle in screen:
                    return screen
                assert frontend.poll() is None, (mode, needle, screen)
            raise AssertionError((mode, needle, screen))
        try:
            if mode == "cli":
                wait("Chain JSON exported")
                chain = exported
            else:
                wait("SAVED CONNECTIONS")
                os.write(outer, (shlex.join(args) + "\r").encode())
                wait("--allow-dangerous")
                chain, = workspace.glob("connect-" + name + "-*.chain.json")
                assert chain.stat().st_mode & 0o777 == 0o600
                if mode == "inline": public_start = len(output)  # Initial typed command is intentionally visible.
            assert not any(s["name"] == name for s in burrow(workspace, "connections")), "staging authenticated"
            request = json.loads(json.loads(chain.read_text())["spec"]["config"]["request"])
            assert request["settings"]["user"] == "tester"
            if mode != "key":
                assert "PubkeyAuthentication no" in request["preview"] and "PreferredAuthentications password" in request["preview"]
                assert Path(request["frontend"]).exists()
            if mode == "cli":
                throw = subprocess.Popen([str(hovel), "throw", str(chain), "--workspace", str(workspace),
                                          "--daemon-endpoint", str(workspace / "hoveld.sock"), "--allow-dangerous", "--now", "--json"],
                                         env=env, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            else:
                os.write(outer, b"\r")
                wait("Type yes to throw:")
                assert not any(s["name"] == name for s in burrow(workspace, "connections")), "review authenticated"
                if mode == "cancel":
                    # A rejected plan leaves the private frontend waiting, but
                    # must not block an independent reviewed connection close.
                    os.write(outer, b"no")
                    time.sleep(.15)
                    os.write(outer, b"\r")
                    sibling = "handoff-sibling"
                    burrow(workspace, "connect", sibling, "127.0.0.1", "--user", "tester",
                           "--port", str(port), "--key", str(key), "--yes")
                    os.write(outer, b"\x1bb")
                    wait("ACTIVE SSH CONNECTIONS")
                    os.write(outer, b"close handoff-sibling\r")
                    wait("Close handoff-sibling")
                    os.write(outer, b"\t\r")
                    wait('"closed"')
                    assert not any(s["name"] == sibling for s in burrow(workspace, "connections"))
                    os.write(outer, b"\x1bh")
                    wait("h0v3l")
                    os.write(outer, b"\x03")
                    deadline = time.monotonic() + 10
                    while Path(request["frontend"]).exists():
                        assert time.monotonic() < deadline, "Ctrl+C left staged authentication waiting"
                        time.sleep(.05)
                    os.write(outer, b"\x1bb")
                    wait("frontend expired or cancelled")
                    os.write(outer, (shlex.join(args) + "\r").encode())
                    wait("Chain ready")
                    chain, = [p for p in workspace.glob("connect-" + name + "-*.chain.json") if p != chain]
                    request = json.loads(json.loads(chain.read_text())["spec"]["config"]["request"])
                    os.write(outer, b"\r")
                    wait("Type yes to throw:")
                os.write(outer, b"yes")
                time.sleep(.15)
                os.write(outer, b"\r")
            if mode not in ("key", "inline"):
                wait("SSH password")
                assert not termios.tcgetattr(slave)[3] & termios.ECHO
                no_leaks(output[public_start:])
                if mode != "cancel":
                    os.write(outer, secret.encode())
                    # Let the renderer publish the in-progress field before
                    # Enter; a same-read submit would miss visible-echo regressions.
                    until = time.monotonic() + .4
                    while time.monotonic() < until:
                        if select.select([outer], [], [], .05)[0]:
                            output.extend(os.read(outer, 65536))
                    no_leaks(output[public_start:])
                os.write(outer, b"\x1b" if mode == "cancel" else b"\r")
            if mode == "cli":
                out, err = throw.communicate(timeout=30)
                assert throw.returncode == 0 and json.loads(out)["results"][0]["state"] == "succeeded", (out, err)
                wait("SSH connection established")
                frontend.wait(timeout=10)
                assert frontend.returncode == 0
                assert json.loads(chain.read_text())["kind"] == "Chain", "CLI result contaminated exported JSON"
            else:
                wait("completed" if mode != "cancel" else "failed")
                os.write(outer, b"\x1bb")
                wait("ACTIVE SSH CONNECTIONS")
                if mode != "cancel":
                    view = wait(name)
                    assert "connected" in view.lower(), view
            if mode != "cancel":
                live = burrow(workspace, "inspect", name)
                assert live["state"] == "connected" and live["creation"], live
                if mode == "password":
                    burrow(workspace, "profile", "save", name)
                    profiles = burrow(workspace, "profiles")
                    assert any(p["name"] == name and p.get("passwordAuth") for p in profiles["profiles"]), profiles
            else:
                deadline = time.monotonic() + 15
                while any(s["name"] == name for s in burrow(workspace, "connections")):
                    assert time.monotonic() < deadline, "cancelled attempt retained a reservation"
                    time.sleep(.1)
            no_leaks(output[public_start:])
            if mode in ("password", "inline"):
                if mode == "inline":
                    assert b"SSH password" not in output[public_start:], "inline value opened a password popup"
                os.write(outer, b"\x1b[A")
                screen = wait("chain connect " + name)
                assert secret not in screen, "inline password recalled from history"
                assert "--password" in screen, "password option lost from recall"
                os.write(outer, b"\x15")
            if mode != "cli":
                os.write(outer, b"quit\r")
                wait("No connections to close" if mode == "cancel" else "Keep running")
                if mode == "cancel":
                    os.write(outer, b"\t")
                os.write(outer, b"\r")
                deadline = time.monotonic() + 15
                while frontend.poll() is None and time.monotonic() < deadline:
                    if select.select([outer], [], [], .05)[0]:
                        output.extend(os.read(outer, 65536))
                assert frontend.poll() == 0, "TUI did not exit"
            assert termios.tcgetattr(slave) == before
            if mode != "key":
                assert not Path(request["frontend"]).exists(), "one-use prompt endpoint survived"
            if mode != "cancel":
                assert burrow(workspace, "inspect", name)["state"] == "connected", "frontend exit closed a completed chain connection"
                burrow(workspace, "close", name, "--yes")
            if mode == "password":
                burrow(workspace, "profile", "delete", name, "--yes")
                stale = subprocess.run([str(hovel), "throw", str(chain), "--workspace", str(workspace),
                                        "--daemon-endpoint", str(workspace / "hoveld.sock"), "--allow-dangerous", "--now", "--json"],
                                       env=env, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=30)
                assert stale.returncode == 0 and json.loads(stale.stdout)["results"][0]["state"] == "failed", stale
                assert not any(s["name"] == name for s in burrow(workspace, "connections")), "expired password chain started SSH"
        finally:
            if throw is not None and throw.poll() is None:
                throw.kill()
                throw.communicate(timeout=5)
            if frontend.poll() is None:
                frontend.terminate()
                frontend.wait(timeout=15)
            if stdout:
                stdout.close()
            if directory := os.environ.get("TEST_UNDECLARED_OUTPUTS_DIR"):
                Path(directory, "chain-connect-" + mode + ".ansi").write_bytes(output[public_start:])
            os.close(outer)
            os.close(slave)
    print("PASS TUI Hovel key/password handoff, cancellation, Alt+B live ownership, CLI private password and no leaks", flush=True)

    # No controlling terminal: review stays passive, approval uses the private
    # broker, and only the original explicitly supplied CLI argv may contain it.
    base = ["127.0.0.1", "--user", "tester", "--port", str(port)]
    def headless(*args):
        result = subprocess.run([binary, "--workspace", str(workspace), *args], env=env,
                                stdin=subprocess.DEVNULL, capture_output=True, timeout=30, start_new_session=True)
        no_leaks(result.stdout + result.stderr)
        return result
    review = headless("connect", "inline-cli", *base, "--password", secret)
    assert review.returncode == 0 and "review" in json.loads(review.stdout), review.stderr
    assert not (workspace / "burrow/inline-cli").exists(), "review launched SSH"
    connected = headless("connect", "inline-cli", *base, "--password", secret, "--yes",
                         "--review", json.loads(review.stdout)["digest"])
    assert connected.returncode == 0 and json.loads(connected.stdout)["state"] == "connected", connected.stderr
    burrow(workspace, "profile", "save", "inline-cli")
    no_leaks(b"")
    burrow(workspace, "close", "inline-cli", "--yes")
    burrow(workspace, "profile", "delete", "inline-cli", "--yes")
    wrong = headless("connect", "inline-wrong", *base, "--password", secret + "-wrong", "--yes")
    assert wrong.returncode != 0 and b"attempt closed" in wrong.stderr, wrong.stderr
    assert not (workspace / "burrow/inline-wrong").exists(), "wrong-password attempt retained"
    # Same host/user as the target, with a password-only jump: no target password
    # may be sent to it. The jump's config disables the available test key.
    jump_config = workspace / "inline-jump-config"
    jump_config.write_text(f"Host jump\n HostName 127.0.0.1\n User tester\n Port {port}\n PubkeyAuthentication no\n PreferredAuthentications password\n")
    refused = headless("connect", "inline-jump", "127.0.0.1", "--user", "tester", "--port", "2222", "--ssh-config", str(jump_config), "--jump", "jump",
                       "--password", secret, "--yes")
    assert refused.returncode != 0 and b"non-target authentication" in refused.stderr, refused.stderr
    assert not (workspace / "burrow/inline-jump").exists(), "jump refusal retained an attempt"
    jump_config.write_text(f"Host jump\n HostName 127.0.0.1\n User tester\n Port {port}\n IdentityFile {json.dumps(str(key))}\n")
    connected = headless("connect", "inline-jump", "127.0.0.1", "--user", "tester", "--port", "2222", "--ssh-config", str(jump_config), "--jump", "jump",
                         "--password", secret, "--yes")
    assert connected.returncode == 0 and json.loads(connected.stdout)["state"] == "connected", connected.stderr
    burrow(workspace, "close", "inline-jump", "--yes")
    exported = workspace / "headless-inline.chain.json"
    with exported.open("wb") as output_file:
        frontend = subprocess.Popen([binary, "--workspace", str(workspace), "chain", "connect", "inline-chain", *base,
                                     "--password=" + secret], env=env, stdin=subprocess.DEVNULL, stdout=output_file,
                                    stderr=subprocess.PIPE, start_new_session=True)
        try:
            deadline = time.monotonic() + 15
            while not exported.stat().st_size:
                assert frontend.poll() is None and time.monotonic() < deadline, "headless export failed"
                time.sleep(.05)
            no_leaks(b"", frontend.pid)
            def throw(*flags):
                return subprocess.run([str(hovel), "throw", str(exported), "--workspace", str(workspace),
                                       "--daemon-endpoint", str(workspace / "hoveld.sock"), "--allow-dangerous", "--json", *flags],
                                      env=env, stdin=subprocess.DEVNULL, capture_output=True, timeout=30, start_new_session=True)
            assert throw().returncode != 0, "headless password bypassed Hovel confirmation"
            assert not (workspace / "burrow/inline-chain").exists()
            completed = throw("--now")
            assert completed.returncode == 0 and json.loads(completed.stdout)["results"][0]["state"] == "succeeded", completed.stdout
            _, error = frontend.communicate(timeout=10)
            assert frontend.returncode == 0, error
            no_leaks(completed.stdout + completed.stderr + error)
            assert json.loads(exported.read_text())["kind"] == "Chain", "export stdout contaminated"
            burrow(workspace, "close", "inline-chain", "--yes")
            stale = throw("--now")
            assert json.loads(stale.stdout)["results"][0]["state"] == "failed"
            assert not (workspace / "burrow/inline-chain").exists(), "one-use chain replayed"
        finally:
            if frontend.poll() is None: frontend.terminate(); frontend.communicate(timeout=15)
    print("PASS inline TUI/headless direct/chain password, passive review, one-use broker, wrong password cleanup, jump account isolation and no downstream secrets", flush=True)
