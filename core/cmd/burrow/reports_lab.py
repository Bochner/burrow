"""Ubuntu preset and saved report read path through production SSH/Hovel and a PTY."""
import base64
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pty
import select
import shlex
import signal
import struct
import subprocess
import termios
import time


def report_checks(burrow, workspace, connection, hv, binary, env, decoder, survey_script):
    def run(*args, **kwargs):
        return burrow(workspace, "run", *args, **kwargs)

    before = run("list")
    for options in ([], ["--os", "mikrotik"], ["--os", "ubuntu", "--local"], ["--os", "ubuntu", "--timeout", ""], ["--os", "ubuntu", "--", "id"]):
        run("survey", connection["name"], *options, ok=False)
    assert run("list") == before, "refused preset prepared work"
    prepared = run("survey", connection["name"], "--os", "ubuntu")
    assert "Survey: Ubuntu v1" in prepared["review"] and "systemctl" in prepared["review"]
    assert run("inspect", prepared["id"])["state"] == "prepared"
    assert base64.b64decode(run("output", prepared["id"], "stdout", "0")["data"]) == b""
    run("launch", prepared["id"], "--collect", "--review", "wrong", "--yes", ok=False)
    saved = run(*shlex.split(prepared["confirm"])[1:])
    assert saved["collection"] == "succeeded" and saved["outputComplete"], saved
    assert saved["input"]["survey"] == "ubuntu" and saved["input"]["script"]["sha256"] == hashlib.sha256(Path(survey_script).read_bytes()).hexdigest()
    artifacts = json.loads(hv("artifact", "list", "--json"))
    collected = [a for a in artifacts if a["runId"] == saved["runID"]]
    assert len(collected) == 5, collected
    report, = [e for e in burrow(workspace, "reports") if e["sourceRun"] == saved["id"]]
    assert report["connection"] == connection["name"] and report["complete"] and report["exit"] == saved["remoteExit"]
    document = burrow(workspace, "report", report["id"])
    original = (workspace / report["path"]).read_bytes()
    assert document["markdown"].encode() == original
    assert hashlib.sha256(original).hexdigest() == report["sha256"]
    assert b"## Coverage" in original and b"Current user and groups" in original and b"Status: OBSERVED" in original
    assert b"UNAVAILABLE" in original or b"FAILED" in original, "non-Ubuntu lab silently claimed full coverage"
    assert saved["remoteExit"] == 1, saved
    log = workspace / "burrow-logs/operations.log"
    notes = log.read_bytes()
    for _ in range(2):
        assert burrow(workspace, "report", report["id"]) == document
    assert log.read_bytes() == notes, "viewing wrote to operation log"
    run("close", saved["id"], "--yes")
    assert burrow(workspace, "report", report["id"]) == document, "run close lost report"

    # Base public module exposes the same saved reader, without another module.
    hv("chain", "create", "report-check", chain="report-check")
    hv("chain", "add", "burrow@0.1.0", chain="report-check")
    hv("target", "add", "local", chain="report-check")
    hv("chain", "config", "set", "workspace", str(workspace), chain="report-check")
    hv("chain", "config", "set", "command", "report " + report["id"], chain="report-check")
    response = json.loads(hv("throw", "--now", "--allow-dangerous", "--json", chain="report-check"))["results"][0]
    assert response["state"] == "succeeded" and json.loads(response["summary"]) == document, response

    path = workspace / report["path"]
    path.write_bytes(bytes([original[0] ^ 1]) + original[1:])
    try:
        assert "SHA256" in burrow(workspace, "report", report["id"], ok=False)
    finally:
        path.write_bytes(original)
    path.chmod(0o644)
    try:
        assert "private regular file" in burrow(workspace, "report", report["id"], ok=False)
    finally:
        path.chmod(0o600)
    moved = path.with_name(path.name + ".hidden")
    path.rename(moved)
    try:
        burrow(workspace, "report", report["id"], ok=False)
        path.symlink_to(moved)
        burrow(workspace, "report", report["id"], ok=False)
    finally:
        if path.is_symlink():
            path.unlink()
        moved.rename(path)
    short = run("survey", connection["name"], "--os", "ubuntu", "--budget", "64", "--yes")
    assert not short["outputComplete"] and short["collection"] == "succeeded", short
    partial, = [e for e in burrow(workspace, "reports") if e["sourceRun"] == short["id"]]
    assert not partial["complete"] and "budget" in partial["detail"] and partial["size"] == 64, partial
    assert len(burrow(workspace, "report", partial["id"])["markdown"].encode()) == 64
    run("close", short["id"], "--yes")
    print("PASS reviewed Ubuntu survey, five artifacts, base-module read, partial capture and unchanged verified evidence", flush=True)

    # The same embedded command set on the actual local Ubuntu host. This is a
    # command-compatibility proof, separate from the remote SSH acceptance above.
    release = Path("/etc/os-release").read_text()
    if '\nID=ubuntu\n' in "\n" + release:
        proof = subprocess.run(["/bin/sh", survey_script], capture_output=True, text=True, timeout=90)
        assert proof.returncode in (0, 1) and "## Coverage" in proof.stdout, proof
        assert "ID=ubuntu" in proof.stdout and "Current user and groups" in proof.stdout
        assert "Status: OBSERVED" in proof.stdout and "uid=" in proof.stdout
        if directory := os.environ.get("TEST_UNDECLARED_OUTPUTS_DIR"):
            Path(directory, "ubuntu-survey.md").write_text(proof.stdout)
        print("PASS actual Ubuntu command-set compatibility (failed/unavailable probes retain their status)", flush=True)
    else:
        print("SKIP local Ubuntu proof: host is not Ubuntu; SSH lab proof passed", flush=True)
    for plain in (False, True):
        report_ui(binary, env, decoder, workspace, burrow, report, connection["name"], plain)
    assert path.read_bytes() == original and burrow(workspace, "report", report["id"]) == document


