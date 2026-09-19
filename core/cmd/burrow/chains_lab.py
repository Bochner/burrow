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
            futures = [pool.submit(throw, "--allow-dangerous", "--now") for _ in range(2)]
            deadline = time.monotonic() + 5
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
        burrow(workspace, "proxy", "remove", name, "--yes")
        burrow(workspace, "tunnel", "remove", replacement["id"], "--yes")
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
