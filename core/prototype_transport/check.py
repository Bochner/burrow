"""Throwaway Linux transport check. No operator SSH configuration or remote lab used."""
import base64
import hashlib
import http.client
import json
import os
from pathlib import Path
import pwd
import signal
import sqlite3
import socket
import subprocess
import sys
import tempfile
import threading
import time
import zipfile


def input_path(value):
    path = Path(value)
    if value.startswith("external/"):
        path = Path(__file__).parents[3] / value.removeprefix("external/")
    return str(path.resolve())


probe, archive, wheel = map(input_path, sys.argv[1:])
# Explicit host prerequisite: Ubuntu 10.2p1-2ubuntu3.6. These are not hermetic
# toolchains; upgrades must change this evidence pin and rerun the proof.
pins = {
    "/usr/bin/ssh": "c6942676b14478f22ccad4b62c40033d18fae3212b5777fd6941aa6e4db1e183",
    "/usr/sbin/sshd": "b5955817a5c6efae0ed00a93033a92cdb73c401712dff996876048391cd12371",
    "/usr/bin/ssh-keygen": "13400171bcdbc5d7d29cafa4305679f5eab78f8f0cb42dba2443f7a04c92aea5",
    "/usr/bin/ssh-agent": "ec957b44fefd3ab45bb5a5c06154a74981498785b5e895a40c3aeea19fd7b540",
    "/usr/bin/ssh-add": "8350fd50ec25ef347a0026f23c6c30dc3fba149aface06e2594a91a98197f9b9",
}
for path, digest in pins.items():
    assert hashlib.sha256(Path(path).read_bytes()).hexdigest() == digest, f"host prerequisite changed: {path}"


def wait_for(check):
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        if check():
            return
        time.sleep(0.05)
    raise AssertionError("fixture transition timed out")


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


