"""Throwaway Linux own-daemon proof. Run through Aspect; never uses real workspaces."""
import base64
import ctypes
import hashlib
import http.client
import json
import os
from pathlib import Path
import select
import signal
import socket
import stat
import struct
import subprocess
import sys
import tempfile
import time
import zipfile

WHEEL_SHA = "7edccdabe04e30098e417d27ab48064c11a8612149124b7df0797251e88a0933"

# The pinned standalone Python omits os.pidfd_open. Use Linux/glibc directly.
LIBC = ctypes.CDLL(None, use_errno=True)
LIBC.pidfd_open.argtypes = [ctypes.c_int, ctypes.c_uint]
LIBC.pidfd_send_signal.argtypes = [ctypes.c_int, ctypes.c_int, ctypes.c_void_p, ctypes.c_uint]


def pidfd_open(pid):
    result = LIBC.pidfd_open(pid, 0)
    if result < 0:
        raise OSError(ctypes.get_errno(), "pidfd_open failed")
    return result


def terminate(pidfd):
    if LIBC.pidfd_send_signal(pidfd, signal.SIGTERM, None, 0) < 0:
        raise OSError(ctypes.get_errno(), "pidfd_send_signal failed")


def require(condition, reason):
    if not condition:
        raise RuntimeError(reason)


def digest(path):
    with open(path, "rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def identity(pid):
    # comm can contain spaces and closing parentheses; starttime is field 22.
    fields = Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()
    require(fields[0] != "Z", "daemon ended")
    return {"pid": pid, "boot": Path("/proc/sys/kernel/random/boot_id").read_text().strip(),
            "ticks": fields[19], "sha256": digest(f"/proc/{pid}/exe")}


def connect(endpoint):
    conn = http.client.HTTPConnection("localhost", timeout=3)
    conn.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    conn.sock.settimeout(3)
    try:
        conn.sock.connect(str(endpoint))
        return conn
    except BaseException:
        conn.close()
        raise


def rpc(conn, method, payload=None):
    # Never let HTTPConnection silently reconnect to a replacement endpoint.
    require(conn.sock is not None, "connection ended; validate again before reconnecting")
    conn.request("POST", "/hovel.daemon.v1.DaemonService/" + method,
                 json.dumps(payload or {}), {"Content-Type": "application/json"})
    response = conn.getresponse()
    body = response.read(1024 * 1024 + 1)
    require(response.status == 200 and len(body) <= 1024 * 1024, "RPC rejected")
    return json.loads(body)


def inspect(conn, pid, expected_sha, workspace):
    peer, uid, _ = struct.unpack("3i", conn.sock.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))
    require((peer, uid) == (pid, os.getuid()), "socket belongs to another process/user")
    before = identity(pid)
    require(before["sha256"] == expected_sha, "running executable differs from pinned artifact")
    info = rpc(conn, "GetDaemonInfo")
    require(info["pid"] == pid and info["workspacePath"] == str(workspace), "workspace/process mismatch")
    require(identity(pid) == before, "process changed during verification")
    return before | {"workspace": str(workspace), "startedAt": info["startedAt"]}


def load_record(path):
    with open(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK), "r") as source:
        metadata = os.fstat(source.fileno())
        require(stat.S_ISREG(metadata.st_mode) and metadata.st_uid == os.getuid()
                and metadata.st_mode & 0o077 == 0, "launch record must be an owner-only regular file")
        body = source.read(8193)
        require(len(body) <= 8192, "oversized launch record")
        return json.loads(body)


def attach(workspace, expected_sha, method="ListSessions", payload=None):
    record = load_record(workspace / "burrow-launch-prototype.json")
    pid = record["pid"]
    require(type(pid) is int and 0 < pid < 2**31, "invalid recorded PID")
    pidfd = pidfd_open(pid)
    conn = None
    try:
        require(not select.select([pidfd], [], [], 0)[0], "recorded process ended")
        conn = connect(workspace / "hoveld.sock")
        require(inspect(conn, pid, expected_sha, workspace) == record, "stale launch record")
        require(not select.select([pidfd], [], [], 0)[0], "process ended during verification")
        # The operation stays on the socket whose peer was verified.
        return rpc(conn, method, payload)
    finally:
        if conn:
            conn.close()
        os.close(pidfd)


