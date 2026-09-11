"""Disposable setup/attachment proof; never installs into the operator's home."""
import hashlib
import http.client
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
import zipfile

def input_path(value):
    path = Path(value)
    if value.startswith("external/"):
        path = Path(__file__).parents[3] / value.removeprefix("external/")
    return str(path.resolve())


wheel, archive, frontend, integration = map(input_path, sys.argv[1:])
DIGEST = "7edccdabe04e30098e417d27ab48064c11a8612149124b7df0797251e88a0933"


def verify(data):
    if hashlib.sha256(data).hexdigest() != DIGEST:
        raise RuntimeError("Hovel package verification failed; retry the pinned download. Nothing installed.")


def rpc(endpoint, method="GetDaemonInfo", acknowledge=False, payload=None):
    conn = http.client.HTTPConnection(endpoint if not endpoint.startswith("/") else "localhost", timeout=2)
    if endpoint.startswith("/"):
        conn.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        conn.sock.settimeout(2)
        conn.sock.connect(endpoint)
    try:
        headers = {"Content-Type": "application/json"}
        if acknowledge:
            headers["X-Hovel-Insecure-Full-Control"] = "true"
        conn.request("POST", "/hovel.daemon.v1.DaemonService/" + method, json.dumps(payload or {}), headers)
        response = conn.getresponse()
        body = response.read(1024 * 1024 + 1)
        assert len(body) <= 1024 * 1024
        return response.status, json.loads(body) if response.status == 200 else body.decode(errors="replace")
    finally:
        conn.close()


def reject_unverifiable(info):
    # No invented handshake fields or successful compatibility path at this pin.
    raise RuntimeError(
        "Cannot verify running Hovel daemon compatibility at " + info["workspacePath"] +
        "; this public API provides no version/capability handshake. "
        "Use an upstream release with that contract; no restart, replacement, or workspace fallback performed.")


