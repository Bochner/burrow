"""Local automation through production commands and supported Hovel throws."""
import base64
import http.client
import json
import os
from pathlib import Path
import re
import shlex
import socket
import subprocess
import time


def automation_checks(burrow, workspace, connection, hovel, env, hv, connect_options):
    def run(*args, **kwargs):
        return burrow(workspace, "run", *args, **kwargs)

    def rpc(method, data):
        client = http.client.HTTPConnection("localhost", timeout=15)
        client.sock = socket.socket(socket.AF_UNIX)
        client.sock.settimeout(15)
        client.sock.connect(str(workspace / "hoveld.sock"))
        try:
            client.request("POST", "/hovel.daemon.v1.DaemonService/" + method, json.dumps(data), {"Content-Type": "application/json"})
            response = client.getresponse()
            body = response.read()
            assert response.status == 200, body
            return json.loads(body)
        finally:
            client.close()

    def output(run_id, stream="stdout"):
        return base64.b64decode(run("output", run_id, stream, "0")["data"])

    def timed_out(run_id):
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            state = run("inspect", run_id)
            if state["state"] == "timed-out":
                return state
            time.sleep(.05)
        raise AssertionError(state)

    result = burrow(workspace, "run", "now", connection["name"], "--local", "--yes", "--",
                    "/bin/sh", "-c", 'printf "%s\\n" "$BURROW_CONNECTION" "$BURROW_SOCKET"; printf "local error" >&2; exit 3')
    assert result["execution"] == "local" and result["localExit"] == 3 and result["remoteExit"] is None, result
    assert result["outputComplete"] and result["collection"] == "succeeded", result
    out = burrow(workspace, "run", "output", result["id"], "stdout", "0")
    assert base64.b64decode(out["data"]) == (connection["name"] + "\n" + connection["socket"] + "\n").encode(), out
    assert out["localExit"] == 3 and out["remoteExit"] is None
    err = burrow(workspace, "run", "output", result["id"], "stderr", "0")
    assert base64.b64decode(err["data"]) == b"local error", err
    burrow(workspace, "run", "close", result["id"], "--yes")
    assert burrow(workspace, "inspect", connection["name"])["state"] == "connected"
    print("PASS local execution, distinct nonzero result and retained streams", flush=True)

    child_input = Path(burrow(workspace, "local")["upload"]) / "child-input.bin"
    child_input.write_bytes(bytes(range(256)) * 512)  # Larger than a pipe buffer.
    delayed = run("now", connection["name"], "--local", "--stdin", child_input.name, "--yes", "--", "/bin/sh", "-c",
                  "exec 3<&0; (sleep 2; printf later; wc -c <&3; printf child-error >&2) & exit 0")
    assert output(delayed["id"]) == b"later131072\n" and output(delayed["id"], "stderr") == b"child-error", delayed
    assert delayed["localExit"] == 0 and delayed["outputComplete"], delayed
    run("close", delayed["id"], "--yes")
    print("PASS ordinary child stdin/stdout/stderr after parent exit", flush=True)

    before = run("list")
    run("prepare", "missing", "--local", "--", "/bin/true", ok=False)
    assert run("list") == before
    prepared = run("now", connection["name"], "--local", "--", "/bin/sh", "-c", "printf approved")
    assert "Execution: local" in prepared["review"] and connection["socket"] in prepared["review"]
    run("launch", prepared["id"], "--review", "stale", "--yes", ok=False)
    assert run("inspect", prepared["id"])["state"] == "prepared"
    assert output(prepared["id"]) == b""
    approved = run(*shlex.split(prepared["confirm"])[1:])
    assert approved["localExit"] == 0 and output(approved["id"]) == b"approved"
    assert run("launch", approved["id"], "--yes")["launchRunID"] == approved["launchRunID"]
    run("close", approved["id"], "--yes")

    # The documented saved-chain interface is tested directly, without `hovel run`.
    identity = rpc("GetDaemonInfo", {})
    assert identity["workspacePath"] == str(workspace), identity
    chain = workspace / "local-automation.chain.json"
    def save(command):
        chain.write_text(json.dumps({"apiVersion": "hovel.dev/v1alpha1", "kind": "Chain", "metadata": {"name": "local-automation"},
            "spec": {"mode": "configured", "steps": [{"id": "local", "uses": "module:burrow@0.1.0"}],
                     "targets": [{"id": "local://selected"}], "config": {"workspace": str(workspace), "command": shlex.join(command)}}}))

    def throw(*flags):
        return subprocess.run([str(hovel), "throw", str(chain), "--workspace", str(workspace), "--daemon-endpoint", str(workspace / "hoveld.sock"), "--json", *flags],
                              env=env, capture_output=True, text=True, stdin=subprocess.DEVNULL, timeout=40)

    save(["run", "now", connection["name"], "--local", "--yes", "--", "/bin/sh", "-c", "printf chain-output; printf chain-error >&2; exit 7"])
    before = run("list")
    for flags, text in [(["--allow-dangerous"], "confirmation"), (["--now"], "dangerous")]:
        rejected = throw(*flags)
        assert rejected.returncode == 1 and text in rejected.stderr, rejected
        # JSON mode still prints an interactive review without explicit --now.
        assert not rejected.stdout.strip() or "Type yes" in rejected.stdout, rejected
        assert run("list") == before, "rejected throw launched work"
    # Explicit now still obeys the operation's launch-key policy.
    rpc("SetLaunchKeyPolicy", {"operation": "default", "mode": "quorum", "quorum": 2})
    rpc("AttachEntity", {"id": "local-check-approver", "kind": "cli", "operation": "default", "activeChain": "local-automation"})
    try:
        rejected = throw("--allow-dangerous", "--now")
        assert rejected.returncode == 1 and ("approval" in rejected.stderr or "launch" in rejected.stderr), rejected
        assert run("list") == before
        pending = re.search(r"pending-[0-9a-f]+", rejected.stderr)
        assert pending, rejected.stderr
        rpc("CancelPendingThrow", {"id": pending.group()})
    finally:
        rpc("DetachEntity", {"id": "local-check-approver"})
        rpc("SetLaunchKeyPolicy", {"operation": "default", "mode": "anyone"})
    completed = throw("--allow-dangerous", "--now")
    assert completed.returncode == 0, completed
    record = json.loads(completed.stdout)["results"][0]
    assert record["state"] == "succeeded", record
    result = json.loads(record["summary"])
    assert result["localExit"] == 7 and result["remoteExit"] is None and result["collection"] == "succeeded", result
    assert output(result["id"]) == b"chain-output" and output(result["id"], "stderr") == b"chain-error"
    artifacts = json.loads(hv("artifact", "list", "--json"))
    collected = [a for a in artifacts if a["runId"] == result["runID"]]
    assert len(collected) == 3, collected
    saved_bytes = {a["path"]: (workspace / a["path"]).read_bytes() for a in collected}
    run("close", result["id"], "--yes")
    assert [a for a in json.loads(hv("artifact", "list", "--json")) if a["runId"] == result["runID"]] == collected, "close removed registered evidence"
    assert all((workspace / path).read_bytes() == data for path, data in saved_bytes.items())
    print("PASS saved-chain execution, explicit workspace, confirmation/danger/launch-key refusal and non-JSON errors", flush=True)

    # A local driver actually uses the selected master; no inherited launch key or credentials.
    source = Path(burrow(workspace, "local")["upload"]) / "local driver.sh"
    source.write_text('''test -z "${HOVEL_MODULE_LAUNCH_KEY+x}${SSH_AUTH_SOCK+x}${AUTOMATION_SECRET_CANARY+x}" || exit 90
printf '%s\\n' "$BURROW_CONNECTION"
/usr/bin/ssh -F "$BURROW_SSH_CONFIG" -S "$BURROW_SOCKET" -o ControlMaster=no -o ProxyCommand=/usr/bin/false -o BatchMode=yes -T "$BURROW_SSH_HOST" 'printf "%s\\n" "$SSH_CONNECTION"'
''')
    burrow(workspace, "connect", "local-second", connection["host"], connection["user"], *connect_options)
    deadline = time.monotonic() + 15
    while (second := burrow(workspace, "inspect", "local-second"))["state"] != "connected":
        assert time.monotonic() < deadline, second
        time.sleep(.05)
    sockets = []
    for selected in [connection, second]:
        driver = run("now", selected["name"], "--local", "--script", source.name, "--mode", "stream", "--interpreter", "/bin/sh", "--yes", "--")
        assert driver["localExit"] == 0 and driver["remoteExit"] is None, driver
        data = output(driver["id"]).decode().splitlines()
        assert data[0] == selected["name"] and len(data[1].split()) == 4, data
        sockets.append((selected["name"], data[1]))
        run("close", driver["id"], "--yes")
    assert sockets[0][1] != sockets[1][1], sockets
    pending = run("prepare", second["name"], "--local", "--", "/bin/true")
    sock = Path(second["socket"])
    hidden = sock.with_name("local-test-hidden")
    sock.rename(hidden)
    try:
        run("launch", pending["id"], "--yes", ok=False)
        assert run("inspect", pending["id"])["state"] == "prepared"
        assert not sock.exists()
    finally:
        hidden.rename(sock)
    run("close", pending["id"], "--yes")
    burrow(workspace, "close", second["name"], "--yes")

    result = run("now", connection["name"], "--local", "--yes", "--", "/bin/sh", "-c", "printf uncertain >&2; exit 255")
    assert result["localExit"] == 255 and result["remoteExit"] is None and result["outputComplete"], result
    run("close", result["id"], "--yes")
    failed = run("prepare", connection["name"], "--local", "--", "/missing/burrow-local-tool")
    run("launch", failed["id"], "--yes", ok=False)
    state = run("inspect", failed["id"])
    assert state["state"] == "local-start-failed" and state["localExit"] is None, state
    assert run("collect", failed["id"], "--yes")["collection"] == "succeeded"
    run("close", failed["id"], "--yes")

    for timeout, parent_exits in ((False, False), (True, False), (False, True), (True, True)):
        flags = ["--timeout", "2s"] if timeout else []
        task = run("prepare", connection["name"], "--local", *flags, "--", "/bin/sh", "-c",
                   "echo $$; sleep 60 & echo $!; " + ("exit 0" if parent_exits else "wait"))
        run("launch", task["id"], "--yes")
        deadline = time.monotonic() + 10
        while not output(task["id"]).count(b"\n") == 2:
            assert time.monotonic() < deadline
            time.sleep(.05)
        if parent_exits and not timeout:
            time.sleep(1.2)  # Parent and bounded pipe drain have finished.
            assert run("inspect", task["id"])["state"] == "running", "ordinary child became uncancellable"
            run("close", task["id"], "--yes", ok=False)
        state = timed_out(task["id"]) if timeout else run("cancel", task["id"], "--yes")
        assert state["state"] == ("timed-out" if timeout else "cancelled"), state
        assert state["cancellation"] == "local-group-terminated; remote termination unconfirmed", state
        assert state["remoteExit"] is None
        if parent_exits:
            assert state["localExit"] == 0 and not state["outputComplete"], state
        for pid in output(task["id"]).decode().split():
            path = Path("/proc") / pid / "stat"
            assert not path.exists() or path.read_text().rsplit(")", 1)[1].split()[0] in ("Z", "X"), pid
        run("collect", task["id"], "--yes")
        run("close", task["id"], "--yes")
    assert burrow(workspace, "inspect", connection["name"])["state"] == "connected"
    print("PASS local socket driver, missing selection/master refusal, local child cleanup and timeout", flush=True)
