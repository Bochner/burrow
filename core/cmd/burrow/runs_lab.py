"""Retained commands through the public Burrow/Hovel interface and real SSH."""
import base64
import hashlib
import json
import os
import signal
import subprocess
import resource
import shlex
from pathlib import Path
import time


def run_checks(burrow, workspace, connection, container, command, hv, binary, env, decoder, scripts_only=False):
    def run(*args, **kw):
        return burrow(workspace, "run", *args, **kw)

    def finished(run_id):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            state = run("inspect", run_id)
            if state["state"] not in ("prepared", "running"):
                return state
            time.sleep(.1)
        raise AssertionError(state)

    def remote_ready(path):
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            result = subprocess.run(["docker", "exec", container, "test", "-s", path], capture_output=True)
            if result.returncode == 0:
                return
            time.sleep(.05)
        raise AssertionError("remote command never published " + path)

    def master_loss_checks():
        missing = run("prepare", connection["name"], "--", "true")
        source.write_text("sleep 60\n")
        loss = run("prepare", connection["name"], "--script", source.name, "--mode", "stage", "--interpreter", "/bin/sh", "--")
        run("launch", loss["id"], "--yes")
        socket = Path(connection["socket"])
        hidden = socket.with_name("master-hidden")
        socket.rename(hidden)
        try:
            run("launch", missing["id"], "--yes", ok=False)
            assert not socket.exists()
            uncertain = run("cancel", loss["id"], "--yes")
            assert uncertain["cancellation"].startswith("unconfirmed"), uncertain
            assert uncertain["stageCleanup"].startswith("unconfirmed"), uncertain
            assert uncertain["remoteExit"] is None
            command("docker", "exec", container, "test", "-f", loss["input"]["stagePath"])
        finally:
            hidden.rename(socket)
        run("cancel", missing["id"], "--yes")
        run("close", missing["id"], "--yes")
        assert run("collect", loss["id"], "--yes")["collection"] == "succeeded"
        run("close", loss["id"], "--yes")
        print("PASS master-loss uncertainty preserves staged files without reconnect", flush=True)

    # Local source is snapshotted before review; streamed source owns stdin.
    uploads = Path(burrow(workspace, "local")["upload"])
    source = uploads / "quoted script.sh"
    source.write_text("printf '<%s>\\n' \"$@\"; printf separate >&2; exit 7\n")
    scripted = run("prepare", connection["name"], "--script", source.name,
                   "--mode", "stream", "--interpreter", "/bin/sh", "--",
                   "My Documents", "café", "$(hostname)", "a'b", "")
    source.write_text("exit 99\n")
    review = run("launch", scripted["id"])
    assert "stream" in review["review"] and "quoted script.sh" in review["review"]
    run("launch", scripted["id"], "--review", review["digest"], "--yes")
    state = finished(scripted["id"])
    assert state["remoteExit"] == 7 and state["outputComplete"], state
    assert base64.b64decode(run("output", scripted["id"], "stdout", "0")["data"]) == "<My Documents>\n<café>\n<$(hostname)>\n<a'b>\n<>\n".encode()
    assert base64.b64decode(run("output", scripted["id"], "stderr", "0")["data"]) == b"separate"
    assert run("collect", scripted["id"], "--yes")["collection"] == "succeeded"
    run("close", scripted["id"], "--yes")

    print("PASS streamed script snapshot and exact arguments", flush=True)
    data = uploads / "input.bin"
    payload = bytes(range(256)) * 1024 + b"SCRIPT-STDIN-SECRET-CANARY"
    data.write_bytes(payload)
    source.write_text("sha256sum; printf '%s' \"$1\" >&2\n")
    for options, argv in [
        (["--script", source.name, "--mode", "inline", "--interpreter", "/bin/sh"], ["literal;λ"]),
        ([], ["/usr/bin/sha256sum"]),
    ]:
        data.write_bytes(payload)
        prepared = run("prepare", connection["name"], *options, "--stdin", data.name, "--", *argv)
        data.write_bytes(b"changed after preparation")
        assert "SCRIPT-STDIN-SECRET-CANARY" not in json.dumps(prepared)
        run("launch", prepared["id"], "--yes")
        state = finished(prepared["id"])
        assert state["remoteExit"] == 0 and state["outputComplete"], state
        output = base64.b64decode(run("output", state["id"], "stdout", "0")["data"])
        assert output.split()[0].decode() == hashlib.sha256(payload).hexdigest(), output
        assert run("collect", state["id"], "--yes")["collection"] == "succeeded"
        run("close", state["id"], "--yes")
    assert b"SCRIPT-STDIN-SECRET-CANARY" not in (workspace / "burrow-logs/operations.log").read_bytes()
    print("PASS inline source and independent binary stdin", flush=True)
    source.write_text("printf '%s\\n' \"$0\"; cat; exit 7\n")
    data.write_bytes(b"staged-input")
    staged = run("prepare", connection["name"], "--script", source.name, "--mode", "stage",
                 "--interpreter", "/bin/sh", "--stdin", data.name, "--")
    stage = staged["input"]["stagePath"]
    command("docker", "exec", container, "test", "!", "-e", str(Path(stage).parent))
    run("launch", staged["id"], "--yes")
    state = finished(staged["id"])
    assert state["remoteExit"] == 7 and state["staging"] == "ready" and state["stageCleanup"] == "removed", state
    assert base64.b64decode(run("output", state["id"], "stdout", "0")["data"]) == (stage + "\nstaged-input").encode()
    command("docker", "exec", container, "test", "!", "-e", str(Path(stage).parent))
    assert run("collect", staged["id"], "--yes")["collection"] == "succeeded"
    run("close", staged["id"], "--yes")
    print("PASS explicit staging and nonzero-exit cleanup", flush=True)
    source.write_text("printf kept\n")
    kept = run("now", connection["name"], "--script", source.name, "--mode", "stage",
               "--interpreter", "/bin/sh", "--keep", "--yes", "--")
    assert kept["stageCleanup"] == "kept" and kept["collection"] == "succeeded", kept
    stage = kept["input"]["stagePath"]
    assert command("docker", "exec", container, "cat", stage) == "printf kept\n"
    assert command("docker", "exec", container, "stat", "-c", "%a", str(Path(stage).parent), stage).split() == ["700", "600"]
    run("close", kept["id"], "--yes")
    command("docker", "exec", container, "test", "-f", stage)

    # Existing remote scripts use the ordinary command path and are never deleted.
    existing = run("now", connection["name"], "--yes", "--", "/bin/sh", stage)
    assert existing["remoteExit"] == 0
    command("docker", "exec", container, "test", "-f", stage)
    run("close", existing["id"], "--yes")

    source.write_text('printf preserve > "$(dirname "$0")/unrelated"\n')
    unrelated = run("now", connection["name"], "--script", source.name, "--mode", "stage",
                    "--interpreter", "/bin/sh", "--yes", "--")
    stage = unrelated["input"]["stagePath"]
    assert unrelated["remoteExit"] == 0 and unrelated["stageCleanup"].startswith("failed"), unrelated
    command("docker", "exec", container, "test", "!", "-e", stage)
    assert command("docker", "exec", container, "cat", str(Path(stage).parent / "unrelated")) == "preserve"
    run("close", unrelated["id"], "--yes")

    collision = run("prepare", connection["name"], "--script", source.name, "--mode", "stage", "--interpreter", "/bin/sh", "--")
    stage = collision["input"]["stagePath"]
    command("docker", "exec", container, "mkdir", str(Path(stage).parent))
    command("docker", "exec", container, "sh", "-c", 'printf original > "$1"', "sh", stage)
    run("launch", collision["id"], "--yes", ok=False)
    state = run("inspect", collision["id"])
    assert state["state"] == "staging-failed" and state["remoteExit"] is None, state
    assert state["stageCleanup"].startswith("not-created"), state
    assert command("docker", "exec", container, "cat", stage) == "original"
    assert run("launch", collision["id"], "--yes")["state"] == "staging-failed"
    assert run("collect", collision["id"], "--yes")["collection"] == "succeeded"
    run("close", collision["id"], "--yes")
    print("PASS keep, existing remote script, staging failure and unrelated-file preservation", flush=True)
    source.write_text("echo $$; sleep 60 & echo $!; wait\n")
    sibling = run("prepare", connection["name"], "--", "sleep", "120")
    run("launch", sibling["id"], "--yes")
    for timeout, keep in ((False, False), (True, False), (True, True)):
        options = ["--timeout", "2s"] if timeout else []
        if keep:
            options.append("--keep")
        stopped = run("prepare", connection["name"], "--script", source.name, "--mode", "stage",
                      "--interpreter", "/bin/sh", *options, "--")
        run("launch", stopped["id"], "--yes")
        if timeout:
            state = finished(stopped["id"])
            assert state["timedOut"] and state["state"] == "timed-out", state
        else:
            state = run("cancel", stopped["id"], "--yes")
            assert state["state"] == "cancelled", state
        assert state["cancellation"] == "ordinary-group-terminated" and state["stageCleanup"] == ("kept" if keep else "removed"), state
        pids = base64.b64decode(run("output", stopped["id"], "stdout", "0")["data"]).split()
        assert len(pids) == 2
        for pid in pids:
            command("docker", "exec", container, "sh", "-c",
                    'test ! -e /proc/$1/stat || test "$(cut -d " " -f 3 /proc/$1/stat)" = Z', "sh", pid.decode())
        assert run("inspect", sibling["id"])["state"] == "running", "script cancellation killed sibling"
        assert run("collect", stopped["id"], "--yes")["collection"] == "succeeded"
        run("close", stopped["id"], "--yes")
    run("cancel", sibling["id"], "--yes")
    run("close", sibling["id"], "--yes")
    print("PASS script cancellation/timeout, cleanup and sibling usability", flush=True)
    source.write_text('mv "$0" "$0.original"; printf replacement > "$0"\n')
    replaced_script = run("now", connection["name"], "--script", source.name, "--mode", "stage", "--interpreter", "/bin/sh", "--yes", "--")
    assert replaced_script["stageCleanup"].startswith("failed"), replaced_script
    assert command("docker", "exec", container, "cat", replaced_script["input"]["stagePath"]) == "replacement"
    run("close", replaced_script["id"], "--yes")

    # Reject traversal, escaping links, special files and inline argument limits.
    outside = uploads.parent / "outside.sh"
    outside.write_text("exit 0\n")
    (uploads / "outside-link").symlink_to(outside)
    os.mkfifo(uploads / "input-pipe")
    source.write_bytes(b"#" * 65537)
    before = set(workspace.glob(".burrow-run-*"))
    for local in ("../outside.sh", "outside-link", "input-pipe"):
        run("prepare", connection["name"], "--stdin", local, "--", "cat", ok=False)
    run("prepare", connection["name"], "--script", source.name, "--mode", "inline", "--interpreter", "/bin/sh", "--", ok=False)
    source.write_bytes(b"printf '\x00'\n")
    run("prepare", connection["name"], "--script", source.name, "--mode", "inline", "--interpreter", "/bin/sh", "--", ok=False)
    assert set(workspace.glob(".burrow-run-*")) == before, "failed preparation leaked working files"
    for p in workspace.rglob("*"):
        if p.is_file() and p.name.endswith((".log", ".db", ".db-wal", ".json")):
            assert b"SCRIPT-STDIN-SECRET-CANARY" not in p.read_bytes(), p
    print("PASS input containment, inline limits, replacement refusal and secret exclusion", flush=True)
    if scripts_only:
        run_ui(binary, env, decoder, burrow, workspace, connection["name"])
        master_loss_checks()
        return

    immediate = run("now", connection["name"], "--budget", "1024", "--yes", "--", "printf", "%s", "--yes")
    assert immediate["id"] and immediate["launchRunID"], immediate
    assert immediate["state"] == "exited" and immediate.get("collection") == "succeeded", immediate
    state = finished(immediate["id"])
    assert state["remoteExit"] == 0 and state["command"] == ["printf", "%s", "--yes"], state
    assert base64.b64decode(run("output", immediate["id"], "stdout", "0")["data"]) == b"--yes"
    artifacts = [a for a in json.loads(hv("artifact", "list", "--json")) if a["runId"] == immediate["runID"]]
    assert len(artifacts) == 3, artifacts
    stdout, = [a for a in artifacts if a["name"].endswith("-stdout")]
    assert (workspace / stdout["path"]).read_bytes() == b"--yes"
    run("close", immediate["id"], "--yes")

    # A failed final audit write must still identify a command that executed.
    audit_command = ["/bin/sh", "-c", "echo ready >/tmp/burrow-now-audit; while [ ! -f /tmp/burrow-now-release ]; do sleep .1; done; printf audit-output"]
    caller = subprocess.Popen([binary, "--workspace", str(workspace), "run", "now", connection["name"], "--yes", "--", *audit_command],
                              env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    log = workspace / "burrow-logs/operations.log"
    try:
        remote_ready("/tmp/burrow-now-audit")
        audit_run, = [r for r in run("list") if r["command"] == audit_command]
        log.chmod(0o400)
        command("docker", "exec", container, "touch", "/tmp/burrow-now-release")
        _, error = caller.communicate(timeout=20)
        assert caller.returncode != 0 and "audit incomplete" in error and audit_run["id"] in error, error
    finally:
        log.chmod(0o600)
        if caller.poll() is None:
            caller.terminate()
            caller.communicate(timeout=10)
    assert run("inspect", audit_run["id"])["remoteExit"] == 0
    run("close", audit_run["id"], "--yes")

    interrupted_command = ["/bin/sh", "-c", "echo ready >/tmp/burrow-now-interrupt; sleep 300"]
    caller = subprocess.Popen([binary, "--workspace", str(workspace), "run", "now", connection["name"], "--yes", "--", *interrupted_command],
                              env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    try:
        remote_ready("/tmp/burrow-now-interrupt")
        interrupted, = [r for r in run("list") if r["command"] == interrupted_command]
        caller.send_signal(signal.SIGINT)
        _, error = caller.communicate(timeout=15)
        assert caller.returncode != 0 and interrupted["id"] in error, error
        assert run("inspect", interrupted["id"])["state"] == "running", "stopping the wait cancelled the remote command"
    finally:
        if caller.poll() is None:
            caller.terminate()
            caller.communicate(timeout=10)
    run("cancel", interrupted["id"], "--yes")
    assert run("collect", interrupted["id"], "--yes")["collection"] == "succeeded"
    run("close", interrupted["id"], "--yes")

    hv("chain", "create", "retained-check", chain="retained-check")
    hv("chain", "add", "burrow@0.1.0", chain="retained-check")
    hv("target", "add", "local", chain="retained-check")
    hv("chain", "config", "set", "workspace", str(workspace), chain="retained-check")

    def hovel_run(*args):
        hv("chain", "config", "set", "command", shlex.join(["run", *args]), chain="retained-check")
        result = json.loads(hv("throw", "--now", "--allow-dangerous", "--json", chain="retained-check"))["results"][0]
        assert result["state"] == "succeeded", result
        return json.loads(result["summary"])

    source.write_text("printf hovel-script\n")
    scripted = hovel_run("now", connection["name"], "--script", source.name, "--mode", "stream", "--interpreter", "/bin/sh", "--yes", "--")
    assert scripted["collection"] == "succeeded" and scripted["remoteExit"] == 0, scripted
    assert base64.b64decode(run("output", scripted["id"], "stdout", "0")["data"]) == b"hovel-script"
    run("close", scripted["id"], "--yes")

    now_review = hovel_run("now", connection["name"], "--", "printf", "reviewed-now")
    contract = burrow(workspace, "capabilities", "run.now")
    review_shape = contract["results"]["RunReview"]["anyOf"][0]
    assert set(review_shape["required"]) <= now_review.keys() <= review_shape["properties"].keys(), (review_shape, now_review)
    assert run("inspect", now_review["id"])["state"] == "prepared", now_review
    assert now_review["confirm"] == f'run launch {now_review["id"]} --collect --review {now_review["digest"]} --yes'
    assert hovel_run(*shlex.split(now_review["confirm"])[1:])["collection"] == "succeeded"
    assert finished(now_review["id"])["remoteExit"] == 0
    assert base64.b64decode(run("output", now_review["id"], "stdout", "0")["data"]) == b"reviewed-now"
    run("close", now_review["id"], "--yes")
    hovel_now = hovel_run("now", connection["name"], "--yes", "--", "true")
    assert hovel_now["collection"] == "succeeded" and hovel_now["remoteExit"] == 0
    assert hovel_now["id"] not in (immediate["id"], now_review["id"])
    assert finished(hovel_now["id"])["remoteExit"] == 0
    run("close", hovel_now["id"], "--yes")

    prepared = hovel_run("prepare", connection["name"], "--", "/bin/sh", "-c",
                   "sleep 1; printf 'retained stdout'; printf 'separate stderr' >&2; exit 7")
    run_id = prepared["id"]
    assert prepared["state"] == "prepared" and prepared["remoteExit"] is None
    assert run("inspect", run_id)["state"] == "prepared"
    review = run("launch", run_id)
    assert "retained stdout" in review["review"]
    assert run("inspect", run_id)["state"] == "prepared"
    launched = hovel_run("launch", run_id, "--review", review["digest"], "--yes")
    assert launched["id"] == run_id
    # Every command above is a fresh caller. None remains to supervise the run.
    state = finished(run_id)
    assert state["state"] == "exited" and state["remoteExit"] == 7, state
    assert state["outputComplete"] is True
    log = workspace / "burrow-logs/operations.log"
    notes = log.read_text()
    assert "-- run launch" in notes and '"remoteExit": 7' in notes and run_id in notes
    assert base64.b64decode(run("output", run_id, "stdout", "0")["data"]) == b"retained stdout"
    assert base64.b64decode(run("output", run_id, "stderr", "0")["data"]) == b"separate stderr"
    assert run("launch", run_id, "--yes")["launchRunID"] == state["launchRunID"]
    assert any(item["id"] == run_id for item in run("list"))
    assert burrow(workspace, "inspect", connection["name"])["state"] == "connected"
    collected = hovel_run("collect", run_id, "--yes")
    assert collected["collection"] == "succeeded" and collected["remoteExit"] == 7
    artifacts = json.loads(hv("artifact", "list", "--json"))
    artifacts = [a for a in artifacts if a["runId"] == collected["runID"]]
    assert len(artifacts) == 3, artifacts
    for item in artifacts:
        data = (workspace / item["path"]).read_bytes()
        assert hashlib.sha256(data).hexdigest() == item["sha256"]
    assert run("collect", run_id, "--yes")["collection"] == "succeeded"
    storage = workspace / Path(artifacts[0]["path"]).parts[0]
    mode = storage.stat().st_mode & 0o777
    storage.chmod(0)
    try:
        run("collect", run_id, "--yes", ok=False)
    finally:
        storage.chmod(mode)
    assert run("inspect", run_id)["remoteExit"] == 7
    assert run("collect", run_id, "--yes")["collection"] == "succeeded"
    assert run("close", run_id, "--yes")["state"] == "closed"
    for item in artifacts:
        assert (workspace / item["path"]).exists(), item

    # Binary streams exceed preview/RPC chunk size and retain exact bytes.
    binary_run = run("prepare", connection["name"], "--budget", "3000000", "--",
                 "/bin/sh", "-c", "head -c 2097152 /dev/zero; head -c 1048576 /dev/zero >&2")
    run("launch", binary_run["id"], "--yes")
    state = finished(binary_run["id"])
    assert state["remoteExit"] == 0 and state["storedBytes"] == [2097152, 1048576], state
    assert state["outputComplete"]
    collected = run("collect", binary_run["id"], "--yes")
    records = json.loads(hv("artifact", "list", "--json"))
    for name, size in (("stdout", 2097152), ("stderr", 1048576)):
        item = next(a for a in records if a["runId"] == collected["runID"] and a["name"].endswith(name))
        assert (workspace / item["path"]).read_bytes() == b"\0" * size
    run("close", binary_run["id"], "--yes")

    # Cleanup stays with the original private directory, not a replacement.
    before = set(workspace.glob(".burrow-run-*"))
    replaced = run("prepare", connection["name"], "--", "true")
    spool, = set(workspace.glob(".burrow-run-*")) - before
    run("launch", replaced["id"], "--yes")
    finished(replaced["id"])
    moved = spool.with_name(spool.name + "-moved")
    spool.rename(moved)
    spool.mkdir(mode=0o700)
    run("collect", replaced["id"], "--yes", ok=False)
    run("close", replaced["id"], "--yes", ok=False)
    assert spool.is_dir(), "close removed an unrelated replacement directory"
    assert sorted(p.name for p in moved.iterdir()) == ["stderr", "stdout"]
    spool.rmdir()
    moved.rename(spool)
    run("close", replaced["id"], "--yes")
    assert not spool.exists()

    budget = run("prepare", connection["name"], "--budget", "7", "--",
                 "/bin/sh", "-c", "printf 123456789; printf abc >&2; exit 9")
    run("launch", budget["id"], "--yes")
    state = finished(budget["id"])
    assert state["remoteExit"] == 9 and not state["outputComplete"], state
    assert state["storedBytes"] == [7, 3] and state["receivedBytes"] == [9, 3]
    assert run("collect", budget["id"], "--yes")["collection"] == "succeeded"
    run("close", budget["id"], "--yes")

    # Kernel-enforced write failure, not a simulated capture result.
    disk = run("prepare", connection["name"], "--", "/bin/sh", "-c",
               "sleep 3; head -c 8192 /dev/zero; exit 4")
    run("launch", disk["id"], "--yes")
    prior = resource.prlimit(disk["ownerPID"], resource.RLIMIT_FSIZE)
    resource.prlimit(disk["ownerPID"], resource.RLIMIT_FSIZE, (1024, prior[1]))
    state = finished(disk["id"])
    resource.prlimit(disk["ownerPID"], resource.RLIMIT_FSIZE, prior)
    assert state["remoteExit"] == 4 and not state["outputComplete"], state
    assert "write failed" in state["outputError"] and state["storedBytes"][0] == 1024
    assert run("collect", disk["id"], "--yes")["collection"] == "succeeded"
    run("close", disk["id"], "--yes")

    sibling = run("prepare", connection["name"], "--", "sleep", "60")
    run("launch", sibling["id"], "--yes")
    child = run("prepare", connection["name"], "--", "/bin/sh", "-c",
                "echo $$ >/tmp/burrow-run-leader; sleep 60 & echo $! >/tmp/burrow-run-child; wait")
    run("launch", child["id"], "--yes")
    remote_ready("/tmp/burrow-run-child")
    assert run("inspect", child["id"])["state"] == "running"
    cancelled = run("cancel", child["id"], "--yes")
    assert cancelled["cancellation"] == "ordinary-group-terminated", cancelled
    assert cancelled["remoteExit"] is None
    command("docker", "exec", container, "sh", "-c",
            'for f in /tmp/burrow-run-leader /tmp/burrow-run-child; do '
            'pid=$(cat "$f"); test ! -e /proc/$pid/stat || '
            'test "$(cut -d " " -f 3 /proc/$pid/stat)" = Z || exit 1; done')
    assert burrow(workspace, "inspect", connection["name"])["state"] == "connected"
    assert run("inspect", sibling["id"])["state"] == "running", "cancel killed sibling"
    assert run("cancel", child["id"], "--yes")["cancellation"] == "ordinary-group-terminated"
    run("collect", child["id"], "--yes")
    run("close", child["id"], "--yes")
    run("cancel", sibling["id"], "--yes")
    run("close", sibling["id"], "--yes")

    refused = run("prepare", connection["name"], "--", "true")
    cleanup = run("prepare", connection["name"], "--", "sleep", "60")
    run("launch", cleanup["id"], "--yes")
    log.chmod(0o400)
    try:
        run("launch", refused["id"], "--yes", ok=False)
        assert run("inspect", refused["id"])["state"] == "prepared"
        run("cancel", cleanup["id"], "--yes", ok=False)
        state = run("inspect", cleanup["id"])
        assert state["cancellation"] == "ordinary-group-terminated", state
        assert state["auditError"], state
    finally:
        log.chmod(0o600)
    run("cancel", refused["id"], "--yes")
    run("close", refused["id"], "--yes")
    run("close", cleanup["id"], "--yes")

    # Lose the launch caller after the remote action, then retry the same run.
    killed = run("prepare", connection["name"], "--", "/bin/sh", "-c",
                 "echo once >>/tmp/burrow-launch-count; sleep 60")
    caller = subprocess.Popen([binary, "--workspace", str(workspace), "run", "launch", killed["id"], "--yes"],
                              env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        remote_ready("/tmp/burrow-launch-count")
        caller.kill()
        caller.communicate(timeout=10)
        again = run("launch", killed["id"], "--yes")
        assert again["state"] == "running"
        assert command("docker", "exec", container, "cat", "/tmp/burrow-launch-count") == "once\n"
    finally:
        if caller.poll() is None:
            caller.kill()
            caller.communicate()
    run("cancel", killed["id"], "--yes")
    run("close", killed["id"], "--yes")

    # A descendant deliberately leaves the process group. The launch leader
    # exits while the descendant holds SSH output open; no stale group signal.
    escaped = run("prepare", connection["name"], "--", "/bin/sh", "-c",
                  "setsid /bin/sh -c 'echo $$ >/tmp/burrow-escaped; sleep 60' & exit 0")
    run("launch", escaped["id"], "--yes")
    remote_ready("/tmp/burrow-escaped")
    result = run("cancel", escaped["id"], "--yes")
    assert result["cancellation"].startswith("unconfirmed"), result
    command("docker", "exec", container, "sh", "-c", 'kill -0 $(cat /tmp/burrow-escaped)')
    command("docker", "exec", container, "sh", "-c", 'kill -TERM -$(cat /tmp/burrow-escaped)')
    run("close", escaped["id"], "--yes")

    # Waiting outlives the ordinary one-minute frontend request deadline.
    # Keep this separate from direct Hovel setup writers (see HovelDispatch).
    slow = subprocess.Popen([binary, "--workspace", str(workspace), "run", "now", connection["name"], "--yes", "--",
                             "/bin/sh", "-c", "sleep 61; printf delayed-output; printf delayed-error >&2; exit 7"],
                            env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    out, error = slow.communicate(timeout=75)
    assert slow.returncode == 0, error
    slow_result = json.loads(out)
    assert slow_result["collection"] == "succeeded" and slow_result["remoteExit"] == 7, slow_result
    assert base64.b64decode(run("output", slow_result["id"], "stdout", "0")["data"]) == b"delayed-output"
    assert base64.b64decode(run("output", slow_result["id"], "stderr", "0")["data"]) == b"delayed-error"
    run("close", slow_result["id"], "--yes")
    run_ui(binary, env, decoder, burrow, workspace, connection["name"])

    # Lost retained owners are never adopted or relaunched. Registered evidence
    # survives; abandoned working files are deliberately not automatic recovery.
    lost = run("prepare", connection["name"], "--", "/bin/sh", "-c",
               "echo $$ >/tmp/burrow-lost-run; sleep 60")
    run("launch", lost["id"], "--yes")
    remote_ready("/tmp/burrow-lost-run")
    os.kill(lost["ownerPID"], signal.SIGKILL)
    run("inspect", lost["id"], ok=False)
    run("launch", lost["id"], "--yes", ok=False)
    run("collect", lost["id"], "--yes", ok=False)
    for item in artifacts:
        assert (workspace / item["path"]).exists()
    command("docker", "exec", container, "sh", "-c", 'kill -TERM -$(cat /tmp/burrow-lost-run)')

    master_loss_checks()


def run_ui(binary, env, decoder, burrow, workspace, connection, local=False):
    import fcntl
    import pty
    import select
    import struct
    import termios

    script = Path(burrow(workspace, "local")["upload"]) / "viewer script.sh"
    execution = ["--local"] if local else []
    mode = "stream" if local else "stage"
    script.write_text("printf 'retained viewer output\\nremote\\033]52;c;untrusted\\007\\n'; printf 'viewer stderr\\n' >&2; exit 7\n")
    saved = burrow(workspace, "run", "prepare", connection, *execution, "--script", script.name, "--mode", mode, "--interpreter", "/bin/sh", "--")
    burrow(workspace, "run", "launch", saved["id"], "--yes")
    deadline = time.monotonic() + 15
    while burrow(workspace, "run", "inspect", saved["id"])["state"] == "running":
        assert time.monotonic() < deadline
        time.sleep(.1)
    burrow(workspace, "run", "collect", saved["id"], "--yes")
    burrow(workspace, "run", "close", saved["id"], "--yes")
    prepared = burrow(workspace, "run", "prepare", connection, *execution, "--", "/bin/sh", "-c",
                      "printf 'remote\\033]52;c;untrusted\\007'; sleep 60")
    run_id = prepared["id"]
    outer, slave = pty.openpty()
    before = termios.tcgetattr(slave)
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))

    def controlling():
        os.setsid()
        fcntl.ioctl(0, termios.TIOCSCTTY, 0)

    frontend = subprocess.Popen([binary, "--workspace", str(workspace), "tui"], env=env,
                                stdin=slave, stdout=slave, stderr=slave, preexec_fn=controlling)
    output = bytearray()
    dimensions = [160, 40]

    def wait(needle, *also):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if select.select([outer], [], [], .05)[0]:
                output.extend(os.read(outer, 65536))
            screen = subprocess.run([decoder, *map(str, dimensions)], input=output, capture_output=True, check=True).stdout.decode()
            if all(value in screen for value in (needle, *also)):
                return screen
            assert frontend.poll() is None, screen
        raise AssertionError((needle, screen))

    def finish():
        deadline = time.monotonic() + 10
        while frontend.poll() is None and time.monotonic() < deadline:
            if select.select([outer], [], [], .05)[0]:
                output.extend(os.read(outer, 65536))
        assert frontend.poll() is not None, "frontend exit stalled"

    try:
        wait(connection)
        os.write(outer, b"unfinished-draft\x0c")
        wait("Collected output")
        wait("retained viewer output")
        wait("FAILED (exit 7)")
        if local:
            wait("local tool; remote outcome unconfirmed")
        for width, height in ((200, 50), (120, 30), (80, 24), (160, 40)):
            while select.select([outer], [], [], .05)[0]:
                output.extend(os.read(outer, 65536))
            output.clear()
            dimensions[:] = [width, height]
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
            os.kill(frontend.pid, signal.SIGWINCH)
            wait("Collected output")
            os.write(outer, b"gg/^  Outcome:\r")
            capture = wait("FAILED (exit 7)")
            if directory := os.environ.get("TEST_UNDECLARED_OUTPUTS_DIR"):
                Path(directory, f"results-collected-{width}x{height}.txt").write_text(capture)
        os.write(outer, b"/remote\\\\u001b\r")
        wait("remote\\u001b]52;c;untrusted\\u0007")
        assert b"\x1b]52;c;untrusted" not in output, "collected output injected terminal control"
        os.write(outer, b"/STDERR\r")
        wait("viewer stderr")
        os.write(outer, b"\t")
        wait("Operator notes")
        os.write(outer, b"\t")
        wait("Collected output")
        os.write(outer, b"\x0c")
        wait("unfinished-draft")
        os.write(outer, b"\x15")
        os.write(outer, b"run launch \t")
        wait("run launch " + run_id)
        os.write(outer, b"\r")
        wait("Proceed?")
        os.write(outer, b"\x1b")
        wait("COMMAND OUTPUT")
        assert burrow(workspace, "run", "inspect", run_id)["state"] == "prepared"
        os.write(outer, ("run launch " + run_id + "\r").encode())
        wait("Proceed?")
        os.write(outer, b"\t\r")
        wait('"launchRunID"', '"id": "' + run_id + '"')
        if local:
            os.write(outer, ("run follow " + run_id + "\r").encode())
            wait("running · local")
            os.write(outer, b"\x1b")
            wait("COMMAND OUTPUT")
        os.write(outer, ("run output " + run_id + " stdout 0\r").encode())
        wait("remote\\u001b]52;c;untrusted\\u0007")
        # The one-step path must review and launch the same newly prepared ID.
        before_now = {r["id"] for r in burrow(workspace, "run", "list")}
        os.write(outer, ("run now " + connection[:-1] + "\t").encode())
        wait("run now " + connection + " -- ")
        os.write(outer, b"printf %s --yes\r")
        wait("Proceed?")
        pending, = [r for r in burrow(workspace, "run", "list") if r["id"] not in before_now]
        assert pending["state"] == "prepared" and pending["command"] == ["printf", "%s", "--yes"]
        os.write(outer, b"\t\r")
        wait('"launchRunID"', '"id": "' + pending["id"] + '"')
        assert {r["id"] for r in burrow(workspace, "run", "list")} == before_now | {pending["id"]}
        deadline = time.monotonic() + 10
        while burrow(workspace, "run", "inspect", pending["id"])["state"] == "running":
            assert time.monotonic() < deadline
            time.sleep(.1)
        assert base64.b64decode(burrow(workspace, "run", "output", pending["id"], "stdout", "0")["data"]) == b"--yes"
        burrow(workspace, "run", "close", pending["id"], "--yes")
        os.write(outer, b"\x0c")
        wait("Collected output")
        os.write(outer, ("/" + pending["id"] + "\r").encode())
        wait(pending["id"])
        os.write(outer, b"/STDOUT\r")
        wait("    --yes")
        os.write(outer, b"\x0c")
        wait("COMMAND OUTPUT")
        os.write(outer, ("run now " + connection + (" --local" if local else "") + " --script " + shlex.quote(script.name) + " --mode " + mode + " --interpreter /bin/sh --\r").encode())
        wait("Proceed?")
        if local:
            wait("local (on the daemon host)")
        cancelled, = [r for r in burrow(workspace, "run", "list") if r["id"] not in before_now]
        os.write(outer, b"\x1b")
        wait("COMMAND OUTPUT")
        assert burrow(workspace, "run", "inspect", cancelled["id"])["state"] == "prepared"
        os.write(outer, ("run launch " + cancelled["id"] + " --collect\r").encode())
        wait("Proceed?")
        os.write(outer, b"\t\r")
        wait('"launchRunID"', '"id": "' + cancelled["id"] + '"')
        completed = burrow(workspace, "run", "inspect", cancelled["id"])
        if local:
            assert completed["localExit"] == 7 and completed["remoteExit"] is None, completed
        else:
            assert completed["remoteExit"] == 7 and completed["stageCleanup"] == "removed", completed
        burrow(workspace, "run", "close", cancelled["id"], "--yes")
        os.write(outer, b"quit\r")
        wait("Keep running")
        os.write(outer, b"\r")
        finish()
        assert frontend.returncode == 0 and termios.tcgetattr(slave) == before
        assert burrow(workspace, "run", "inspect", run_id)["state"] == "running"
    finally:
        if frontend.poll() is None:
            frontend.terminate()
            finish()
        os.close(outer)
        os.close(slave)
        burrow(workspace, "run", "cancel", run_id, "--yes")
        burrow(workspace, "run", "close", run_id, "--yes")
