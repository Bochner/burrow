"""Independent CLI and TUI clients observe one real SSH owner and retirement."""
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import signal
import struct
import subprocess
import termios
import time


def workspace_checks(binary, env, decoder, root, sibling_workspace, burrow, wait, options, daemons):
    w = root / "headless"
    info = burrow(w, "--offline", "workspace", "open")
    daemons.append(info["pid"])
    before_owner = burrow(w, "workspace", "retire")
    outer, slave = pty.openpty()
    before = termios.tcgetattr(slave)
    dimensions = [160, 40]
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))

    def controlling():
        os.setsid()
        fcntl.ioctl(0, termios.TIOCSCTTY, 0)

    frontend = subprocess.Popen([binary, "--workspace", str(w), "--offline", "tui"],
                                env=env | {"TERM": "xterm-256color", "NO_COLOR": "1"},
                                stdin=slave, stdout=slave, stderr=slave, preexec_fn=controlling)
    output = bytearray()

    def screen(needle, absent=b""):
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if select.select([outer], [], [], .1)[0]:
                output.extend(os.read(outer, 65536))
            text = subprocess.run([decoder, *map(str, dimensions)], input=output,
                                  capture_output=True, check=True, timeout=3).stdout
            if needle in text and (not absent or absent not in text):
                return text
            assert frontend.poll() is None, text
        raise AssertionError((needle, absent, text))

    def connected(workspace, name):
        return wait(lambda: (s if (s := burrow(workspace, "inspect", name))["state"] == "connected" else None))

    def failure(action, digest):
        return json.loads(burrow(w, "workspace", action, "--yes", "--review", digest, ok=False))["error"]

    try:
        screen(b"No connections")
        burrow(w, "profile", "create", "saved", "127.0.0.1", "tester", *options[:-1])
        settings = burrow(w, "profiles")
        review = burrow(w, "profile", "connect", "saved", "--as", "cli-live")
        assert burrow(w, "connections") == [], "review authenticated"
        burrow(w, "profile", "connect", "saved", "--as", "cli-live", "--yes", "--review", review["digest"])
        first = connected(w, "cli-live")
        screen(b"cli-live")
        assert failure("retire", before_owner["digest"])["code"] == "review_changed"

        burrow(w, "connect", "sibling", "127.0.0.1", "tester", *options)
        sibling = connected(w, "sibling")
        burrow(sibling_workspace, "connect", "outside", "127.0.0.1", "tester", *options)
        outside = connected(sibling_workspace, "outside")
        close = burrow(w, "close", "cli-live")
        assert close["digest"]
        rejected = burrow(w, "close", "cli-live", "--yes", "--review", "0" * 64, ok=False)
        assert json.loads(rejected)["error"]["operation"] == "connection.close"
        assert burrow(w, "inspect", "cli-live")["creation"] == first["creation"]
        burrow(w, "close", "cli-live", "--yes", "--review", close["digest"])
        screen(b"sibling", absent=b"cli-live")
        assert burrow(w, "inspect", "sibling")["masterPID"] == sibling["masterPID"]
        burrow(w, "profile", "connect", "saved", "--as", "cli-live", "--yes")
        replacement = connected(w, "cli-live")
        assert replacement["creation"] != first["creation"]
        burrow(w, "close", "cli-live", "--yes", "--review", close["digest"], ok=False)
        assert burrow(w, "inspect", "cli-live")["creation"] == replacement["creation"]

        # Collected output is real Hovel evidence, independent of the manager.
        evidence = burrow(w, "run", "now", "cli-live", "--yes", "--", "printf", "headless-evidence")
        assert evidence["collection"] == "succeeded", evidence
        captured = burrow(w, "run", "output", evidence["id"], "stdout", "0")
        retained = burrow(w, "run", "inspect", evidence["id"])

        def artifacts():
            hovel = root / "cache/burrow/hovel/0.4.2/hovel"
            p = subprocess.run([str(hovel), "run", "--workspace", str(w), "--daemon-endpoint", str(w / "hoveld.sock"),
                                "--", "artifact", "list", "--json"], env=env, capture_output=True, text=True, timeout=20)
            assert p.returncode == 0, p.stderr
            return [a for a in json.loads(p.stdout) if a["runId"] == evidence["runID"]]

        collected = artifacts()
        assert collected, "no collected Hovel artifacts"
        saved_bytes = {a["path"]: (w / a["path"]).read_bytes() for a in collected}
        retire = burrow(w, "workspace", "retire")
        restart = burrow(w, "workspace", "restart")
        assert retire["owner"]["session"] == first["session"]
        assert {s["name"] for s in retire["connections"]} == {"cli-live", "sibling"}
        assert failure("restart", retire["digest"])["code"] == "review_changed"
        assert failure("retire", "0" * 64)["code"] == "review_changed"
        assert burrow(w, "workspace", "retire", "--yes=false", "--review", retire["digest"])["state"] == "review"
        assert burrow(w, "inspect", "cli-live")["creation"] == replacement["creation"]
        # The accepted whole-manager review explicitly covers concurrent additions.
        burrow(w, "connect", "added", "127.0.0.1", "tester", *options)
        added = connected(w, "added")
        result = burrow(w, "workspace", "restart", "--yes", "--review", restart["digest"])
        assert result["state"] == "ready" and result["owner"] == restart["owner"]
        assert {s["creation"] for s in result["connections"]} == {replacement["creation"], sibling["creation"], added["creation"]}
        screen(b"No connections", absent=b"cli-live")
        assert frontend.poll() is None
        assert burrow(w, "workspace", "inspect")["pid"] == info["pid"]
        assert burrow(w, "profiles") == settings
        assert burrow(w, "run", "inspect", evidence["id"]) == retained
        assert artifacts() == collected
        assert all((w / path).read_bytes() == data for path, data in saved_bytes.items())
        assert burrow(w, "run", "output", evidence["id"], "stdout", "0") == captured
        assert burrow(sibling_workspace, "inspect", "outside")["masterPID"] == outside["masterPID"]
        for state in (replacement, sibling, added):
            assert not Path(state["socket"]).exists()

        # A fresh approved connect creates a new owner; old confirmations cannot retire it.
        burrow(w, "profile", "connect", "saved", "--as", "after", "--yes")
        after = connected(w, "after")
        assert after["generation"] != first["generation"]
        assert failure("restart", restart["digest"])["code"] == "review_changed"
        screen(b"after")
        os.kill(after["masterPID"], signal.SIGKILL)
        wait(lambda: burrow(w, "inspect", "after")["state"] == "lost")
        screen(b"DISCONNECTED", absent=b"No connections")
        review = burrow(w, "workspace", "retire")
        assert burrow(w, "workspace", "retire", "--yes", "--review", review["digest"])["state"] == "retired"
        screen(b"No connections")

        # Failed observation is visibly unknown, never an empty inventory.
        receipt = w / "burrow-launch.json"
        original = receipt.read_text()
        changed = json.loads(original)
        changed["workspace"] = str(sibling_workspace)
        receipt.write_text(json.dumps(changed))
        try:
            screen(b"UNVERIFIED", absent=b"No connections")
            assert failure("retire", review["digest"])["code"] == "unverified"
        finally:
            receipt.write_text(original)
        screen(b"No connections")
        for width, height in ((120, 30), (80, 24), (200, 50), (160, 40)):
            while select.select([outer], [], [], 0)[0]:
                os.read(outer, 65536)
            output.clear()
            dimensions[:] = [width, height]
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
            os.kill(frontend.pid, signal.SIGWINCH)
            screen(b"No connections")
        os.write(outer, b"\x03")
        screen(b"Keep working")
        os.write(outer, b"\t\r")
        until = time.monotonic() + 10
        while frontend.poll() is None and time.monotonic() < until:
            if select.select([outer], [], [], .1)[0]:
                os.read(outer, 65536)
        assert frontend.wait(timeout=5) == 0
        assert termios.tcgetattr(slave) == before
        burrow(sibling_workspace, "close", "outside", "--yes")
        print("PASS headless lifecycle: shared CLI/TUI owner, stale reviews, siblings, evidence, retirement and loss", flush=True)
    finally:
        if frontend.poll() is None:
            frontend.kill()
            frontend.wait()
        os.close(outer)
        os.close(slave)
