"""Independent live readers through production commands and controlling PTYs."""
import base64
from concurrent.futures import ThreadPoolExecutor
import fcntl
import json
import os
from pathlib import Path
import pty
import re
import resource
import select
import shlex
import signal
import struct
import subprocess
import termios
import time


def follow_checks(burrow, workspace, connection, container, command, hv, binary, env, decoder):
    activity_checks(burrow, workspace, connection, container, command, hv, binary, env, decoder)
    def run(*args, **kw):
        return burrow(workspace, "run", *args, **kw)

    prepared = run("prepare", connection["name"], "--budget", "300000", "--", "/bin/sh", "-c",
                   "printf 'first\\n'; printf 'warning\\n' >&2; "
                   "while [ ! -f /tmp/burrow-follow-release ]; do sleep .1; done; "
                   "head -c 131072 /dev/zero; printf 'after-preview\\n'; exit 7")
    run_id = prepared["id"]
    try:
        launched = run("launch", run_id, "--yes")
        first = run("output", run_id, "stdout", "0")
        assert base64.b64decode(first["data"]) == b"first\n", first
        assert first["state"] == "running" and first["remoteExit"] is None, first
        assert first["storedBytes"] == 6 and first["budgetPerStream"] == 300000, first
        assert first["nextOffset"] == 6 and not first["outputComplete"], first
        hv("chain", "create", "follow-check", chain="follow-check")
        hv("chain", "add", "burrow@0.1.0", chain="follow-check")
        hv("target", "add", "local", chain="follow-check")
        hv("chain", "config", "set", "workspace", str(workspace), chain="follow-check")
        hv("chain", "config", "set", "command", shlex.join(["run", "output", run_id, "stderr", "0"]), chain="follow-check")
        module_result = json.loads(hv("throw", "--now", "--allow-dangerous", "--json", chain="follow-check"))["results"][0]
        assert module_result["state"] == "succeeded", module_result
        module_chunk = json.loads(module_result["summary"])
        assert module_chunk["state"] == "running" and base64.b64decode(module_chunk["data"]) == b"warning\n"
        command("docker", "exec", container, "touch", "/tmp/burrow-follow-release")
        deadline = time.monotonic() + 15
        while run("inspect", run_id)["state"] == "running":
            assert time.monotonic() < deadline
            time.sleep(.05)

        def read_from(offset):
            data = bytearray()
            while True:
                chunk = run("output", run_id, "stdout", str(offset))
                part = base64.b64decode(chunk["data"])
                assert len(part) <= 32768
                assert chunk["nextOffset"] == offset + len(part), chunk
                assert chunk["state"] == "exited" and chunk["remoteExit"] == 7, chunk
                assert chunk["outputComplete"] and not chunk["outputError"], chunk
                data.extend(part)
                offset = chunk["nextOffset"]
                if not part:
                    return bytes(data)

        expected = b"\0" * 131072 + b"after-preview\n"
        log = workspace / "burrow-logs/operations.log"
        notes = log.read_bytes()
        with ThreadPoolExecutor(max_workers=3) as readers:
            a = readers.submit(read_from, first["nextOffset"])
            b = readers.submit(read_from, 0)
            c = readers.submit(run, "output", run_id, "stderr", "0")
            assert a.result() == expected
            assert b.result() == b"first\n" + expected
            assert base64.b64decode(c.result()["data"]) == b"warning\n"
        assert log.read_bytes() == notes, "output reads wrote viewing data to operation logs"
        state = run("inspect", run_id)
        assert state["launchRunID"] == launched["launchRunID"] and not state.get("collection")
        print("PASS live status, bounded resume, concurrent independent readers and beyond-preview bytes", flush=True)
    finally:
        command("docker", "exec", container, "touch", "/tmp/burrow-follow-release")
        run("cancel", run_id, "--yes")
        run("close", run_id, "--yes")

    for disk in (False, True):
        item = run("prepare", connection["name"], "--budget", "16384" if disk else "7", "--",
                   "/bin/sh", "-c", "sleep 2; head -c 8192 /dev/zero; exit 4")
        previous_limit = None
        try:
            run("launch", item["id"], "--yes")
            if disk:
                previous_limit = resource.prlimit(item["ownerPID"], resource.RLIMIT_FSIZE)
                resource.prlimit(item["ownerPID"], resource.RLIMIT_FSIZE, (1024, previous_limit[1]))
            deadline = time.monotonic() + 15
            while run("inspect", item["id"])["state"] == "running":
                assert time.monotonic() < deadline
            if disk:
                resource.prlimit(item["ownerPID"], resource.RLIMIT_FSIZE, previous_limit)
            chunk = run("output", item["id"], "stdout", "0")
            assert not chunk["outputComplete"] and chunk["remoteExit"] == 4, chunk
            assert ("write failed" if disk else "budget exceeded") in chunk["outputError"], chunk
            assert len(base64.b64decode(chunk["data"])) == (1024 if disk else 7)
            assert chunk["receivedBytes"] == 8192
            assert run("collect", item["id"], "--yes")["collection"] == "succeeded"
        finally:
            if previous_limit:
                resource.prlimit(item["ownerPID"], resource.RLIMIT_FSIZE, previous_limit)
            run("cancel", item["id"], "--yes")
            run("close", item["id"], "--yes")
    print("PASS output readers expose budget and kernel write failures with collectible partial bytes", flush=True)
    for plain in (False, True):
        follow_ui(burrow, workspace, connection["name"], container, command, hv, binary, env, decoder, plain)


