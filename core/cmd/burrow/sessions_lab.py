"""Retained SSH shells through the production CLI and declared SSH fixture."""
import base64
import http.client
import json
import os
from pathlib import Path
import signal
import socket
import time


def session_checks(burrow, w, first, command, container, wait, options, root, daemons):
    # A controlled login startup proves real remote shell PIDs without exposing
    # any input back door before the shared-controller implementation.
    startup = root / "session-profile.sh"
    startup.write_text('case $- in *i*) echo $$ > /tmp/burrow-session-$$; '
                       'if test -f /tmp/burrow-session-exit; then exit 7; fi; '
                       'if test -f /tmp/burrow-session-flood; then '
                       'while :; do printf "bounded-session-output-0123456789\\n"; done; fi;; esac\n')
    command("docker", "cp", startup, container + ":/etc/profile.d/burrow-session.sh")

    def remote_pids():
        return set(command("docker", "exec", container, "sh", "-c",
                           "cat /tmp/burrow-session-[0-9]* 2>/dev/null || true").split())

    def remote_live(pid):
        return command("docker", "exec", container, "sh", "-c",
                       f"if test -r /proc/{pid}/stat; then awk '{{print $3}}' /proc/{pid}/stat; fi").strip() not in ("", "Z")

    def create(workspace=w, name="gateway", *flags):
        before = remote_pids()
        shell = burrow(workspace, "session", "create", name, "--yes", *flags)
        remote = wait(lambda: remote_pids() - before)
        assert len(remote) == 1, remote
        return shell, remote.pop()

    def rpc(method, request):
        connection = http.client.HTTPConnection("localhost", timeout=10)
        connection.sock = socket.socket(socket.AF_UNIX)
        connection.sock.settimeout(10)
        connection.sock.connect(str(w / "hoveld.sock"))
        connection.request("POST", "/hovel.daemon.v1.DaemonService/" + method,
                           json.dumps(request), {"Content-Type": "application/json"})
        response = connection.getresponse()
        result = response.status, response.read()
        connection.close()
        return result

    def control(command, args, session=first["session"]):
        return rpc("RunSessionCommand", {"SessionID": session, "Request": {"command": command, "args": args}})

    close_review = burrow(w, "close", "gateway")
    review = burrow(w, "session", "create", "gateway")
    assert review["digest"] and burrow(w, "session", "list", "gateway") == []
    burrow(w, "session", "create", "gateway", "--yes", "--review", "0" * 64, ok=False)
    assert burrow(w, "session", "list", "gateway") == []
    one, remote_one = create(w, "gateway", "--review", review["digest"])
    two, remote_two = create()
    assert one["id"] != two["id"]
    assert remote_one != remote_two and remote_live(remote_one) and remote_live(remote_two)
    burrow(w, "close", "gateway", "--yes", "--review", close_review["digest"], ok=False)
    burrow(w, "session", "create", "gateway", "--yes", "--review", review["digest"], ok=False)
    for shell in (one, two):
        # Each burrow() invocation has exited before the next one starts.
        seen = burrow(w, "session", "inspect", "gateway", shell["id"])
        assert seen["state"] == "running" and Path(f'/proc/{seen["pid"]}').exists(), seen
        assert seen["connection"]["creation"] == first["creation"]
    assert {s["id"] for s in burrow(w, "session", "list", "gateway")} == {one["id"], two["id"]}
    # Hovel polls Read every 250ms. An empty immediate return spins RPCs even
    # while the shell is idle; allow ample overhead without a CPU-speed limit.
    def read_calls():
        counters = dict(line.split(": ") for line in Path(f'/proc/{one["ownerPID"]}/io').read_text().splitlines())
        return int(counters["syscr"])
    before = read_calls()
    time.sleep(.6)
    assert read_calls() - before < 100, "idle session ignored the broker read wait"
    # Normal discovery remains usable after recognized shells are registered.
    burrow(w, "connect", "shell-peer", "127.0.0.1", "tester", *options)
    peer = wait(lambda: (s if (s := burrow(w, "inspect", "shell-peer"))["state"] == "connected" else None))
    burrow(w, "session", "inspect", "shell-peer", one["id"], ok=False)
    burrow(w, "session", "close", "shell-peer", one["id"], "--yes", ok=False)
    assert control("shell-close", ["wrong-generation", first["creation"], one["id"], "0" * 64])[0] != 200
    # Both advertised and unadvertised raw I/O must fail closed.
    assert rpc("WriteSession", {"SessionID": one["id"], "Data": base64.b64encode(b"touch /tmp/BYPASS\n").decode()})[0] != 200
    assert control("input", ["invented-token", "YQ=="], one["id"])[0] != 200
    assert control("resize", ["120", "30"], one["id"])[0] != 200
    assert control("observe", ["0"], one["id"])[0] != 200
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    tunnel = burrow(w, "tunnel", "create", "gateway", "forward", str(port), "127.0.0.1", "2222", "--yes")
    command("docker", "exec", container, "sh", "-c", "printf shell-sibling-transfer > /tmp/burrow-shell-transfer")
    file_review = burrow(w, "scp", "gateway", "get", "/tmp/burrow-shell-transfer")
    download = burrow(w, "scp", "gateway", "get", "/tmp/burrow-shell-transfer", "--yes", "--review", file_review["digest"])
    burrow(w, "session", "close", "gateway", one["id"], "--yes")
    assert not Path(f'/proc/{one["pid"]}').exists()
    wait(lambda: not remote_live(remote_one))
    assert burrow(w, "session", "inspect", "gateway", two["id"])["state"] == "running"
    assert burrow(w, "inspect", "gateway")["masterPID"] == first["masterPID"]
    with socket.create_connection(("127.0.0.1", port), timeout=3) as forwarded:
        assert forwarded.recv(64).startswith(b"SSH-")
    wait(lambda: burrow(w, "downloads", download["id"])["state"] == "complete")
    assert (w / "burrow-files/downloads/burrow-shell-transfer").read_bytes() == b"shell-sibling-transfer"
    burrow(w, "tunnel", "remove", tunnel["id"], "--yes")
    burrow(w, "session", "close", "gateway", two["id"], "--yes")
    wait(lambda: not remote_live(remote_two))

    burrow(w, "connect", "native-close", "127.0.0.1", "tester", *options)
    wait(lambda: burrow(w, "inspect", "native-close")["state"] == "connected")
    native, remote_native = create(w, "native-close")
    assert rpc("CloseSession", {"SessionID": native["id"]})[0] == 200
    wait(lambda: not remote_live(remote_native))
    assert burrow(w, "session", "list", "native-close") == []
    assert burrow(w, "inspect", "native-close")["shellCount"] == 0
    burrow(w, "close", "native-close", "--yes")

    burrow(w, "connect", "audit-close", "127.0.0.1", "tester", *options)
    audit_owner = wait(lambda: (s if (s := burrow(w, "inspect", "audit-close"))["state"] == "connected" else None))
    audited, remote_audited = create(w, "audit-close")
    log = w / "burrow-logs/operations.log"
    log.chmod(0o400)
    try:
        burrow(w, "close", "audit-close", "--yes", ok=False)
        wait(lambda: not remote_live(remote_audited))
        assert not Path(audit_owner["socket"]).parent.exists(), "audit failure stranded an otherwise cleaned owner"
    finally:
        log.chmod(0o600)

    # A direct SDK close can physically succeed while its audit fails. The
    # closed broker record must still expose the live owner's cleanup result.
    audited, remote_audited = create()
    log.chmod(0o400)
    try:
        assert rpc("CloseSession", {"SessionID": audited["id"]})[0] != 200
        wait(lambda: not remote_live(remote_audited))
        seen = burrow(w, "session", "inspect", "gateway", audited["id"])
        assert seen["state"] == "closed" and seen["auditError"] and "reaped" in seen["cleanup"], seen
    finally:
        log.chmod(0o600)
    burrow(w, "session", "close", "gateway", audited["id"], "--yes", ok=False)
    assert burrow(w, "session", "list", "gateway") == []

    command("docker", "exec", container, "touch", "/tmp/burrow-session-exit")
    exited, remote_exited = create()
    command("docker", "exec", container, "rm", "/tmp/burrow-session-exit")
    seen = wait(lambda: (s if (s := burrow(w, "session", "inspect", "gateway", exited["id"]))["state"] == "exited" else None))
    assert seen["sshExit"] == 7 and burrow(w, "inspect", "gateway")["state"] == "connected", seen
    assert not remote_live(remote_exited)
    burrow(w, "session", "close", "gateway", exited["id"], "--yes")

    command("docker", "exec", container, "touch", "/tmp/burrow-session-flood")
    flood, remote_flood = create()
    command("docker", "exec", container, "rm", "/tmp/burrow-session-flood")
    wait(lambda: burrow(w, "session", "inspect", "gateway", flood["id"])["received"] > 1 << 20)
    memory = lambda: int(Path(f'/proc/{flood["ownerPID"]}/statm').read_text().split()[1]) * os.sysconf("SC_PAGE_SIZE")
    before = memory()
    time.sleep(.5)
    start = time.monotonic()
    snapshot = burrow(w, "session", "inspect", "gateway", flood["id"])
    assert snapshot["buffered"] <= 65536 and snapshot["dropped"] > 0, snapshot
    assert memory() - before < 32 << 20, "output queue grew without bound"
    closed = burrow(w, "session", "close", "gateway", flood["id"], "--yes")
    assert closed["received"] >= snapshot["received"] and closed["dropped"] >= snapshot["dropped"], closed
    assert closed["state"] == "closed" and "reaped" in closed["detail"], closed
    assert time.monotonic() - start < 10, "output blocked close/control"
    wait(lambda: not remote_live(remote_flood))

    # A connection review binds dependent-shell membership even inside the owner.
    close_review = burrow(w, "close", "shell-peer")
    peer_shell, remote_peer = create(w, "shell-peer")
    peer_two, remote_peer_two = create(w, "shell-peer")
    code, _ = control("close-selected", [first["generation"], peer["creation"], close_review["digest"]])
    assert code != 200 and remote_live(remote_peer)
    saved = (w / "burrow-profiles.json").read_bytes()
    evidence = burrow(w, "run", "now", "shell-peer", "--yes", "--", "printf", "retained-shell-evidence")
    output = burrow(w, "run", "output", evidence["id"], "stdout", "0")
    original = Path(peer["socket"])
    preserved = original.with_name("preserved-master")
    original.rename(preserved)
    foreign = socket.socket(socket.AF_UNIX)
    try:
        foreign.bind(str(original))
        foreign.listen()
        burrow(w, "close", "shell-peer", "--yes", ok=False)
        assert remote_live(remote_peer) and original.exists(), "replaced ownership was touched"
    finally:
        foreign.close()
        original.unlink()
        preserved.rename(original)
    burrow(w, "close", "shell-peer", "--yes")
    wait(lambda: not remote_live(remote_peer))
    wait(lambda: not remote_live(remote_peer_two))
    assert not Path(f'/proc/{peer_shell["pid"]}').exists()
    assert not Path(f'/proc/{peer_two["pid"]}').exists()
    assert burrow(w, "inspect", "gateway")["masterPID"] == first["masterPID"]
    assert (w / "burrow-profiles.json").read_bytes() == saved
    assert burrow(w, "run", "output", evidence["id"], "stdout", "0") == output

    # Destructive loss scenarios use separate owners, preserving the main suite.
    for loss in ("master", "module", "manager", "daemon", "retire"):
        workspace = root / ("session-" + loss)
        info = burrow(workspace, "--offline", "workspace", "open")
        daemons.append(info["pid"])
        burrow(workspace, "connect", "target", "127.0.0.1", "tester", *options)
        state = wait(lambda: (s if (s := burrow(workspace, "inspect", "target"))["state"] == "connected" else None))
        shell, remote = create(workspace, "target")
        if loss == "retire":
            review = burrow(workspace, "workspace", "retire")
            second, remote_second = create(workspace, "target")
            burrow(workspace, "workspace", "retire", "--yes", "--review", review["digest"])
            wait(lambda: not remote_live(remote_second))
            assert not Path(f'/proc/{second["pid"]}').exists()
            assert burrow(workspace, "connections") == []
        else:
            pid = {"master": state["masterPID"], "module": shell["ownerPID"], "manager": state["ownerPID"], "daemon": info["pid"]}[loss]
            os.kill(pid, signal.SIGKILL)
            if loss == "daemon":
                burrow(workspace, "session", "inspect", "target", shell["id"], ok=False)
                burrow(workspace, "status", ok=False)  # Stale receipts require manual recovery.
                workspace = root / "session-fresh-daemon"
                relaunched = burrow(workspace, "status")
                daemons.append(relaunched["pid"])
                assert burrow(workspace, "session", "list", "target") == []
            else:
                seen = wait(lambda: (s if (s := burrow(workspace, "session", "inspect", "target", shell["id"]))["state"] in ("lost", "unavailable") else None))
                assert "uncertain" in seen["detail"], seen
                if loss == "module":
                    burrow(workspace, "close", "target", "--yes", ok=False)
                    assert Path(state["socket"]).exists(), "uncertain cleanup removed owner"
                burrow(workspace, "session", "create", "target", "--yes", "--review", "0" * 64, ok=False)
        wait(lambda: not remote_live(remote))
    command("docker", "exec", container, "rm", "/etc/profile.d/burrow-session.sh")
    print("PASS retained remote PIDs, launcher exit, selected/connection/manager close, stale/wrong-owner refusal, bounded flood and loss", flush=True)