with tempfile.TemporaryDirectory(prefix="burrow-ssh-", dir=Path.home() / ".cache") as scratch:
    root = Path(scratch)
    processes = []
    env = {k: v for k, v in os.environ.items() if not k.startswith(("SSH_", "HOVEL_"))}
    env.update(XDG_CONFIG_HOME=str(root / "config"), XDG_DATA_HOME=str(root / "data"))
    # Synthetic passphrase exists only in this temporary, owner-only fixture.
    passphrase = os.urandom(24).hex()
    secret = root / "secret"
    secret.write_text(passphrase)
    secret.chmod(0o600)
    askpass = root / "askpass"
    askpass.write_text("#!/bin/sh\nexec /bin/cat '" + str(secret) + "'\n")
    askpass.chmod(0o700)
    env.update(SSH_ASKPASS=str(askpass), SSH_ASKPASS_REQUIRE="force", DISPLAY=":fixture")

    def run(args, *, success=True, input=None, timeout=25, extra=None):
        result = subprocess.run(list(map(str, args)), cwd=root, env=env | (extra or {}), input=input,
                                capture_output=True, text=True, timeout=timeout)
        assert passphrase not in result.stdout + result.stderr, "secret leaked"
        assert (result.returncode == 0) == success, (args, result.returncode, result.stdout, result.stderr,
            (root / "master.log").read_text() if (root / "master.log").exists() else "")
        return result

    def spawn(args, name, extra=None, **kwargs):
        log = (root / (name + ".log")).open("w")
        p = subprocess.Popen(list(map(str, args)), cwd=root, env=env | (extra or {}),
                             stdout=log, stderr=log, start_new_session=True, **kwargs)
        log.close()
        processes.append(p)
        return p

    ssh_config = root / "ssh_config"
    master_socket = root / "my-lab"
    ssh = ["/usr/bin/ssh", "-F", ssh_config]

    def control(*args, success=True):
        return run(ssh + ["-S", master_socket, *args, "target"], success=success)

    def reuse(command, success=True):
        # OpenSSH normally falls back to a new login if the socket disappears.
        # ProxyCommand=false makes that fallback fail even during a check/use race.
        return run(ssh + ["-S", master_socket, "-o", "ProxyCommand=/bin/false", "target", command], success=success)

    try:
        for name in ("host", "client", "encrypted"):
            # Feed the generated passphrase on stdin, never on the command line.
            args = ["/usr/bin/ssh-keygen", "-q", "-t", "ed25519", "-f", root / name]
            if name == "encrypted":
                run(args, input=passphrase + "\n" + passphrase + "\n")
            else:
                run(args + ["-N", ""])
        authorized = root / "authorized_keys"
        authorized.write_text((root / "client.pub").read_text() + (root / "encrypted.pub").read_text())
        port = free_port()
        config = root / "sshd_config"
        config.write_text(f"""ListenAddress 127.0.0.1
Port {port}
HostKey {root / 'host'}
PidFile {root / 'sshd.pid'}
AuthorizedKeysFile {authorized}
StrictModes yes
PasswordAuthentication no
KbdInteractiveAuthentication no
UsePAM no
AllowTcpForwarding yes
Subsystem sftp internal-sftp
LogLevel VERBOSE
""")
        server = spawn(["/usr/sbin/sshd", "-D", "-e", "-f", config], "sshd")

        def listening():
            assert server.poll() is None, (root / "sshd.log").read_text()
            try:
                with socket.create_connection(("127.0.0.1", port), timeout=.1):
                    return True
            except OSError:
                return False
        wait_for(listening)
        known = root / "known_hosts"
        # Explicit trust is based on the generated fixture key, not ssh-keyscan.
        hostkey = (root / "host.pub").read_text().split()
        trusted = f"[127.0.0.1]:{port} {hostkey[0]} {hostkey[1]}\n"
        ssh_config.write_text(f"""Host target jump
  HostName 127.0.0.1
  Port {port}
  User {pwd.getpwuid(os.getuid()).pw_name}
  IdentityFile {root / 'client'}
  IdentitiesOnly yes
  UserKnownHostsFile {known}
  GlobalKnownHostsFile /dev/null
  StrictHostKeyChecking yes
  BatchMode yes
  ConnectTimeout 3
  ControlMaster no
  ControlPersist no
  LogLevel ERROR
""")
        run(ssh + ["target", "true"], success=False)
        known.write_text(trusted)
        assert run(ssh + ["target", "printf trusted"]).stdout == "trusted"
        known.write_text(f"[127.0.0.1]:{port} " + " ".join((root / "client.pub").read_text().split()[:2]) + "\n")
        changed = run(ssh + ["target", "true"], success=False)
        assert "HOST IDENTIFICATION HAS CHANGED" in changed.stderr
        known.write_text(trusted)
        print("PASS unknown host rejected, explicit fixture-key trust, changed key rejected", flush=True)

        # Fresh config without a default identity tests encrypted-key/agent paths alone.
        auth_config = root / "auth_config"
        auth_config.write_text(ssh_config.read_text().replace(f"  IdentityFile {root / 'client'}\n", ""))
        auth = ["/usr/bin/ssh", "-F", auth_config]
        assert run(auth + ["-o", "BatchMode=no", "-i", root / "encrypted", "target", "printf encrypted"]).stdout == "encrypted"
        run(auth + ["-o", "BatchMode=no", "-i", root / "encrypted", "target", "true"],
            success=False, extra={"SSH_ASKPASS": "/bin/false"})
        agent_socket = root / "agent"
        agent = spawn(["/usr/bin/ssh-agent", "-D", "-a", agent_socket], "agent")
        wait_for(agent_socket.exists)
        agent_env = {"SSH_AUTH_SOCK": str(agent_socket)}
        run(["/usr/bin/ssh-add", root / "encrypted"], extra=agent_env)
        assert run(auth + ["-o", "IdentitiesOnly=no", "target", "printf agent"], extra=agent_env).stdout == "agent"
        run(auth + ["-o", "IdentitiesOnly=no", "target", "true"], success=False,
            extra={"SSH_AUTH_SOCK": str(root / "missing-agent")})
        print("PASS encrypted key, cancelled passphrase, agent, unavailable agent", flush=True)
        for method, flags, extra in (
            ("encrypted", ["-o", "BatchMode=no", "-i", root / "encrypted"], {}),
            ("agent", ["-o", "IdentitiesOnly=no"], agent_env),
        ):
            auth_socket = root / (method + "-master")
            auth_master = spawn(auth + flags + ["-M", "-S", auth_socket, "-N", "target"], method + "-master", extra=extra)
            wait_for(lambda: auth_socket.exists() or auth_master.poll() is not None)
            assert auth_master.poll() is None
            identity = run(auth + ["-S", auth_socket, "-o", "ProxyCommand=/bin/false", "target", "printf '%s' \"$SSH_CONNECTION\""]).stdout
            assert run([probe, "exec", auth_socket]).stdout == identity
            run(auth + ["-S", auth_socket, "-O", "exit", "target"])
            auth_master.wait(timeout=5)
        print("PASS encrypted-key and agent masters reused by external and Go clients", flush=True)

        password_server = spawn([probe, "password-server", root], "password-server")
        password_port = root / "password-port"
        wait_for(password_port.exists)
        password_address = password_port.read_text().split(":")[-1]
        with known.open("a") as trust:
            trust.write(f"[127.0.0.1]:{password_address} {hostkey[0]} {hostkey[1]}\n")
        password_socket = root / "password-master"
        password_args = auth + ["-p", password_address, "-o", "BatchMode=no", "-o", "PreferredAuthentications=password"]
        run(password_args + ["target", "fixed-response"], success=False, extra={"SSH_ASKPASS": "/bin/false"})
        password_master = spawn(password_args + ["-M", "-S", password_socket, "-N", "target"], "password-master")
        wait_for(lambda: password_socket.exists() or password_master.poll() is not None)
        assert password_master.poll() is None
        assert run(auth + ["-S", password_socket, "-o", "ProxyCommand=/bin/false", "target", "fixed-response"]).stdout == "password-ok"
        assert run([probe, "exec", password_socket]).stdout == "password-ok"
        run(auth + ["-S", password_socket, "-O", "exit", "target"])
        password_master.wait(timeout=5)
        print("PASS password master authentication, cancelled password, external and Go reuse (fixed-response server)", flush=True)

        # Same controlled server serves jump and destination roles; two real SSH transports.
        assert run(ssh + ["-J", "jump", "target", "printf jumped"]).stdout == "jumped"
        print("PASS SSH alias and ProxyJump", flush=True)

        master = spawn(ssh + ["-M", "-S", master_socket, "-N", "target"], "master")
        wait_for(lambda: master_socket.exists() or master.poll() is not None)
        assert master.poll() is None, (root / "master.log").read_text()
        assert root.stat().st_mode & 0o077 == 0 and master_socket.stat().st_mode & 0o077 == 0
        print("SOCKET", master_socket, flush=True)
        master_socket.chmod(0o666)
        run([probe, "exec", master_socket], success=False)
        master_socket.chmod(0o600)
        link = root / "socket-link"
        link.symlink_to(master_socket)
        run([probe, "exec", link], success=False)
        print("PASS Go bridge rejects permissive socket and symlink", flush=True)
        identity = reuse("printf '%s' \"$SSH_CONNECTION\"").stdout
        assert run([probe, "exec", master_socket]).stdout == identity
        external = spawn(ssh + ["-S", master_socket, "-o", "ProxyCommand=/bin/false", "target", "sleep 2; printf external"], "external")
        assert run([probe, "exec", master_socket]).stdout == identity
        assert external.wait(timeout=5) == 0 and (root / "external.log").read_text() == "external"
        assert control("-O", "check").returncode == 0
        print("PASS concurrent external shell + Go channel share SSH_CONNECTION; shell exit retains master", flush=True)
        # Collision preflight is mandatory: OpenSSH itself may disable multiplexing and log in again.
        try:
            fd = os.open(master_socket, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        except FileExistsError:
            pass
        else:
            os.close(fd)
            raise AssertionError("collision accepted")
        assert run([probe, "exec", master_socket]).stdout == identity
        print("PASS collision rejected before launch; existing connection unchanged", flush=True)
        print(run([probe, "pty", master_socket]).stdout, end="", flush=True)
        files = root / "files"
        files.mkdir()
        print(run([probe, "sftp", master_socket, files]).stdout, end="", flush=True)
        print("SUBPROCESS SFTP:", run([probe, "pipe-sftp", master_socket, files, ssh_config]).stdout, end="", flush=True)
        assert run([probe, "exec", master_socket]).stdout == identity

        echo = socket.socket()
        echo.bind(("127.0.0.1", 0))
        echo.listen()
        echo.settimeout(.2)
        echo_port = echo.getsockname()[1]
        stop = threading.Event()

        def echo_loop():
            while not stop.is_set():
                try:
                    client, _ = echo.accept()
                except socket.timeout:
                    continue
                with client:
                    client.settimeout(2)
                    try:
                        client.sendall(client.recv(16))
                    except OSError:
                        pass
        thread = threading.Thread(target=echo_loop, daemon=True)
        thread.start()
        print(run([probe, "direct", master_socket, f"127.0.0.1:{echo_port}"]).stdout, end="", flush=True)
        assert control("-O", "check").returncode == 0
        # Each potentially destructive bridge probe gets its own master.
        refusal_guard = socket.socket()
        refusal_guard.bind(("127.0.0.1", 0))
        refusal_address = f"127.0.0.1:{refusal_guard.getsockname()[1]}"
        for mode in ("refused", "remote"):
            separate_socket = root / mode
            separate = spawn(ssh + ["-M", "-S", separate_socket, "-N", "target"], mode)
            wait_for(separate_socket.exists)
            result = subprocess.run([probe, mode, str(separate_socket), refusal_address], cwd=root, env=env,
                                    capture_output=True, text=True, timeout=25)
            if mode == "refused":
                assert result.returncode == 0 and "visible Go direct-tcpip refusal" in result.stdout
                assert separate.wait(timeout=5) == 255
                assert "no remote_id" in (root / (mode + ".log")).read_text()
            else:
                assert result.returncode == 2 and "probe timed out" in result.stderr
            print("GO", mode, "exit=", result.returncode, "master=", separate.poll(),
                  result.stdout, result.stderr, "MASTER LOG", (root / (mode + ".log")).read_text(), flush=True)
            if separate.poll() is None and separate_socket.exists():
                run(ssh + ["-S", separate_socket, "-O", "exit", "target"])
                separate.wait(timeout=5)
        for kind in ("L", "R", "D"):
            local = free_port()
            spec = f"127.0.0.1:{local}" + ("" if kind == "D" else f":127.0.0.1:{echo_port}")
            control("-O", "forward", "-" + kind, spec)
            with socket.create_connection(("127.0.0.1", local), timeout=2) as stream:
                if kind == "D":
                    stream.sendall(b"\x05\x01\x00")
                    assert stream.recv(2) == b"\x05\x00"
                    stream.sendall(b"\x05\x01\x00\x01\x7f\x00\x00\x01" + echo_port.to_bytes(2, "big"))
                    response = stream.recv(10)
                    assert response[:2] == b"\x05\x00", response
                stream.sendall(b"ping")
                assert stream.recv(4) == b"ping"
            control("-O", "cancel", "-" + kind, spec)
            try:
                with socket.create_connection(("127.0.0.1", local), timeout=.2):
                    raise AssertionError("cancelled listener still open")
            except ConnectionRefusedError:
                pass
            assert run([probe, "exec", master_socket]).stdout == identity
            print(f"PASS OpenSSH {kind} forward, data, removal, parent retained", flush=True)
        control("-O", "forward", "-L", f"127.0.0.1:{echo_port}:127.0.0.1:{echo_port}", success=False)
        control("-O", "forward", "-R", f"127.0.0.1:{echo_port}:127.0.0.1:{echo_port}", success=False)
        stop.set()
        thread.join(timeout=3)
        echo.close()
        print("PASS visible local/remote bind failures", flush=True)
        refused = run(ssh + ["-S", master_socket, "-o", "ProxyCommand=/bin/false", "-W", refusal_address, "target"], success=False)
        assert "failed" in refused.stderr.lower()
        assert run([probe, "exec", master_socket]).stdout == identity
        print("PASS subprocess forwarding refusal returns failure and retains master", flush=True)
        refusal_guard.close()

        # Actual published Hovel daemon and packaged public SDK adapter.
        hovel = root / "hovel"
        assert hashlib.sha256(Path(wheel).read_bytes()).hexdigest() == "7edccdabe04e30098e417d27ab48064c11a8612149124b7df0797251e88a0933"
        with zipfile.ZipFile(wheel) as package:
            hovel.write_bytes(package.read("hovel/bin/hovel"))
        hovel.chmod(0o700)
        workspace = root / "workspace"
        env["BURROW_PROOF_SOCKET"] = str(master_socket)
        daemon = spawn([hovel, "daemon", "serve", "--workspace", workspace], "hovel")
        wait_for(lambda: (workspace / "hoveld.sock").exists() or daemon.poll() is not None)
        assert daemon.poll() is None, (root / "hovel.log").read_text()

        def cli(*args, operator=False):
            prefix = [hovel, "run", "--workspace", workspace]
            if operator:
                prefix += ["--op", "proof", "--chain", "proof"]
            return run(prefix + ["--", *args]).stdout

        def rpc(method, data):
            conn = http.client.HTTPConnection("localhost", timeout=3)
            conn.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            conn.sock.settimeout(3)
            conn.sock.connect(str(workspace / "hoveld.sock"))
            conn.request("POST", "/hovel.daemon.v1.DaemonService/" + method, json.dumps(data), {"Content-Type": "application/json"})
            response = conn.getresponse()
            body = response.read()
            conn.close()
            return response.status, json.loads(body) if response.status == 200 else body.decode(errors="replace")

        cli("module", "install", archive)
        cli("op", "create", "proof")
        cli("chain", "create", "proof", operator=True)
        cli("chain", "add", "burrow-transport-prototype@0.0.0", operator=True)
        cli("target", "add", "ssh://controlled-fixture", operator=True)
        for attempt in range(2):
            result = json.loads(cli("throw", "--now", "--json", operator=True))["results"][0]
            assert result["state"] == "succeeded", result
            session = result["sessions"][0]["id"]
            # Independent RPC requests act as frontend disconnect/reconnect.
            status, body = rpc("WriteSession", {"SessionId": session, "Data": base64.b64encode(b"stty size; printf HOVEL_END\\n\n").decode()})
            assert status == 200, body
            output = b""
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                status, body = rpc("ReadSession", {"SessionId": session, "TimeoutMs": 100})
                assert status == 200, body
                output += base64.b64decode(body.get("Data", "") or "")
                if b"HOVEL_END" in output:
                    break
            assert b"24 80" in output, (output, body)
            status, body = rpc("ResizeSession", {"SessionId": session, "Rows": 40, "Cols": 120})
            assert status != 200, body
            assert reuse("printf alive").stdout == "alive"
            status, body = rpc("CloseSession", {"SessionID": session})
            assert status == 200, body
            assert run([probe, "exec", master_socket]).stdout == identity
        print("PASS Hovel SSH PTY open/read/write/frontend disconnect/close/reopen; OBSERVED resize method unavailable", flush=True)
        live = json.loads(cli("throw", "--now", "--json", operator=True))["results"][0]["sessions"][0]["id"]
        loss = subprocess.Popen([probe, "loss", str(master_socket), str(files)], cwd=root, env=env,
                                stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        processes.append(loss)
        while True:
            line = loss.stdout.readline()
            assert line, loss.stderr.read()
            print(line, end="", flush=True)
            if "READY FOR MASTER LOSS" in line:
                break
        control("-O", "exit")
        master.wait(timeout=5)
        output, errors = loss.communicate("continue\n", timeout=10)
        assert loss.returncode == 0, (output, errors)
        print(output, end="", flush=True)
        reuse("printf must-not-run", success=False)
        assert not master_socket.exists()
        def hovel_closed():
            status, body = rpc("ReadSession", {"SessionID": live, "TimeoutMs": 100})
            assert status == 200, body
            return body["Closed"]
        wait_for(hovel_closed)
        print("PASS active Hovel SSH session observes master loss as closed", flush=True)
        print("PASS external master termination observed; missing socket cannot silently log in again", flush=True)

        # Ownership investigation: a new daemon inherits only the inert fixture config.
        daemon.terminate()
        daemon.wait(timeout=10)
        owner_root = root / "owners"
        owner_root.mkdir(mode=0o700)
        env.update(BURROW_OWNER_ROOT=str(owner_root), BURROW_OWNER_CONFIG=str(ssh_config))
        workspace = root / "owner-workspace"
        daemon = spawn([hovel, "daemon", "serve", "--workspace", workspace], "owner-hovel")
        wait_for(lambda: (workspace / "hoveld.sock").exists())
        cli("module", "install", archive)
        cli("op", "create", "proof")
        cli("chain", "create", "proof", operator=True)
        cli("chain", "add", "burrow-transport-prototype@0.0.0", operator=True)
        cli("target", "add", "ssh://controlled-fixture", operator=True)

        def owned_run():
            result = json.loads(cli("throw", "--now", "--allow-dangerous", "--json", operator=True))["results"][0]
            assert result["state"] == "succeeded", result
            return result, result["sessions"][0]["id"]

        denied = run([hovel, "run", "--workspace", workspace, "--op", "proof", "--chain", "proof", "--", "throw", "--now", "--json"], success=False)
        assert "dangerous" in denied.stderr.lower() + denied.stdout.lower()
        assert not (owner_root / "gateway").exists()
        print("PASS dangerous operation refuses before master startup without explicit allowance", flush=True)

        def alive(pid):
            stat = Path(f"/proc/{pid}/stat")
            return stat.exists() and stat.read_text().rsplit(")", 1)[1].split()[0] != "Z"

        for mode in ("close", "module-kill", "master-loss", "log-ceiling", "bounded-logs", "daemon-kill"):
            result, session = owned_run()
            owned_socket = owner_root / "gateway" / "master"
            owner_pid, master_pid = [int(field.split("=")[1]) for field in result["summary"].split()]
            run_id = result["runId"]
            clients = []
            forwards = []
            owned = ssh + ["-S", owned_socket, "-o", "ProxyCommand=/bin/false"]
            try:
                assert alive(owner_pid) and alive(master_pid)
                assert owned_socket.stat().st_mode & 0o077 == 0
                with sqlite3.connect(workspace / "workspace.db") as db:
                    artifacts = list(db.execute("select path, sha256 from artifacts where run_id = ?", (run_id,)))
                    plans = [json.loads(row[0]) for row in db.execute("select plan_json from throw_plans")]
                assert artifacts and plans and all(p["confirmationId"] for p in plans)
                artifact_bytes = [(workspace / path, (workspace / path).read_bytes(), digest) for path, digest in artifacts]
                for path, data, digest in artifact_bytes:
                    assert hashlib.sha256(data).hexdigest() == digest
                    assert path.is_relative_to(workspace)
                if mode in ("close", "bounded-logs"):
                    collision = json.loads(cli("throw", "--now", "--allow-dangerous", "--json", operator=True))["results"][0]
                    assert collision["state"] != "succeeded", collision
                    assert alive(master_pid)
                for kind in ("L", "R", "D"):
                    local = free_port()
                    spec = f"127.0.0.1:{local}" + ("" if kind == "D" else f":127.0.0.1:{port}")
                    run(owned + ["-O", "forward", "-" + kind, spec, "target"])
                    forwards.append(local)
                for i in range(2):
                    client = spawn(owned + ["-tt", "target", "sleep 60"], f"owner-shell-{mode}-{i}")
                    clients.append(client)
                assert run(owned + ["target", "printf retained"]).stdout == "retained"
                status, body = rpc("WriteSession", {"SessionID": session, "Data": base64.b64encode(b"log\n").decode()})
                assert status == 200, body
                def logged():
                    with sqlite3.connect(workspace / "workspace.db") as db:
                        rows = list(db.execute("select timestamp from events where run_id = ? and message = ?", (run_id, "retained-owner-diagnostic")))
                    return bool(rows) and all(row[0] for row in rows)
                wait_for(logged)
                print("PASS daemon-owned master with no required shell; two clients and three tunnels; diagnostics after Run", flush=True)
                if mode == "bounded-logs":
                    status, body = rpc("WriteSession", {"SessionID": session, "Data": base64.b64encode(b"bounded-logs\n").decode()})
                    assert status == 200, body
                    with sqlite3.connect(workspace / "workspace.db") as db:
                        count = db.execute("select count(*) from events where run_id = ? and message = 'diagnostic budget exhausted; further milestones suppressed'", (run_id,)).fetchone()[0]
                    assert count == 1, count
                    print("PASS 600 bounded diagnostic attempts warn once without breaking control", flush=True)
                if mode in ("close", "bounded-logs"):
                    status, body = rpc("CloseSession", {"SessionID": session})
                    assert status == 200, body
                    wait_for(lambda: not alive(master_pid))
                    for client in clients: client.wait(timeout=5)
                    for local in forwards:
                        with socket.socket() as sock:
                            assert sock.connect_ex(("127.0.0.1", local)) != 0
                    assert not owned_socket.exists()
                    print("PASS explicit close ends master, both clients, all three listeners, socket", flush=True)
                elif mode == "module-kill":
                    os.kill(owner_pid, signal.SIGKILL)
                    wait_for(lambda: not alive(owner_pid))
                    wait_for(lambda: not alive(master_pid))
                    for client in clients: client.wait(timeout=5)
                    for local in forwards:
                        with socket.socket() as sock: assert sock.connect_ex(("127.0.0.1", local)) != 0
                    print("PASS module SIGKILL: Linux parent-death signal ends master, clients and listeners", flush=True)
                elif mode == "master-loss":
                    run(owned + ["-O", "exit", "target"])
                    wait_for(lambda: not alive(master_pid))
                    run(owned + ["target", "printf forbidden-fallback"], success=False)
                    print("PASS external master loss: no fallback login or automatic reconnect", flush=True)
                elif mode == "daemon-kill":
                    daemon.kill()
                    daemon.wait(timeout=5)
                    wait_for(lambda: not alive(owner_pid))
                    wait_for(lambda: not alive(master_pid))
                    for client in clients: client.wait(timeout=5)
                    for local in forwards:
                        with socket.socket() as sock: assert sock.connect_ex(("127.0.0.1", local)) != 0
                    print("PASS daemon SIGKILL: SDK stream EOF wrapper closes owned master and listeners", flush=True)
                else:
                    try:
                        rpc("WriteSession", {"SessionID": session, "Data": base64.b64encode(b"log-ceiling\n").decode()})
                    except (TimeoutError, OSError):
                        pass
                    def closed_by_protocol():
                        status, body = rpc("ReadSession", {"SessionID": session, "TimeoutMs": 50})
                        return status == 200 and body.get("Closed")
                    wait_for(closed_by_protocol)
                    assert run(owned + ["target", "printf protocol-orphan"]).stdout == "protocol-orphan"
                    for local in forwards:
                        with socket.create_connection(("127.0.0.1", local), timeout=1): pass
                    status, failure = rpc("CloseSession", {"SessionID": session})
                    assert status != 200 and "notification count exceeds maximum 256" in str(failure), (status, failure)
                    with sqlite3.connect(workspace / "workspace.db") as db:
                        count = db.execute("select count(*) from events where run_id = ? and type = 'hovel.module.log'", (run_id,)).fetchone()[0]
                    assert count == 256, count
                    print("OBSERVED GAP: exactly 256 stored module logs; next notification marks session closed with live master/listeners; explicit CloseSession returns error (cleanup may still execute)", flush=True)
                for path, data, digest in artifact_bytes:
                    assert path.read_bytes() == data
                print("PASS Hovel-recorded confirmation and artifact hashes/paths; evidence survives resource close/loss", flush=True)
            finally:
                if owned_socket.exists():
                    run(owned + ["-O", "exit", "target"])
                wait_for(lambda: not alive(master_pid))
                for client in clients: client.wait(timeout=5)
                if alive(owner_pid): os.kill(owner_pid, signal.SIGKILL)
                # Only remove this fixture's empty reservation after verified master exit.
                if owned_socket.parent.exists(): owned_socket.parent.rmdir()
    finally:
        for process in reversed(processes):
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=3)
    print("PASS transport fixture processes reaped and scratch resources removed on exit", flush=True)