def launch(root, hovel, archive):
    workspace = root / "workspace"
    endpoint = workspace / "hoveld.sock"
    require(not endpoint.exists(), "endpoint already exists; refusing automatic replacement")
    with (root / "daemon.log").open("w") as log:
        daemon = subprocess.Popen([str(hovel), "daemon", "serve", "--workspace", str(workspace)],
                                  stdout=log, stderr=log, stdin=subprocess.DEVNULL, start_new_session=True)
    try:
        for _ in range(200):
            require(daemon.poll() is None, "daemon failed to start")
            if endpoint.exists():
                break
            time.sleep(0.05)
        conn = connect(endpoint)
        try:
            record = inspect(conn, daemon.pid, digest(hovel), workspace)
        finally:
            conn.close()
        path = workspace / "burrow-launch-prototype.json"
        with open(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w") as output:
            json.dump(record, output)

        def cli(*args, operator=False):
            prefix = [str(hovel), "run", "--workspace", str(workspace), "--daemon-endpoint", str(endpoint)]
            if operator:
                prefix += ["--op", "proof", "--chain", "proof"]
            return subprocess.run(prefix + ["--", *args], text=True, capture_output=True,
                                  check=True, timeout=20).stdout

        cli("module", "install", archive)
        cli("op", "create", "proof")
        cli("chain", "create", "proof", operator=True)
        cli("chain", "add", "burrow-sdk-prototype@0.0.0", operator=True)
        cli("target", "add", "mock://inert", operator=True)
        result = json.loads(cli("throw", "--now", "--json", operator=True))["results"][0]
        require(result["state"] == "succeeded", "fixture failed")
        print(json.dumps({"record": record, "session": result["sessions"][0]["id"],
                          "module": int(result["summary"].split("pid=")[1])}), flush=True)
        # Frontend exits; daemon and session intentionally remain alive.
    except BaseException:
        daemon.terminate()
        daemon.wait(timeout=10)
        raise


def input_path(value):
    path = Path(value)
    if value.startswith("external/"):
        path = Path(__file__).parents[3] / value.removeprefix("external/")
    return str(path.resolve())


def check(wheel, archive):
    require(digest(wheel) == WHEEL_SHA, "package checksum mismatch")
    with tempfile.TemporaryDirectory(prefix="burrow-reuse-") as scratch:
        root = Path(scratch)
        hovel = root / "hovel"
        with zipfile.ZipFile(wheel) as package:
            hovel.write_bytes(package.read("hovel/bin/hovel"))
        hovel.chmod(0o700)
        expected_sha = digest(hovel)
        env = {k: v for k, v in os.environ.items() if not k.startswith("HOVEL_")}
        env.update(HOME=str(root), XDG_CONFIG_HOME=str(root / "config"), XDG_DATA_HOME=str(root / "data"))
        workspace = root / "workspace"
        path = workspace / "burrow-launch-prototype.json"
        pidfd = None
        other = None
        try:
            first = subprocess.run([sys.executable, __file__, "launch", str(root), str(hovel), archive],
                                   env=env, cwd=root, text=True, capture_output=True, timeout=60)
            require(first.returncode == 0, first.stderr)
            state = json.loads(first.stdout)
            record = state["record"]
            pidfd = pidfd_open(record["pid"])
            original = path.read_text()

            def client(ok=True, pin=expected_sha):
                outcome = subprocess.run([sys.executable, __file__, "attach", str(workspace), pin],
                                         env=env, cwd=root, text=True, capture_output=True, timeout=15)
                require((outcome.returncode == 0) == ok, outcome.stdout + outcome.stderr)
                if ok:
                    require(state["session"] in outcome.stdout, "retained session missing")
                else:
                    require("REFUSED" in outcome.stderr, outcome.stderr)
                return outcome

            client()
            client(False, pin="0" * 64)
            attach(workspace, expected_sha, "WriteSession",
                   {"SessionID": state["session"], "Data": base64.b64encode(b"x").decode()})
            received = b""
            for _ in range(20):
                chunk = attach(workspace, expected_sha, "ReadSession",
                               {"SessionID": state["session"], "TimeoutMs": 100})
                received += base64.b64decode(chunk.get("Data", ""))
                if b"byte=78" in received:
                    break
            require(b"byte=78" in received, "retained session does not respond")
            print("PASS launcher exits; new frontend verifies same daemon/session and live input/output", flush=True)

            for field, value in (("pid", os.getpid()), ("ticks", "0"), ("boot", "stale"),
                                 ("sha256", "0" * 64), ("workspace", str(root / "absent")),
                                 ("startedAt", "stale")):
                path.write_text(json.dumps(record | {field: value}))
                client(False)
            for body in ("{", "{}", "x" * 8193):
                path.write_text(body)
                client(False)
            path.write_text(original)
            path.chmod(0o644)
            client(False)
            path.chmod(0o600)
            path.rename(path.with_suffix(".saved"))
            client(False)
            path.symlink_to(path.with_suffix(".saved"))
            client(False)
            path.unlink()
            path.with_suffix(".saved").rename(path)
            client()
            require(not (root / "absent").exists(), "failure selected another workspace")
            print("PASS stale PID/start/boot/hash/workspace/discovery identity; malformed, missing, unsafe and symlink records refuse", flush=True)

            # Changing installed bytes must neither change nor establish running provenance.
            hovel.rename(root / "running-hovel")
            hovel.write_bytes(b"replaced installed executable")
            client()
            print("PASS installed-path replacement does not misidentify retained running executable", flush=True)

            endpoint = workspace / "hoveld.sock"
            parked = workspace / "retained.sock"
            endpoint.rename(parked)
            with (root / "other.log").open("w") as log:
                other = subprocess.Popen([str(root / "running-hovel"), "daemon", "serve", "--workspace",
                                          str(root / "other"), "--listen", str(endpoint)],
                                         env=env, stdout=log, stderr=log, stdin=subprocess.DEVNULL)
            for _ in range(200):
                require(other.poll() is None, (root / "other.log").read_text())
                if endpoint.exists():
                    break
                time.sleep(0.05)
            client(False)
            require(other.poll() is None and not select.select([pidfd], [], [], 0)[0], "refusal stopped a daemon")
            require(path.read_text() == original, "refusal rewrote provenance")
            other.terminate()
            other.wait(timeout=10)
            endpoint.unlink(missing_ok=True)
            parked.rename(endpoint)
            client()
            print("PASS unrelated pinned daemon at expected endpoint rejected; both processes preserved", flush=True)

            attach(workspace, expected_sha, "CloseSession", {"SessionID": state["session"]})
            for _ in range(200):
                if not Path(f"/proc/{state['module']}").exists():
                    break
                time.sleep(0.05)
            require(not Path(f"/proc/{state['module']}").exists(), "module survived explicit close")
            terminate(pidfd)
            require(select.select([pidfd], [], [], 10)[0], "daemon survived explicit shutdown")
            client(False)
            require(path.read_text() == original, "dead-process refusal rewrote provenance")
            print("PASS explicit session close and daemon shutdown; ended-process record refuses without restart", flush=True)
            # Explicit harness replacement, never a client recovery action.
            with (root / "replacement.log").open("w") as log:
                other = subprocess.Popen([str(root / "running-hovel"), "daemon", "serve", "--workspace",
                                          str(workspace)], env=env, stdout=log, stderr=log,
                                         stdin=subprocess.DEVNULL)
            for _ in range(200):
                require(other.poll() is None, "replacement failed to start")
                if endpoint.exists():
                    break
                time.sleep(0.05)
            client(False)
            require(other.poll() is None and path.read_text() == original, "replacement refusal changed daemon/record")
            print("PASS explicitly restarted pinned daemon in same workspace rejects old launch identity", flush=True)
        finally:
            if other and other.poll() is None:
                other.terminate()
                other.wait(timeout=10)
            if pidfd is not None:
                if not select.select([pidfd], [], [], 0)[0]:
                    terminate(pidfd)
                    require(select.select([pidfd], [], [], 10)[0], "cleanup failed")
                os.close(pidfd)


if __name__ == "__main__":
    try:
        if sys.argv[1] == "launch":
            launch(Path(sys.argv[2]), Path(sys.argv[3]), sys.argv[4])
        elif sys.argv[1] == "attach":
            print(json.dumps(attach(Path(sys.argv[2]), sys.argv[3])))
        else:
            check(*map(input_path, sys.argv[1:]))
    except (RuntimeError, OSError, ValueError, KeyError, TypeError, http.client.HTTPException) as error:
        print(f"REFUSED: {error}. No automatic attach/restart/replacement/workspace fallback.", file=sys.stderr)
        sys.exit(1)
