"""Drive the real embedded Hovel CLI; all mutations use disposable workspaces."""
import base64
import fcntl
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import time

signal.alarm(180)
binary, wheel, package, decoder = [str(Path(p).resolve()) for p in sys.argv[1:]]
with tempfile.TemporaryDirectory(prefix="bt-") as scratch:
    root = Path(scratch)
    env = {k: v for k, v in os.environ.items() if not k.startswith("HOVEL_")}
    env.update(HOME=scratch, XDG_CACHE_HOME=str(root / "cache"), XDG_CONFIG_HOME=str(root / "config"), NO_COLOR="1", TERM="xterm-256color", DISPLAY="", WAYLAND_DISPLAY="")
    daemons = []
    terminal = None
    master, slave = pty.openpty()
    before = termios.tcgetattr(slave)
    dimensions = [160, 40]
    output = bytearray()

    def status(workspace):
        result = subprocess.run([binary, "--workspace", str(workspace), "--hovel-package", wheel, "status"], env=env, capture_output=True, text=True, timeout=30, check=True)
        info = json.loads(result.stdout)
        daemons.append(info["pid"])
        return info

    def cli(workspace, *args):
        executable = root / "cache/burrow/hovel/0.4.2/hovel"
        result = subprocess.run([str(executable), "run", "--workspace", str(workspace), "--daemon-endpoint", str(workspace / "hoveld.sock"), "--", *args], env=env, capture_output=True, text=True, timeout=20)
        assert result.returncode == 0, result.stdout + result.stderr
        return result.stdout

    def screen():
        return subprocess.run([decoder, *map(str, dimensions)], input=output, capture_output=True, timeout=3, check=True).stdout.decode()

    def wait(needle, prompt=False):
        deadline = time.monotonic() + 12
        stable, since = None, time.monotonic()
        while time.monotonic() < deadline:
            if select.select([master], [], [], .05)[0]:
                output.extend(os.read(master, 65536))
            view = screen()
            center = [line[28:126].rstrip() for line in view.splitlines()[3:38]]
            if center != stable:
                stable, since = center, time.monotonic()
            center = [line for line in center if "h0v3l" in line]
            ready = center and re.search(r"h0v3l.*>\s*$", center[-1])
            if needle in view and (not prompt or ready) and time.monotonic() - since > .25:
                return view
            assert terminal.poll() is None, (terminal.returncode, view)
        Path(os.environ["TEST_UNDECLARED_OUTPUTS_DIR"], "terminal-debug").write_bytes(output)
        raise AssertionError((needle, view))

    def send(data):
        os.write(master, data if isinstance(data, bytes) else data.encode())

    def command(text, needle, prompt=True):
        send(text)
        # Match human entry: let go-prompt display the draft before Enter.
        # Its parser treats a combined text+CR read as pasted text.
        if text not in ("yes", "no"):
            wait(text)
        else:
            time.sleep(.1)
        send(b"\r")
        return wait(needle, prompt)

    def click(x, y):
        send(f"\x1b[<0;{x+1};{y+1}M\x1b[<0;{x+1};{y+1}m")

    def menu(text):
        send(b"\x1d\x10")  # terminal escape, then Burrow palette
        wait("Type to filter")
        send(text)
        wait(text)
        send(b"\r")

    def controlling():
        os.setsid()
        fcntl.ioctl(0, termios.TIOCSCTTY, 0)

    def cli_children():
        children = set()
        for task in Path(f"/proc/{terminal.pid}/task").iterdir():
            try:
                children.update(map(int, (task / "children").read_text().split()))
            except FileNotFoundError:
                pass
        return children

    try:
        a, b = root / "a", root / "b"
        info_a, info_b = status(a), status(b)
        executable = root / "cache/burrow/hovel/0.4.2/hovel"
        absent = root / "no-start"
        refused = subprocess.run([str(executable), "shell", "--workspace", str(absent)], env=env | {"HOVEL_DAEMON_ENDPOINT": str(absent / "hoveld.sock")}, capture_output=True, timeout=10)
        assert refused.returncode != 0 and not absent.exists(), "explicit endpoint fell through to startup"
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))
        # Foreign endpoint/config environment must not redirect the embedded CLI.
        hostile = env | {"HOVEL_DAEMON_ENDPOINT": str(b / "hoveld.sock"), "HOVEL_CONFIG": str(root / "absent")}
        terminal = subprocess.Popen([binary, "--workspace", str(a), "--offline"], env=hostile, stdin=slave, stdout=slave, stderr=slave, preexec_fn=controlling)
        wait("SAVED CONNECTION")
        assert re.search(rb"\x1b\[\?100[0236]h", output), "panel selection needs mouse events"
        assert b"\x1b]52;" not in output, "unexpected clipboard operation"
        send("management-draft")
        click(39, 1)
        wait("h0v3l>")
        view = wait("modules: 1")
        expected = "\n".join(line[28:126].rstrip() for line in view.splitlines()[3:6])
        send(b"\x1b[<0;29;4M\x1b[<32;160;6M\x1b[<0;160;6m")
        wait("[Copy]")
        assert b"\x1b]52;" not in output, "drag release copied automatically"
        send(b"\x03")
        wait("Copy sent to terminal")
        payloads = re.findall(rb"\x1b\]52;c;([A-Za-z0-9+/=]*)", output)
        assert payloads and base64.b64decode(payloads[-1]).decode() == expected, payloads
        assert terminal.poll() is None, "Ctrl+C selection copy quit the TUI"
        send(b"\x1b")
        wait("modules: 1")
        # A harmless read-only module still travels through Hovel's plan/confirm contract.
        command("op create embedded-a", "Operation selected: embedded-a")
        command("chain create check", "steps:0")
        command("chain add burrow@0.1.0", "burrow@0.1.0")
        command("target add local://workspace", "local://workspace")
        command(f"chain config set workspace {a}", str(a))
        assert json.loads(cli(a, "throw", "list", "--json")) is None
        command("throw --allow-dangerous", "yes", False)
        # Leave A's actual confirmation pending while operating B's independent CLI.
        click(2, 20)
        wait("Exact destination")
        send(str(b) + "\r")
        wait("● b")
        click(39, 1)
        wait("h0v3l>")
        command("op create embedded-b", "Operation selected: embedded-b")
        click(2, 3)
        wait("THROW REVIEW")
        command("no", "cancelled")
        plans = json.loads(cli(a, "throw", "list", "--json"))
        assert len(plans) == 1
        plan_id = plans[0]["id"]
        rejected = cli(a, "throw", "inspect", plan_id, "--events", "--json")
        assert "hovel.throw.started" not in rejected and "hovel.throw.confirmed" not in rejected, rejected
        assert "embedded-a" in cli(a, "op", "list")
        assert "embedded-a" not in cli(b, "op", "list")
        command("review", "yes", False)
        command("yes", "confirmed")
        command("throw --allow-dangerous", "Verified Burrow workspace")
        # Scroll the real CLI back to its historical startup banner, then resume.
        send(b"\x1b[1;2H")  # Shift+Home
        wait("modules: 1")
        wait("History")
        send(b"\x1b[1;2F")  # Shift+End
        wait("h0v3l", True)
        send(b"\x1b[<64;45;15M")  # wheel up inside the CLI pane
        wait("History")
        send(b"\x1b[1;2F")
        wait("h0v3l", True)
        completed = cli(a, "throw", "inspect", plan_id, "--events", "--json")
        assert "hovel.throw.started" in completed and "hovel.throw.completed" in completed, completed
        click(2, 4)
        wait("op:embedded-b")
        assert "embedded-b" in cli(b, "op", "list")
        assert "embedded-b" not in cli(a, "op", "list")
        click(2, 3)
        wait("embedded-a/check")
        send(b"\x1bb")  # Alt+B leaves the live CLI without forwarding the key.
        wait("management-draft")
        send(b"\x1bh")  # Alt+H restores this workspace's existing CLI.
        wait("embedded-a/check")
        send(b"\x1bs")  # restore native selection while Hovel retains focus
        wait("Ctrl+Shift+C/V")
        send(b"\x1b[200~op create pasted\n\x1b[201~")
        time.sleep(.3)
        assert "pasted" not in cli(a, "op", "list"), "paste executed a command"
        send(b"\x03")
        time.sleep(.2)
        assert terminal.poll() is None, "Ctrl-C quit outer UI"
        send(b"\x1bs")  # re-enable pointer controls for the remaining click scenarios
        for width, height in [(120, 30), (80, 24), (200, 50), (160, 40)]:
            while select.select([master], [], [], .05)[0]:
                output.extend(os.read(master, 65536))
            output.clear()  # Bubble Tea redraws on resize; old cursor geometry is not replayed at the new size.
            dimensions[:] = [width, height]
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
            os.kill(terminal.pid, signal.SIGWINCH)
            capture = wait("Hovel")
            Path(os.environ["TEST_UNDECLARED_OUTPUTS_DIR"], f"hovel-{width}x{height}.txt").write_text(capture)
        send(b"\x15")
        wait("h0v3l", True)
        command("quit", "CLI: exited", False)
        click(39, 1)
        wait("CLI: exited")
        wait("Daemon: connected")
        wait("[Restart CLI]")
        click(29, 37)
        wait("h0v3l", True)
        assert status(a)["pid"] == info_a["pid"]
        send(b"\x04")  # Hovel's actual EOF path, not a Burrow quit binding.
        wait("CLI: exited")
        send(b"\x12")  # Ctrl+R explicitly restarts only the exited CLI.
        wait("h0v3l", True)
        # Daemon loss must not start a replacement or silently retry a tab.
        os.kill(info_a["pid"], signal.SIGTERM)
        wait("UNVERIFIED")
        menu("Close Hovel CLI")
        wait("CLI closed")
        click(39, 1)
        wait("REFUSED")
        click(39, 1)
        wait("REFUSED")
        children = cli_children()
        assert children, "B's background CLI was lost"
        send(b"\x1d\x03")
        wait("Keep working")
        send(b"\t\r")
        assert terminal.wait(timeout=10) == 0
        assert all(not Path(f"/proc/{pid}").exists() for pid in children), "frontend did not reap its CLI processes"
        assert termios.tcgetattr(slave) == before
        assert status(b)["pid"] == info_b["pid"]
        print("PASS embedded pinned CLI: confirmation, rejection, workspace isolation, chrome, paste, resize, exit, daemon loss and retained daemon")
    finally:
        if terminal and terminal.poll() is None:
            terminal.kill()
            terminal.wait()
        os.close(master)
        os.close(slave)
        for pid in set(daemons):
            try:
                os.kill(pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
