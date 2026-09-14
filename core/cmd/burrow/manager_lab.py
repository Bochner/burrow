"""Production manager acceptance through CLI and public daemon controls."""
import hashlib
import fcntl
import http.client
import json
import os
from pathlib import Path
import socket
import subprocess
import time
import uuid


def manager_checks(binary, w, root, env, port, key, first, burrow, wait):
    def rpc(command, args=(), session=None):
        connection = http.client.HTTPConnection("localhost", timeout=10)
        connection.sock = socket.socket(socket.AF_UNIX)
        connection.sock.settimeout(10)
        connection.sock.connect(str(w / "hoveld.sock"))
        connection.request("POST", "/hovel.daemon.v1.DaemonService/RunSessionCommand",
                           json.dumps({"SessionID": session or first["session"],
                                       "Request": {"command": command, "args": list(args)}}),
                           {"Content-Type": "application/json"})
        response = connection.getresponse()
        raw = response.read()
        data = json.loads(raw) if response.status == 200 else raw.decode()
        connection.close()
        return response.status, data

    code, result = rpc("identity")
    assert code == 200
    owner = json.loads(result["stdout"])
    assert owner["generation"] == first["generation"]
    code, shell = rpc("shell", [first["generation"], first["creation"]])
    assert code == 200 and json.loads(shell["stdout"])["socketInode"] == first["socketInode"]
    assert rpc("shell", ["wrong-generation", first["creation"]])[0] != 200
    assert rpc("shell", [first["generation"], "missing-creation"])[0] != 200
    base = ["127.0.0.1", "tester", "--port", str(port), "--key", str(key)]
    # Cancellation before a queued dispatch must never launch an SSH creation.
    dispatch_lock = os.open(w / "burrow", os.O_RDONLY | os.O_DIRECTORY)
    fcntl.flock(dispatch_lock, fcntl.LOCK_EX)
    queued = subprocess.Popen([binary, "--workspace", str(w), "connect", "queued-cancel", *base, "--yes"],
                              env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        # Existing non-secret phase evidence confirms all seven immutable request
        # fields have been submitted, so this is cancellation at the dispatch queue.
        trace = Path(env["BURROW_PHASE_TRACE"])
        wait(lambda: sum(f'"pid":{queued.pid},"phase":"call:SetChainConfig"' in line
                         for line in trace.read_text().splitlines()) == 7)
        queued.terminate()
        queued.communicate(timeout=5)
        assert queued.returncode != 0
        assert not (w / "burrow/queued-cancel").exists()
        assert burrow(w, "inspect", first["name"])["masterPID"] == first["masterPID"]
    finally:
        if queued.poll() is None:
            queued.kill()
            queued.wait()
        os.close(dispatch_lock)
    # Fail the owner's log admission after the frontend has saved its attempt.
    # The workspace dispatch lock holds the confirmed adapter before owner Open.
    dispatch_lock = os.open(w / "burrow", os.O_RDONLY | os.O_DIRECTORY)
    fcntl.flock(dispatch_lock, fcntl.LOCK_EX)
    rejected = subprocess.Popen([binary, "--workspace", str(w), "connect", "audit-refused", *base, "--yes"],
                                env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    audit = w / "burrow-logs/operations.log"
    try:
        trace = Path(env["BURROW_PHASE_TRACE"])
        wait(lambda: sum(f'"pid":{rejected.pid},"phase":"call:SetChainConfig"' in line
                         for line in trace.read_text().splitlines()) == 7)
        audit.chmod(0o400)
        os.close(dispatch_lock)
        dispatch_lock = None
        rejected.communicate(timeout=20)
        state = burrow(w, "inspect", "audit-refused")
        assert state["state"] == "lost" and "audit incomplete" in state["detail"], state
        assert state["masterPID"] == 0, state
        burrow(w, "close", "audit-refused", "--yes", ok=False)
        assert not (w / "burrow/audit-refused").exists()
        assert rpc("identity")[0] == 200, "failed-open cleanup killed the manager"
    finally:
        audit.chmod(0o600)
        if dispatch_lock is not None: os.close(dispatch_lock)
        if rejected.poll() is None: rejected.kill(); rejected.wait()
    # Independent processes submit distinct immutable request chains.
    processes = [subprocess.Popen([binary, "--workspace", str(w), "connect", name, *base, "--yes"],
                                  env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                 for name in ("parallel-a", "parallel-b")]
    try:
        states = []
        for p in processes:
            out, err = p.communicate(timeout=30)
            assert p.returncode == 0, err
            state = json.loads(out)
            states.append(wait(lambda: (s if (s := burrow(w, "inspect", state["name"]))["state"] == "connected" else None)))
        assert len({s["creation"] for s in states}) == 2
        assert all(s["session"] == first["session"] and s["ownerPID"] == first["ownerPID"] for s in states)
        assert len({s["runID"] for s in states}) == 2
        burrow(w, "close", "parallel-a", "--yes")
        assert burrow(w, "inspect", "parallel-b")["masterPID"] == states[1]["masterPID"]
        burrow(w, "close", "parallel-b", "--yes")
    finally:
        for p in processes:
            if p.poll() is None:
                p.kill()
                p.wait()

    # Reviewed settings are not a mutable alias lookup: name, action and config
    # changes must require a new recap, including a saved-profile expansion.
    config = root / "review-config"
    config.write_text(f"Host reviewed\n HostName 127.0.0.1\n Port {port}\n User tester\n IdentityFile \"{key}\"\n")
    args = ["reviewed", "-", "--ssh-config", str(config)]
    review = burrow(w, "connect", "frozen", *args)
    burrow(w, "connect", "different-name", *args, "--yes", "--review", review["digest"], ok=False)
    config.write_text(config.read_text().replace("User tester", "User changed"))
    burrow(w, "connect", "frozen", *args, "--yes", "--review", review["digest"], ok=False)
    assert not (w / "burrow/frozen").exists() and not (w / "burrow/different-name").exists()

    # Default identities come from the current operator, never a fixed key name
    # in Burrow. The fixture owns this synthetic HOME and all its key material.
    ssh = root / ".ssh"
    ssh.mkdir(mode=0o700, exist_ok=True)
    default = ssh / "id_ed25519"
    default.write_bytes(key.read_bytes())
    default.chmod(0o600)
    try:
        burrow(w,"connect","default-key","127.0.0.1","tester","--port",port,"--yes")
        wait(lambda:burrow(w,"inspect","default-key")["state"]=="connected")
        burrow(w,"close","default-key","--yes")
        burrow(w,"connect","tilde-key","127.0.0.1","tester","--port",port,"--key","~/.ssh/id_ed25519","--yes")
        wait(lambda:burrow(w,"inspect","tilde-key")["state"]=="connected")
        burrow(w,"close","tilde-key","--yes")
    finally:
        default.unlink()

    # Wrong generations and stale creations never address siblings.
    assert rpc("close", ["wrong-generation", first["creation"]])[0] != 200
    assert rpc("close", [first["generation"], "missing-creation"])[0] != 200
    assert burrow(w, "inspect", first["name"])["masterPID"] == first["masterPID"]
    assert rpc("close-reviewed", [first["generation"], "[]"])[0] != 200

    # Owner rejects modified requests. These controls deliberately test refusal,
    # never an alternative frontend execution path.
    raw = json.dumps({"id": uuid.uuid4().hex, "owner": owner,
                      "settings": {"workspace": str(w), "name": "changed-adapter", "host": "127.0.0.1", "user": "tester", "port": port, "key": str(key)},
                      "preview": "changed"})
    assert rpc("connect", [raw, "wrong-review", "refusal-check"])[0] != 200
    assert rpc("connect", [raw, hashlib.sha256(raw.encode()).hexdigest(), "refusal-check"])[0] != 200
    assert not (w / "burrow/changed-adapter").exists()
    print("PASS production concurrent frontends, exact preview binding, stale controls and changed request refusal", flush=True)


def audit_cleanup_checks(burrow, root, options, daemons):
    w = root / "audit-cleanup"
    daemons.append(burrow(w, "--offline", "status")["pid"])
    states = []
    for name in ("cleanup-a", "cleanup-b"):
        burrow(w, "connect", name, "127.0.0.1", "tester", *options)
        deadline = time.monotonic()+15
        while True:
            state = burrow(w, "inspect", name)
            if state["state"] == "connected": states.append(state); break
            assert time.monotonic()<deadline, state
            time.sleep(.1)
    states = burrow(w, "connections")
    log = w / "burrow-logs/operations.log"
    log.chmod(0o400)
    try:
        conn = http.client.HTTPConnection("localhost", timeout=30)
        conn.sock = socket.socket(socket.AF_UNIX)
        conn.sock.settimeout(30)
        conn.sock.connect(str(w / "hoveld.sock"))
        conn.request("POST", "/hovel.daemon.v1.DaemonService/RunSessionCommand",
                     json.dumps({"SessionID":states[0]["session"], "Request":{"command":"close-reviewed", "args":[states[0]["generation"],json.dumps(states)]}}),
                     {"Content-Type":"application/json"})
        response=conn.getresponse(); response.read(); conn.close()
        assert response.status != 200, "missing cleanup audit warning"
        for state in states:
            assert not Path(state["socket"]).parent.exists(), "logging failure stranded a sibling"
        assert burrow(w,"connections") == []
    finally:
        log.chmod(0o600)
    print("PASS unavailable audit still closes all reviewed connections and reports incomplete logging",flush=True)