with tempfile.TemporaryDirectory(prefix="burrow-setup-proof-") as scratch:
    root = Path(scratch)
    env = {k: v for k, v in os.environ.items() if not k.startswith("HOVEL_")}
    env.update(HOME=str(root / "home"), XDG_CONFIG_HOME=str(root / "config"), XDG_DATA_HOME=str(root / "data"))
    Path(env["HOME"]).mkdir()
    data = Path(wheel).read_bytes()
    verify(data)
    try:
        verify(data + b"tampered")
    except RuntimeError as error:
        print("PASS corrupt package: " + str(error), flush=True)
    else:
        raise AssertionError("corrupt package accepted")
    # Fixed member extraction avoids arbitrary archive paths; no pip/runtime bootstrap needed.
    hovel = root / "data" / "burrow" / "hovel" / "0.4.2" / "hovel"
    hovel.parent.mkdir(parents=True)
    with zipfile.ZipFile(wheel) as package:
        hovel.write_bytes(package.read("hovel/bin/hovel"))
    hovel.chmod(0o700)
    print("PASS verified published Linux amd64 wheel, extracted upstream Go executable", flush=True)
    workspace = root / "data" / "burrow" / "workspace"
    other = root / "selected-workspace"
    unused = root / "must-not-create"
    daemons = []

    def cli(*args, extra_env=None, success=True):
        result = subprocess.run([str(hovel), *args], cwd=root, env=env | (extra_env or {}),
                                text=True, capture_output=True, timeout=15)
        assert (result.returncode == 0) == success, (args, result.stdout, result.stderr)
        return result.stdout + result.stderr

    def serve(path, full=False):
        log = (root / (path.name + ".log")).open("w")
        args = [str(hovel), "daemon", "serve", "--workspace", str(path),
                "--listen", str(path / "hoveld.sock"), "--listen", "tcp://127.0.0.1:0",
                "--advertise", "tcp://127.0.0.1:0=tcp://127.0.0.1:0"]
        if full:
            args.append("--allow-insecure-tcp")
        process = subprocess.Popen(args, cwd=root, env=env, stdout=log, stderr=log, start_new_session=True)
        log.close()
        daemons.append(process)
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            assert process.poll() is None, (root / (path.name + ".log")).read_text()
            try:
                status, info = rpc(str(path / "hoveld.sock"))
                if status == 200:
                    return process, info
            except (OSError, http.client.HTTPException):
                pass
            time.sleep(0.05)
        raise AssertionError("daemon startup timed out")

    try:
        print(cli("version"), flush=True)
        first, info = serve(workspace)
        second, other_info = serve(other, full=True)
        assert info["workspacePath"] == str(workspace) and other_info["workspacePath"] == str(other)
        # Discovery is precisely these two known workspaces; status never starts a daemon.
        for candidate in (workspace, other):
            output = cli("status", "--workspace", str(candidate))
            assert str(candidate) in output, output
        print("PASS stable explicit default path, two bounded workspace status/reachability probes", flush=True)
        config = root / "client.yaml"
        config.write_text("apiVersion: hovel.dev/v1alpha1\nkind: HovelConfig\ndaemon:\n  client:\n    endpoint: " +
                          str(workspace / "hoveld.sock") + "\n")
        base = ("run", "--workspace", str(unused), "--config", str(config))
        def selected(expected, flags=(), extra_env=None):
            output = cli(*base, *flags, "--", "control", "daemon", "status", extra_env=extra_env)
            assert str(expected) in output, output
            assert not unused.exists(), "attachment silently created a local workspace"
        selected(workspace)
        selected(other, extra_env={"HOVEL_DAEMON_ENDPOINT": str(other / "hoveld.sock")})
        selected(workspace, ("--daemon-endpoint", str(workspace / "hoveld.sock")),
                 {"HOVEL_DAEMON_ENDPOINT": str(other / "hoveld.sock")})
        print("PASS actual CLI precedence: explicit flag > Hovel environment > daemon.client config", flush=True)
        tcp = next(item["bind"] for item in info["listeners"] if item["network"] == "tcp")
        full_tcp = next(item["bind"] for item in other_info["listeners"] if item["network"] == "tcp")
        for endpoint, access in ((tcp, "read-only"), (full_tcp, "insecure-full")):
            assert rpc(endpoint)[1]["access"] == "read-only"
            observed = rpc(endpoint, acknowledge=True)[1]
            assert observed["access"] == access, (access, observed)
        for endpoint, ack in ((tcp, False), (tcp, True), (full_tcp, False)):
            denied = rpc(endpoint, "CreateOperation", ack, {"Operation": "inert-proof"})
            assert denied[0] == 403, denied
        assert rpc(full_tcp, "CreateOperation", True, {"Operation": "inert-proof"})[0] == 200
        selected(other, ("--daemon-endpoint", "tcp://" + full_tcp, "--allow-insecure-daemon"))
        selected(other, ("--daemon-endpoint", full_tcp, "--allow-insecure-daemon"))
        print("PASS remote endpoint forms/actual daemon workspace, read-only denial, two-sided insecure-full acknowledgement", flush=True)
        for endpoint, message in (("https://" + tcp, "https"),
                                  (str(root / "missing.sock"), "connect")):
            output = cli(*base, "--daemon-endpoint", endpoint, "--daemon-connect-timeout", "100ms",
                         "--", "control", "daemon", "status", success=False)
            assert message in output.lower(), output
            assert not unused.exists()
        assert set(info) <= {"workspacePath", "pid", "startedAt", "health", "access", "listeners"}, info
        try:
            reject_unverifiable(info)
        except RuntimeError as error:
            print("EXPECTED ATTACHMENT BLOCK: " + str(error), flush=True)
        assert rpc(str(workspace / "hoveld.sock"))[1] == info
        assert first.poll() is None and second.poll() is None and not unused.exists()
        print("PASS failed/unverifiable attachment preserves daemon identity and creates no fallback workspace", flush=True)
        assert rpc(str(workspace / "hoveld.sock"), "CreateOperation", payload={"Operation": "persistent-proof"})[0] == 200
        first.terminate()  # Explicit fixture restart, never an attachment fallback.
        assert first.wait(timeout=10) == 0
        restarted, new_info = serve(workspace)
        assert new_info["workspacePath"] == info["workspacePath"] and new_info["pid"] != info["pid"]
        output = cli("run", "--workspace", str(workspace), "--daemon-endpoint", str(workspace / "hoveld.sock"),
                     "--", "op", "list")
        assert "persistent-proof" in output, output
        print("PASS explicit restart reuses the same default workspace and its saved operation", flush=True)
    finally:
        for process in daemons:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
        assert all(process.poll() is not None for process in daemons)
    # Trusted fixture provenance is controlled here; this is NOT a compatibility-check bypass
    # for an operator-selected daemon. Reuse the prior real two-client detach/close proof.
    subprocess.run([sys.executable, integration, archive, frontend, str(hovel)],
                   env=env, cwd=root, check=True, timeout=180)
    print("PASS setup proof cleanup; runtime handoff tested, arbitrary-daemon compatibility remains blocked", flush=True)