def activity_checks(burrow, workspace, connection, container, command, hv, binary, env, decoder):
    viewers = []
    consoles = []
    run_id = None
    new_connection = False

    def console(args, plain=False):
        outer, slave = pty.openpty()
        before = termios.tcgetattr(slave)
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))
        def controlling():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)
        p = subprocess.Popen([binary, "--workspace", str(workspace), *args],
                             env=env | {"NO_COLOR": "1" if plain else "", "COLORTERM": "truecolor"},
                             stdin=slave, stdout=slave, stderr=slave, preexec_fn=controlling)
        view = {"process": p, "outer": outer, "slave": slave, "before": before, "output": bytearray()}
        consoles.append(view)
        return view

    def drain():
        for view in consoles:
            while select.select([view["outer"]], [], [], 0)[0]:
                view["output"].extend(os.read(view["outer"], 65536))

    def screen(view, needle):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            drain()
            text = subprocess.run([decoder, "160", "40"], input=view["output"], capture_output=True, check=True).stdout
            if needle in text:
                return text
            assert view["process"].poll() is None, text
            time.sleep(.05)
        raise AssertionError((needle, text))

    def start():
        p = subprocess.Popen([binary, "--workspace", str(workspace), "follow", "--json"],
                             env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        view = {"process": p, "pending": b"", "events": []}
        viewers.append(view)
        return view

    def wait(view, predicate):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            drain()
            for event in view["events"]:
                if predicate(event):
                    return event
            p = view["process"]
            if select.select([p.stdout], [], [], .1)[0]:
                chunk = os.read(p.stdout.fileno(), 65536)
                assert chunk, (p.poll(), p.stderr.read())
                lines = (view["pending"] + chunk).split(b"\n")
                view["pending"] = lines.pop()
                view["events"].extend(json.loads(line) for line in lines)
        raise AssertionError(view["events"][-20:])

    try:
        human, tui = console(["follow"]), console(["tui"], plain=True)
        screen(human, b"BURROW FOLLOW")
        screen(tui, connection["name"].encode())
        a, b = start(), start()
        for view in viewers:
            wait(view, lambda e: e["kind"] == "ready")
        burrow(workspace, "profile", "save", connection["name"], "--as", "feed-target")
        burrow(workspace, "profile", "connect", "feed-target", "--as", "activity-host", "--yes")
        new_connection = True
        wait(a, lambda e: e.get("state") == "connected" and e["source"] == "burrow/connection" and "activity-host" in json.dumps(e))
        screen(tui, b"activity-host")
        assert b"activity-host" in re.sub(rb"\x1b\[[0-9;]*m", b"", human["output"])
        # Shared boundary records failed/refused operations without claiming success.
        burrow(workspace, "tunnel", "remove", "activity-host/" + "0" * 32, "--yes", ok=False)
        wait(a, lambda e: e["kind"] == "failed" and "tunnel remove activity-host" in e["message"])
        prepared = burrow(workspace, "run", "prepare", connection["name"], "--", "/bin/sh", "-c",
                          "printf 'activity-ready\\n'; printf 'activity-warning\\n\\303' >&2; "
                          "while [ ! -f /tmp/burrow-activity-release ]; do sleep .1; done; "
                          "printf 'activity-end\\n'; exit 7")
        run_id = prepared["id"]
        # Drive the same base module using documented native Hovel commands.
        hv("chain", "create", "activity-events", chain="activity-events")
        hv("chain", "add", "burrow@0.1.0", chain="activity-events")
        hv("target", "add", "local", chain="activity-events")
        hv("chain", "config", "set", "workspace", str(workspace), chain="activity-events")
        hv("chain", "config", "set", "command", "profiles", chain="activity-events")
        native = json.loads(hv("throw", "--now", "--allow-dangerous", "--json", chain="activity-events"))
        assert native["results"][0]["state"] == "succeeded", native
        wait(a, lambda e: e.get("chain") == "activity-events" and e["message"] == "run completed")
        burrow(workspace, "run", "launch", run_id)  # Review must not launch.
        review = wait(a, lambda e: e["kind"] == "review" and e.get("resourceID") == run_id)
        assert burrow(workspace, "run", "inspect", run_id)["state"] == "prepared", review
        burrow(workspace, "run", "launch", run_id, "--yes")
        wait(a, lambda e: e["kind"] == "started" and e.get("resourceID") == run_id and e["source"] == "burrow/audit")
        for view in (a, b):
            wait(view, lambda e: e["kind"] == "output" and e.get("stream") == "stdout"
                 and b"activity-ready" in base64.b64decode(e["data"]))
            wait(view, lambda e: e["kind"] == "output" and e.get("stream") == "stderr"
                 and b"activity-warning" in base64.b64decode(e["data"]))
        # Initial owner failure must not turn existing captured bytes into live output.
        owner_pid = burrow(workspace, "run", "inspect", run_id)["ownerPID"]
        os.kill(owner_pid, signal.SIGSTOP)
        try:
            recovering = start()
            wait(recovering, lambda e: e["kind"] == "gap" and e["source"] == "burrow/runs")
            wait(recovering, lambda e: e["kind"] == "ready")
        finally:
            os.kill(owner_pid, signal.SIGCONT)
        wait(recovering, lambda e: e["kind"] == "snapshot" and e.get("resourceID") == run_id)
        assert not any(e["kind"] == "output" and e.get("resourceID") == run_id for e in recovering["events"])
        a["process"].send_signal(signal.SIGINT)
        assert a["process"].wait(timeout=5) == 0
        assert burrow(workspace, "run", "inspect", run_id)["state"] == "running"
        command("docker", "exec", container, "touch", "/tmp/burrow-activity-release")
        event = wait(b, lambda e: e["kind"] == "result" and e.get("resourceID") == run_id
                     and e.get("state") == "exited")
        assert event["details"]["remoteExit"] == 7
        wait(b, lambda e: e["kind"] == "output" and b"activity-end" in base64.b64decode(e["data"]))
        resumed_output = wait(recovering, lambda e: e["kind"] == "output" and e.get("resourceID") == run_id)
        assert base64.b64decode(resumed_output["data"]) == b"activity-end\n" and resumed_output["offset"] > 0, resumed_output
        burrow(workspace, "run", "collect", run_id, "--yes")
        wait(b, lambda e: e["kind"] == "collected" and e.get("resourceID") == run_id)
        drain()
        rendered = re.sub(rb"\x1b\[[0-9;]*m", b"", human["output"])
        for expected in (b"activity-ready", b"activity-warning", b"activity-end", b"remote exit: 7", b"\\xc3"):
            assert expected in rendered, (expected, rendered[-6000:])
        assert b"?1049" not in human["output"] and termios.tcgetattr(human["slave"]) == human["before"]
        # A real transfer yields owner-measured bytes and completion.
        command("docker", "exec", container, "sh", "-c", "head -c 4194304 /dev/zero > /tmp/burrow-activity-data")
        plan = burrow(workspace, "scp", connection["name"], "get", "/tmp/burrow-activity-data", "activity-data")
        transfer = burrow(workspace, "scp", connection["name"], "get", "/tmp/burrow-activity-data", "activity-data", "--yes", "--review", plan["digest"])
        wait(b, lambda e: e.get("resourceID") == transfer["id"] and e["kind"] in ("progress", "result"))
        burrow(workspace, "close", "activity-host", "--yes")
        new_connection = False
        wait(b, lambda e: e.get("state") == "closed" and "activity-host" in json.dumps(e))
        print("PASS workspace activity: review, real stdout/stderr, independent viewers, nonzero exit and collection", flush=True)
    finally:
        command("docker", "exec", container, "touch", "/tmp/burrow-activity-release")
        for view in viewers:
            p = view["process"]
            if p.poll() is None:
                p.terminate()
                p.wait(timeout=10)
        for view in consoles:
            p = view["process"]
            if p.poll() is None:
                p.terminate()
                deadline = time.monotonic() + 10
                while p.poll() is None and time.monotonic() < deadline:
                    drain()
                    time.sleep(.02)
                if p.poll() is None:
                    p.kill()
                p.wait(timeout=5)
        for view in consoles:
            os.close(view["outer"])
            os.close(view["slave"])
        if new_connection:
            burrow(workspace, "close", "activity-host", "--yes")
        if run_id:
            burrow(workspace, "run", "cancel", run_id, "--yes")
            burrow(workspace, "run", "close", run_id, "--yes")


def follow_ui(burrow, workspace, connection, container, command, hv, binary, env, decoder, plain):
    prefix = f"/tmp/burrow-live-{int(plain)}"
    script = ("printf 'viewer-ready\\n'; printf 'separate-error\\n' >&2; "
              f"while [ ! -f {prefix}-burst ]; do sleep .05; done; "
              "i=0; while [ $i -lt 2000 ]; do printf 'burst-%04d: abcdefghijklmnopqrstuvwxyz0123456789\\n' $i; i=$((i+1)); done; "
              f"printf '\\303'; touch {prefix}-split; "
              f"while [ ! -f {prefix}-end ]; do sleep .05; done; "
              "printf '\\251\\033]52;c;hostile\\007\\377\\nTAIL-END\\n'; printf 'stderr-end\\n' >&2; exit 7")
    prepared = burrow(workspace, "run", "prepare", connection, "--budget", "300000", "--", "/bin/sh", "-c", script)
    run_id = prepared["id"]
    burrow(workspace, "run", "launch", run_id, "--yes")
    frontends = []
    failed_capture = None
    dangling = None

    def open_view(stream, offset="0", redirect=False):
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
            viewer_env["COLORTERM"] = "truecolor"
        proc = subprocess.Popen([binary, "--workspace", str(workspace), "run", "follow", run_id, stream, offset],
                                env=viewer_env, stdin=slave, stdout=subprocess.DEVNULL if redirect else slave,
                                stderr=slave, preexec_fn=controlling)
        view = {"process": proc, "outer": outer, "slave": slave, "before": before,
                "output": bytearray(), "width": 160, "height": 40}
        frontends.append(view)
        return view

    def drain(view):
        if select.select([view["outer"]], [], [], .02)[0]:
            view["output"].extend(os.read(view["outer"], 65536))
        for _ in range(16):
            if not select.select([view["outer"]], [], [], 0)[0]:
                break
            view["output"].extend(os.read(view["outer"], 65536))

    def finish(view):
        deadline = time.monotonic() + 10
        while view["process"].poll() is None and time.monotonic() < deadline:
            drain(view)  # A terminal keeps reading while the renderer restores it.
        assert view["process"].poll() is not None, "frontend exit stalled"

    def screen(view):
        drain(view)
        return subprocess.run([decoder, str(view["width"]), str(view["height"])],
                              input=view["output"], capture_output=True, check=True).stdout.decode()

    def wait(view, needle, seconds=20):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            text = screen(view)
            if needle in text:
                return text
            assert view["process"].poll() is None, text
        raise AssertionError((needle, text))

    def send(view, value):
        os.write(view["outer"], value)

    def resize(view, width, height):
        while select.select([view["outer"]], [], [], .02)[0]:
            view["output"].extend(os.read(view["outer"], 65536))
        view["output"].clear()
        view["width"], view["height"] = width, height
        fcntl.ioctl(view["slave"], termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
        os.kill(view["process"].pid, signal.SIGWINCH)

    expected = b"viewer-ready\n" + b"".join(f"burst-{i:04d}: abcdefghijklmnopqrstuvwxyz0123456789\n".encode() for i in range(2000))
    closed = False
    try:
        a, b = open_view("stdout"), open_view("stderr", redirect=True)
        wait(a, "viewer-ready")
        wait(b, "separate-error")
        # One viewer leaves during a read; the other keeps its own cursor.
        send(b, b"\x1b")
        wait(b, "COMMAND OUTPUT")
        send(b, b"unfinished-draft")
        wait(b, "unfinished-draft")
        b["process"].terminate()
        finish(b)
        assert burrow(workspace, "run", "inspect", run_id)["state"] == "running"
        b = open_view("stderr", str(len(b"separate-error\n")))
        wait(b, "running")
        command("docker", "exec", container, "touch", prefix + "-burst")
        wait(a, "burst-")
        start = time.monotonic()
        send(a, b"\x1b[5~")
        # Readiness includes scheduling the VT decoder; measure latency without
        # treating a shared runner's one-second scheduling delay as a UI failure.
        wait(a, "PAUSED")
        pause_latency = time.monotonic() - start
        send(a, b"\x1bOP")  # F1, even during output bursts.
        wait(a, "LIVE OUTPUT HELP")
        send(a, b"\x1b")
        wait(a, "PAUSED")
        paused = burrow(workspace, "run", "inspect", run_id)
        assert paused["state"] == "running" and not paused.get("collection")
        send(a, b"\x1b[F")
        wait(a, f"–{len(expected)+1}")  # Incomplete UTF-8 has been captured, not rendered as corruption.
        assert "�" not in screen(a)
        command("docker", "exec", container, "touch", prefix + "-end")
        wait(a, "TAIL-END")
        wait(a, "exit 7")
        capture = wait(a, "é\\u001b]52;c;hostile\\u0007\\xff")
        assert b"\x1b]52;c;hostile" not in a["output"], "remote clipboard control escaped into management"
        for width, height in ((200, 50), (120, 30), (80, 24), (160, 40)):
            resize(a, width, height)
            capture = wait(a, "TAIL-END")
            assert "LIVE OUTPUT" in capture and "exit 7" in capture
            if directory := os.environ.get("TEST_UNDECLARED_OUTPUTS_DIR"):
                Path(directory, f"live-output-{width}x{height}-{plain}.txt").write_text(capture)
        # Close/reopen keeps the exact paused viewport and stream position.
        send(a, b"\x1b[5~")
        wait(a, "PAUSED")
        before_close = screen(a)
        before_rows = re.findall(r"burst-\d{4}: \w+", before_close)
        send(a, b"\x1b")
        wait(a, "COMMAND OUTPUT")
        send(a, ("run follow " + run_id + "\r").encode())
        capture = wait(a, "PAUSED")
        assert "Preview bytes" in capture and "burst-" in capture
        capture = wait(a, before_rows[-1])
        after_rows = re.findall(r"burst-\d{4}: \w+", capture)
        assert before_rows == after_rows, (before_rows, after_rows, capture)
        # The independent viewer can resume stderr and then follow stdout from zero.
        assert "separate-error" not in wait(b, "stderr-end")
        send(b, b"\t")
        wait(b, "TAIL-END")
        # Viewer process disconnect does not close the retained run or collect it.
        a["process"].terminate()
        finish(a)
        state = burrow(workspace, "run", "inspect", run_id)
        assert state["remoteExit"] == 7 and not state.get("collection"), state
        collected = burrow(workspace, "run", "collect", run_id, "--yes")
        artifacts = json.loads(hv("artifact", "list", "--json"))
        stdout = next(item for item in artifacts if item["runId"] == collected["runID"] and item["name"].endswith("-stdout"))
        assert (workspace / stdout["path"]).read_bytes() == expected + b"\xc3\xa9\x1b]52;c;hostile\x07\xff\nTAIL-END\n"
        # A closed run becomes unavailable, never quietly relaunched or reconnected.
        burrow(workspace, "run", "close", run_id, "--yes")
        closed = True
        wait(b, "UNVERIFIED")
        send(b, b"\x1b")
        wait(b, "COMMAND OUTPUT")
        failed_capture = burrow(workspace, "run", "now", connection, "--budget", "7", "--yes", "--", "printf", "partial-output")
        send(b, ("run follow " + failed_capture["id"] + "\r").encode())
        wait(b, "INCOMPLETE")
        wait(b, "budget exceeded")
        wait(b, "exit 0")
        send(b, b"\x03")
        wait(b, "COMMAND OUTPUT")
        dangling = burrow(workspace, "run", "prepare", connection, "--", "/bin/sh", "-c",
                          f"printf '\\303'; while [ ! -f {prefix}-dangling ]; do sleep .05; done")
        burrow(workspace, "run", "launch", dangling["id"], "--yes")
        send(b, ("run follow " + dangling["id"] + "\r").encode())
        wait(b, "Preview bytes 0–1")
        send(b, b"\x1b[5~")
        wait(b, "PAUSED")
        command("docker", "exec", container, "touch", prefix + "-dangling")
        wait(b, "exit 0")
        send(b, b"\x1b[F")
        wait(b, "\\xc3")
        send(b, b"\x03")
        wait(b, "COMMAND OUTPUT")
        send(b, b"quit\r")
        wait(b, "Keep running")
        send(b, b"\r")
        finish(b)
        assert b["process"].returncode == 0 and termios.tcgetattr(b["slave"]) == b["before"]
        print(f"PASS live PTY follow/pause/reopen, two viewers, resize, safe split/binary text and exact evidence; NO_COLOR={plain}; pause response={pause_latency:.3f}s", flush=True)
    finally:
        for view in frontends:
            if view["process"].poll() is None:
                view["process"].terminate()
                try:
                    finish(view)
                finally:
                    if view["process"].poll() is None:
                        view["process"].kill()
                        view["process"].wait(timeout=5)
            os.close(view["outer"])
            os.close(view["slave"])
        command("docker", "exec", container, "touch", prefix + "-end", prefix + "-burst", prefix + "-dangling")
        if not closed:
            burrow(workspace, "run", "cancel", run_id, "--yes")
            burrow(workspace, "run", "close", run_id, "--yes")
        if failed_capture:
            burrow(workspace, "run", "close", failed_capture["id"], "--yes")
        if dangling:
            burrow(workspace, "run", "cancel", dangling["id"], "--yes")
            burrow(workspace, "run", "close", dangling["id"], "--yes")