def report_ui(binary, env, decoder, workspace, burrow, report, connection, plain):
    outer, slave = pty.openpty()
    before = termios.tcgetattr(slave)
    dimensions = [160, 40]
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
    proc = subprocess.Popen([binary, "--workspace", str(workspace), "tui"], env=viewer_env,
                            stdin=slave, stdout=slave, stderr=slave, preexec_fn=controlling)
    output = bytearray()

    def wait(needle):
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if select.select([outer], [], [], .05)[0]:
                output.extend(os.read(outer, 65536))
            screen = subprocess.run([decoder, *map(str, dimensions)], input=output, capture_output=True, check=True).stdout.decode()
            if needle in screen:
                return screen
            assert proc.poll() is None, screen
        raise AssertionError((needle, screen))

    try:
        wait(connection)
        os.write(outer, ("report " + report["id"] + "\r").encode())
        wait("Ubuntu host survey")
        for width, height in ((160, 40), (200, 50), (120, 30), (80, 24)):
            while select.select([outer], [], [], .05)[0]:
                output.extend(os.read(outer, 65536))
            output.clear()
            dimensions[:] = [width, height]
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
            os.kill(proc.pid, signal.SIGWINCH)
            wait("Reports")
            os.write(outer, b"\x1b[F")
            capture = wait("vulnerability findings")
            if directory := os.environ.get("TEST_UNDECLARED_OUTPUTS_DIR"):
                Path(directory, f"reports-pty-{width}x{height}-{plain}.txt").write_text(capture)
            os.write(outer, b"\x1b[H")
            wait("Ubuntu host survey")
        os.write(outer, b"\x1b")
        wait("INCOMPLETE CAPTURE")
        os.write(outer, b"\x1b")
        wait("COMMAND OUTPUT")
        # Existing output and notes editors still open and switch independently.
        os.write(outer, b"unchanged-draft\x0c")
        wait("Collected output")
        os.write(outer, b"\t")
        wait("Operator notes")
        os.write(outer, b"\x0c")
        wait("unchanged-draft")
        os.write(outer, b"\x15")
        before_runs = {r["id"] for r in burrow(workspace, "run", "list")}
        os.write(outer, ("run survey " + connection[:-1] + "\t").encode())
        wait("run survey " + connection + " --os ubuntu")
        os.write(outer, b"\r")
        wait("Proceed?")
        pending, = [r for r in burrow(workspace, "run", "list") if r["id"] not in before_runs]
        assert pending["state"] == "prepared" and pending["input"]["survey"] == "ubuntu"
        os.write(outer, b"\t\r")
        wait('"launchRunID"')
        completed = burrow(workspace, "run", "inspect", pending["id"])
        assert completed["state"] == "exited" and completed["outputComplete"], completed
        saved, = [e for e in burrow(workspace, "reports") if e["sourceRun"] == pending["id"]]
        assert saved["complete"], saved
        assert {r["id"] for r in burrow(workspace, "run", "list")} == before_runs | {pending["id"]}
        burrow(workspace, "run", "close", pending["id"], "--yes")
        os.write(outer, b"\x03")
        wait("Keep running")
        os.write(outer, b"\r")
        deadline = time.monotonic() + 10
        while proc.poll() is None and time.monotonic() < deadline:
            if select.select([outer], [], [], .05)[0]:
                output.extend(os.read(outer, 65536))
        assert proc.poll() == 0 and termios.tcgetattr(slave) == before
        print(f"PASS Reports PTY scrolling/reflow/return, existing Ctrl+L editors and survey approval (NO_COLOR={plain})", flush=True)
    finally:
        if proc.poll() is None:
            os.killpg(proc.pid, signal.SIGKILL)
            proc.wait(timeout=5)
        os.close(outer)
        os.close(slave)
