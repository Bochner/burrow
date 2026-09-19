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


def run_checks(burrow, workspace, connection, container, command, hv, binary, env, decoder):
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

    hv("chain", "create", "retained-check", chain="retained-check")
    hv("chain", "add", "burrow@0.1.0", chain="retained-check")
    hv("target", "add", "local", chain="retained-check")
    hv("chain", "config", "set", "workspace", str(workspace), chain="retained-check")

    def hovel_run(*args):
        hv("chain", "config", "set", "command", shlex.join(["run", *args]), chain="retained-check")
        result = json.loads(hv("throw", "--now", "--allow-dangerous", "--json", chain="retained-check"))["results"][0]
        assert result["state"] == "succeeded", result
        return json.loads(result["summary"])

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

    # Selected-master loss refuses launch and cannot make a new authenticated
    # connection. Restore only the fixture socket name; the owner remains exact.
    missing = run("prepare", connection["name"], "--", "true")
    loss = run("prepare", connection["name"], "--", "sleep", "60")
    run("launch", loss["id"], "--yes")
    socket = Path(connection["socket"])
    hidden = socket.with_name("master-hidden")
    socket.rename(hidden)
    try:
        run("launch", missing["id"], "--yes", ok=False)
        assert not socket.exists()
        uncertain = run("cancel", loss["id"], "--yes")
        assert uncertain["cancellation"].startswith("unconfirmed"), uncertain
        assert uncertain["remoteExit"] is None
    finally:
        hidden.rename(socket)
    run("cancel", missing["id"], "--yes")
    run("close", missing["id"], "--yes")
    assert run("collect", loss["id"], "--yes")["collection"] == "succeeded"
    run("close", loss["id"], "--yes")


def run_ui(binary, env, decoder, burrow, workspace, connection):
    import fcntl
    import pty
    import select
    import struct
    import termios

    prepared = burrow(workspace, "run", "prepare", connection, "--", "/bin/sh", "-c",
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

    def wait(needle):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if select.select([outer], [], [], .05)[0]:
                output.extend(os.read(outer, 65536))
            screen = subprocess.run([decoder, "160", "40"], input=output, capture_output=True, check=True).stdout.decode()
            if needle in screen:
                return
            assert frontend.poll() is None, screen
        raise AssertionError((needle, screen))

    try:
        wait(connection)
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
        wait('"launchRunID"')
        os.write(outer, ("run output " + run_id + " stdout 0\r").encode())
        wait("remote\\u001b]52;c;untrusted\\u0007")
        os.write(outer, b"quit\r")
        wait("Keep running")
        os.write(outer, b"\r")
        frontend.wait(timeout=10)
        assert frontend.returncode == 0 and termios.tcgetattr(slave) == before
        assert burrow(workspace, "run", "inspect", run_id)["state"] == "running"
    finally:
        if frontend.poll() is None:
            frontend.terminate()
            frontend.wait(timeout=5)
        os.close(outer)
        os.close(slave)
        burrow(workspace, "run", "cancel", run_id, "--yes")
        burrow(workspace, "run", "close", run_id, "--yes")
