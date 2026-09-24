"""Retained SSH shells through the production CLI and declared SSH fixture."""
import base64
from concurrent.futures import ThreadPoolExecutor
import http.client
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import time


def session_checks(burrow, w, first, command, container, wait, options, root, daemons, binary, env):
    # A controlled login startup proves real remote shell PIDs without exposing
    # any input back door before the shared-controller implementation.
    startup = root / "session-profile.sh"
    startup.write_text('case $- in *i*) echo $$ > /tmp/burrow-session-$$; printf INITIAL-GEOMETRY:; stty size; '
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
    def private(action, request, shell=one, ok=True):
        p = subprocess.run([binary, "--workspace", str(w), "session", action,
                            "gateway", shell["id"], "--request-stdin"],
                           input=json.dumps(request), capture_output=True, text=True,
                           env=env, timeout=15)
        if ok is None:
            return p.returncode, json.loads(p.stdout or p.stderr)
        assert (p.returncode == 0) == ok, (action, p.stdout, p.stderr)
        return json.loads(p.stdout if ok else p.stderr)

    controller = private("claim", {"label": "agent-one"})
    assert controller["token"] and controller["generation"] > 0
    accepted = private("input", {"token": controller["token"], "data": base64.b64encode(b"printf 'SHARED-%s\\n' shell\n").decode()})
    assert accepted["acceptedBytes"] > 0 and "exitCode" not in accepted
    observed = wait(lambda: (r if b"SHARED-shell" in base64.b64decode((r := burrow(w, "session", "observe", "gateway", one["id"]))["data"]) else None))
    assert observed["gap"] is False
    assert base64.b64decode(burrow(w, "session", "observe", "gateway", one["id"])["data"]).startswith(base64.b64decode(observed["data"]))
    # Separate no-TTY processes race for the same observed generation.
    def takeover(label):
        p = subprocess.run([binary, "--workspace", str(w), "session", "takeover",
                            "gateway", one["id"], "--request-stdin"],
                           input=json.dumps({"label": label, "generation": controller["generation"],
                                             "columns": 101, "rows": 31}),
                           capture_output=True, text=True, env=env, timeout=15)
        return p.returncode, json.loads(p.stdout or p.stderr)
    with ThreadPoolExecutor(2) as clients:
        competing = list(clients.map(takeover, ("agent-two", "agent-three")))
    assert sorted(code for code, _ in competing) == [0, 1], competing
    winner = next(result for code, result in competing if code == 0)
    for action, extra in (("input", {"data": "YQ=="}), ("resize", {"columns": 1, "rows": 1}), ("release", {})):
        private(action, {"token": controller["token"], **extra}, ok=False)
    private("input", {"token": winner["token"], "data": base64.b64encode(b"stty size\n").decode()})
    wait(lambda: b"31 101" in base64.b64decode(burrow(w, "session", "observe", "gateway", one["id"])["data"]))
    private("release", {"token": winner["token"]})
    released = burrow(w, "session", "inspect", "gateway", one["id"])
    assert released["controller"] == "" and (released["columns"], released["rows"]) == (101, 31)
    print("PASS independent CLI readers, concurrent takeover, stale authority and release", flush=True)
    controller = private("claim", {})
    # Alternate-screen content must survive loss of its original paint bytes.
    draw = b"stty -echo; printf '\\033[?1049h\\033[2J\\033[HKEPT-SCREEN\\033[31;1H'; head -c 100000 /dev/zero | tr '\\000' '\\007'; printf '\\033[3;5HREDRAW-END'; stty echo\n"
    private("input", {"token": controller["token"], "data": base64.b64encode(draw).decode()})
    wait(lambda: burrow(w, "session", "inspect", "gateway", one["id"])["dropped"] > 0)
    missing = burrow(w, "session", "observe", "gateway", one["id"], "0")
    assert missing["gap"] and missing["synchronization"] == "out-of-sync" and missing["next"] == 0
    screen = wait(lambda: (s if "REDRAW-END" in (s := burrow(w, "session", "snapshot", "gateway", one["id"]))["screen"]["text"] else None))
    assert "KEPT-SCREEN" in screen["screen"]["text"] and screen["screen"]["alternate"]
    assert screen["synchronization"] == "snapshot-current" and screen["next"] > 100000
    assert not burrow(w, "session", "observe", "gateway", one["id"], str(screen["next"]))["gap"]
    private("input", {"token": controller["token"], "data": base64.b64encode(b"printf '\\033[?1049l'\n").decode()})
    print("PASS explicit current-screen recovery after byte-history truncation", flush=True)
    sized, remote_sized = create(w, "gateway", "--columns", "93", "--rows", "27")
    initial = wait(lambda: (r if b"INITIAL-GEOMETRY:27 93" in base64.b64decode((r := burrow(w, "session", "observe", "gateway", sized["id"]))["data"]) else None))
    assert (initial["shell"]["columns"], initial["shell"]["rows"]) == (93, 27)
    burrow(w, "session", "close", "gateway", sized["id"], "--yes")
    wait(lambda: not remote_live(remote_sized))
    # Malformed private objects fail at the owner too, not just CLI parsing.
    def typed(action, payload, session=one["id"], **extra):
        return rpc("RunSessionCommand", {"SessionID": session, "Request": {
            "command": action, "inputEncoding": "utf-8", "inputData": json.dumps(payload), **extra}})
    for payload in ({"token": controller["token"], "columns": 0, "rows": 24},
                    {"token": controller["token"], "columns": None, "rows": 24},
                    {"token": controller["token"], "columns": 1000, "rows": 1000},
                    {"token": "observer", "columns": 80, "rows": 24}):
        assert typed("resize", payload)[0] != 200
    assert typed("claim", {"rows": None}, args=[])[0] != 200
    assert typed("takeover", {"generation": controller["generation"], "rows": None})[0] != 200
    for data in ("", "not-base64", base64.b64encode(b"x" * 4097).decode()):
        assert typed("input", {"token": controller["token"], "data": data})[0] != 200
    assert typed("input", {"token": controller["token"], "data": "YQ==", "unexpected": "secret-canary"})[0] != 200
    assert typed("input", {"token": controller["token"], "data": "YQ=="}, args=[controller["token"]])[0] != 200
    assert control("input", [controller["token"], "YQ=="], one["id"])[0] != 200
    for cols, rows in ((0, 24), (80, -1), (1001, 24), (1000, 1000)):
        burrow(w, "session", "create", "gateway", "--columns", str(cols), "--rows", str(rows), "--yes", ok=False)
    geometry_review = burrow(w, "session", "create", "gateway", "--columns", "99")
    burrow(w, "session", "create", "gateway", "--columns", "98", "--yes", "--review", geometry_review["digest"], ok=False)
    # A resumed fast reader observes new output; a slow one remains out of sync.
    offset = burrow(w, "session", "snapshot", "gateway", one["id"])["next"]
    private("input", {"token": controller["token"], "data": base64.b64encode(b"sleep 60\n").decode()})
    time.sleep(.2)
    private("input", {"token": controller["token"], "data": "Aw=="})
    private("input", {"token": controller["token"], "data": base64.b64encode(b"printf 'INTERRUPTED-%s\\n' done\n").decode()})
    wait(lambda: b"INTERRUPTED-done" in base64.b64decode(burrow(w, "session", "observe", "gateway", one["id"], str(offset))["data"]))
    assert burrow(w, "session", "observe", "gateway", one["id"], "0")["synchronization"] == "out-of-sync"
    private("resize", {"token": controller["token"], "columns": 111, "rows": 33})
    private("input", {"token": controller["token"], "data": base64.b64encode(b"stty size\n").decode()})
    wait(lambda: b"33 111" in base64.b64decode(burrow(w, "session", "observe", "gateway", one["id"], str(offset))["data"]))
    # An oversized rendered link must not return an apparently recovered view.
    second_control = private("claim", {"columns": 20, "rows": 300}, shell=two)
    oversized = b"stty -echo; printf '\\033]8;;'; head -c 300000 /dev/zero | tr '\\000' a; printf '\\033\\\\'; for i in $(seq 1 300); do printf 'HUGE-LINK\\r\\n'; done; printf '\\033]8;;\\033\\\\'; stty echo\n"
    private("input", {"token": second_control["token"], "data": base64.b64encode(oversized).decode()}, shell=two)
    wait(lambda: burrow(w, "session", "inspect", "gateway", two["id"])["received"] > 303000)
    owner_memory = lambda: int(Path(f'/proc/{two["ownerPID"]}/statm').read_text().split()[1]) * os.sysconf("SC_PAGE_SIZE")
    before_snapshot = owner_memory()
    failed_snapshot = wait(lambda: (s if (s := burrow(w, "session", "snapshot", "gateway", two["id"]))["synchronization"] == "out-of-sync" else None))
    assert "screen" not in failed_snapshot and failed_snapshot["recoveryError"], failed_snapshot
    assert owner_memory() - before_snapshot < 32 << 20, "snapshot rendered oversized repeated links before checking its budget"
    private("input", {"token": second_control["token"], "data": base64.b64encode(b"printf '\\033[?25l\\033cAFTER-RESET'\n").decode()}, shell=two)
    recovered = wait(lambda: (s if "AFTER-RESET" in (s := burrow(w, "session", "snapshot", "gateway", two["id"])).get("screen", {}).get("text", "") else None))
    assert recovered["synchronization"] == "snapshot-current" and recovered["screen"]["visible"], recovered
    # The multiplexing master owns the passed local PTY descriptor. Block the
    # remote raw reader instead, then fill its SSH window via the public route.
    blocked, remote_blocked = create()
    blocked_control = private("claim", {}, shell=blocked)
    private("input", {"token": blocked_control["token"], "data": base64.b64encode(b"stty raw -echo; printf 'BLOCKED-%s' ready; sleep 60\n").decode()}, shell=blocked)
    wait(lambda: b"BLOCKED-ready" in base64.b64decode(burrow(w, "session", "observe", "gateway", blocked["id"])["data"]))
    started = time.monotonic()
    for _ in range(2048):
        code, body = typed("input", {"token": blocked_control["token"], "data": base64.b64encode(b"x" * 4096).decode()}, session=blocked["id"])
        assert code == 200, body
        accepted = json.loads(json.loads(body)["stdout"])
        if accepted["backpressure"]:
            assert accepted["acceptedBytes"] < 4096
            break
    else:
        raise AssertionError("blocked remote reader did not apply bounded backpressure")
    with ThreadPoolExecutor(2) as clients:
        sending = clients.submit(private, "input", {"token": blocked_control["token"], "data": "YQ=="}, blocked, None)
        closing = clients.submit(burrow, w, "session", "close", "gateway", blocked["id"], "--yes")
        status, outcome = sending.result()
        assert status in (0, 1)
        if status == 0:
            assert outcome["acceptedBytes"] in (0, 1)
        else:
            assert outcome["error"]["operation"] == "session.input"
        closed = closing.result()
        assert closed["state"] == "closed" and "unconfirmed" in closed["cleanup"]
    assert time.monotonic() - started < 15, "backpressure stalled control/close"
    private("input", {"token": blocked_control["token"], "data": "YQ=="}, shell=blocked, ok=False)
    assert not Path(f'/proc/{blocked["pid"]}').exists()
    # A shell waiting for its foreground sleep can defer HUP handling. The
    # contract promises local reaping and explicit remote-outcome uncertainty.
    tokens = [controller["token"], winner["token"], blocked_control["token"], second_control["token"]]
    for path in w.rglob("*"):
        if path.is_file() and not path.is_symlink():
            saved = path.read_bytes()
            assert all(token.encode() not in saved for token in tokens), ("token persisted", path.name)
    print("PASS initial/live geometry, private validation, Ctrl+C, slow reader, backpressure and close/input race", flush=True)
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
    code, body = control("observe", [str(screen["next"])], one["id"])
    assert code == 200 and not json.loads(json.loads(body)["stdout"])["gap"]
    for method in ("ReadSession", "TailSession"):
        code, body = rpc(method, {"SessionID": one["id"]})
        assert code == 200 and not json.loads(body).get("Data"), (method, body)
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
    assert burrow(w, "session", "observe", "gateway", exited["id"])["closed"]
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
                observation = burrow(workspace, "session", "observe", "target", shell["id"])
                assert observation["lost"] and observation["shell"]["state"] in ("lost", "unavailable"), observation
                if loss == "module":
                    burrow(workspace, "close", "target", "--yes", ok=False)
                    assert Path(state["socket"]).exists(), "uncertain cleanup removed owner"
                burrow(workspace, "session", "create", "target", "--yes", "--review", "0" * 64, ok=False)
        wait(lambda: not remote_live(remote))
    command("docker", "exec", container, "rm", "/etc/profile.d/burrow-session.sh")
    print("PASS retained remote PIDs, launcher exit, selected/connection/manager close, stale/wrong-owner refusal, bounded flood and loss", flush=True)
