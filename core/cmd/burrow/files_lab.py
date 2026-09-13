"""Real SFTP browsing through the retained production owner."""
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
import tempfile
import time
import fcntl
import json
import os
import pty
import select
import signal
import struct
import subprocess
import termios


def file_ui(binary, env, decoder, workspace, name="gateway", slow=None):
    def cli(*args):
        p = subprocess.run([binary, "--workspace", str(workspace), *args], env=env, capture_output=True, check=True)
        return json.loads(p.stdout)
    if not slow:
        cli("profile", "create", "edit-fixture", "example.test", "tester", "--port", "22")
    outer, slave = pty.openpty()
    before = termios.tcgetattr(slave)
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))
    def controlling():
        os.setsid()
        fcntl.ioctl(0, termios.TIOCSCTTY, 0)
    frontend = subprocess.Popen([binary, "--workspace", str(workspace), "tui"], env=env,
                                stdin=slave, stdout=slave, stderr=slave, preexec_fn=controlling)
    output = bytearray()
    def wait(needle, present=True):
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if select.select([outer], [], [], .05)[0]:
                output.extend(os.read(outer, 65536))
            screen = subprocess.run([decoder, "160", "40"], input=output, capture_output=True, check=True).stdout.decode()
            found = any(all(part in line for part in needle) for line in screen.splitlines()) if isinstance(needle, tuple) else needle in screen
            if found==present:
                return screen
            assert frontend.poll() is None, screen
        raise AssertionError((needle, screen))
    try:
        wait(name)
        os.write(outer, ("scp " + name + "\r").encode())
        wait("FILE MODE")
        wait("LISTING /config")
        if slow:
            slow()
            os.write(outer, b"cd /tmp/uncached-directory/x")
            wait("Discovering paths")
            started=time.monotonic()
            os.write(outer,b"\x1bOP")  # F1: help while SFTP handshake is delayed
            wait("FILE BROWSING")
            assert time.monotonic()-started<1.5, "slow discovery blocked input"
            os.write(outer,b"\x1b")
            wait("FILE BROWSING",present=False)
            os.write(outer,b"\x03")
            wait("SAVED CONNECTIONS")
            os.write(outer,b"quit\r")
            wait("Keep running")
            os.write(outer,b"\r")
            assert frontend.wait(timeout=10)==0
            assert termios.tcgetattr(slave)==before
            return
        os.write(outer, b"ls /tmp/burrow-file-check\r")
        screen = wait("old.txt")
        assert "PERMISSIONS" in screen and "OWNER" in screen and "MODIFIED" in screen, screen
        os.write(outer, b"cd /tmp/burrow-file-check/su")
        wait("COMPLETION")
        wait("sub dir/'")
        os.write(outer, b"\t\r")
        wait(("Remote:", "/tmp/burrow-file-check/sub dir"))
        os.write(outer, b"history\r")
        wait("SCP HISTORY")
        local=Path(workspace)/"burrow-files/uploads/local dir"
        local.mkdir(exist_ok=True)
        (local/"local.txt").write_text("local fixture")
        os.write(outer,b"lcd upload 'local dir'\r")
        wait("Local upload directory:")
        os.write(outer,b"lls upload\r")
        wait("local.txt")
        os.write(outer, b"back\r")
        wait("SAVED CONNECTIONS")
        screen = wait("edit-fixture")
        row = next(i for i, line in enumerate(screen.splitlines()) if "edit-fixture" in line)
        # SGR right-click the saved row, choose Huh's second action, and edit in
        # the real local Vim through the same PTY host used for SSH shell tabs.
        os.write(outer, f"\x1b[<2;32;{row+1}M".encode())
        wait("Edit in Vim")
        os.write(outer, b"\x1b[B\r")
        wait("Vim · edit-fixture")
        wait('"port"')
        os.write(outer, b':%s/"port": 22/"port": 2222/\r:wq\r')
        wait("Saved connection updated")
        assert cli("profile", "select", "edit-fixture")["port"] == 2222
        assert cli("inspect", name)["state"] == "connected"
        print("PASS right-click saved connection, real Vim tab, validated save and retained SSH master", flush=True)
        os.write(outer, b"quit\r")
        wait("Keep running")
        os.write(outer, b"\r")
        assert frontend.wait(timeout=10) == 0
        assert termios.tcgetattr(slave) == before
    finally:
        if frontend.poll() is None:
            frontend.kill()
            frontend.wait()
        os.close(outer)
        os.close(slave)


def load_checks(burrow, workspace, container, command, options, binary, env, decoder):
    """Observe server sessions and SFTP requests, not frontend timing guesses."""
    config_path = "/config/sshd/sshd_config"
    original = command("docker", "exec", container, "cat", config_path)
    with tempfile.TemporaryDirectory(prefix="burrow-file-observer-") as scratch:
        root = Path(scratch)
        wrapper = root / "observe"
        wrapper.write_text('''#!/bin/sh
case "$SSH_ORIGINAL_COMMAND" in
  *sftp*)
    printf 'sftp\\n' >> /tmp/burrow-file-sessions
    [ ! -e /tmp/burrow-file-nosftp ] || exit 126
    if [ -e /tmp/burrow-file-delay ]; then sleep 4; fi
    exec /usr/lib/ssh/sftp-server -e -l DEBUG3 2>> /tmp/burrow-file-requests
    ;;
  *)
    printf 'exec\\n' >> /tmp/burrow-file-sessions
    [ ! -e /tmp/burrow-file-noexec ] || exit 126
    exec /bin/sh -c "$SSH_ORIGINAL_COMMAND"
    ;;
esac
''')
        wrapper.chmod(0o755)
        command("docker", "exec", container, "test", "-x", "/usr/lib/ssh/sftp-server")
        config = root / "sshd_config"
        config.write_text(original + "\nForceCommand /tmp/burrow-file-observe\n")
        command("docker", "cp", wrapper, container + ":/tmp/burrow-file-observe")
        command("docker", "cp", config, container + ":" + config_path)
        reload = "kill -HUP $(cat /config/sshd.pid)"
        command("docker", "exec", container, "sh", "-c", reload)
        try:
            burrow(workspace, "connect", "file-observer", "127.0.0.1", "tester", *options)
            deadline = time.monotonic() + 15
            while burrow(workspace, "inspect", "file-observer")["state"] != "connected":
                assert time.monotonic() < deadline
                time.sleep(.1)
            def browse(operation, path):
                return burrow(workspace, "scp", "file-observer", operation, path)
            def counts():
                sessions = command("docker", "exec", container, "cat", "/tmp/burrow-file-sessions").splitlines()
                requests = command("docker", "exec", container, "cat", "/tmp/burrow-file-requests")
                return sessions.count("sftp"), sessions.count("exec"), requests.count("opendir ")
            base = "/tmp/burrow-file-check"
            first = browse("ls", base)
            before = counts()
            assert before[0] == 1 and before[1] == 1 and before[2] > 0, before
            with ThreadPoolExecutor(max_workers=8) as workers:
                values = list(workers.map(lambda _: browse("complete", base), range(16)))
            assert all(value.get("entries") or value.get("notice") for value in values)
            assert counts() == before, (before, counts())
            browse("ls", base)
            after = counts()
            assert after[0] == before[0] + 1 and after[1] == before[1], (before, after)
            assert after[2] > before[2], (before, after)
            # Invalidated directory aliases cannot bypass per-connection throttling.
            with ThreadPoolExecutor(max_workers=8) as workers:
                list(workers.map(lambda n: browse("complete", base + "/" + "./" * n), range(1, 17)))
            burst = counts()
            assert burst[0] - after[0] <= 2, (after, burst)
            time.sleep(1.2)
            assert counts() == burst, "idle discovery issued remote work"
            assert first["entries"]
            command("docker","exec",container,"touch","/tmp/burrow-file-noexec")
            command("docker","exec",container,"chown","12345:23456",base+"/old.txt")
            reduced=browse("ls",base)
            entry=next(e for e in reduced["entries"] if e["name"]=="old.txt")
            assert entry["owner"]=="12345" and entry["group"]=="23456" and "Reduced metadata" in reduced["notice"], reduced
            command("docker","exec",container,"chown","tester:root",base+"/old.txt")
            file_ui(binary,env,decoder,workspace,"file-observer",lambda:command("docker","exec",container,"touch","/tmp/burrow-file-delay"))
            # A cancelled handshake must not proceed to directory enumeration.
            cancelled=counts()
            time.sleep(4.5)
            assert counts()==cancelled, (cancelled,counts())
            command("docker","exec",container,"touch","/tmp/burrow-file-nosftp")
            burrow(workspace,"scp","file-observer","ls",base,ok=False)
            assert burrow(workspace,"inspect","file-observer")["state"]=="connected"
            print("PASS measured remote-load budget: cached completion has zero directory reads; explicit refresh reuses account names; idle stays silent", flush=True)
        finally:
            burrow(workspace, "close", "file-observer", "--yes")
            config.write_text(original)
            command("docker", "cp", config, container + ":" + config_path)
            command("docker", "exec", container, "sh", "-c", reload)


def file_checks(burrow, workspace, state, container, command):
    base = "/tmp/burrow-file-check"
    command("docker", "exec", container, "mkdir", "-p", base + "/sub dir", base + "/denied")
    for name in ("old.txt", "α space.txt", ".hidden", "line\nname", "$(false)"):
        command("docker", "exec", container, "touch", base + "/" + name)
    command("docker", "exec", container, "touch", "-t", "200001010000", base + "/old.txt")
    command("docker", "exec", container, "ln", "-s", "sub dir", base + "/dir-link")
    command("docker", "exec", container, "ln", "-s", "absent", base + "/broken")
    command("docker", "exec", container, "chown", "-R", "tester", base)
    command("docker", "exec", container, "chmod", "000", base + "/denied")
    command("docker", "exec", container, "touch", base + "/hostile\x1b]52;c;payload\a")
    current = burrow(workspace, "scp", "gateway")
    assert current["path"].startswith("/"), current
    listing = burrow(workspace, "scp", "gateway", "ls", base)
    names = [entry["name"] for entry in listing["entries"]]
    assert names[0] == "old.txt" and {"α space.txt", ".hidden", "line\nname", "$(false)", "broken"} <= set(names), listing
    assert "." not in names and ".." not in names
    link = next(entry for entry in listing["entries"] if entry["name"] == "dir-link")
    assert link["link"] == "sub dir" and link["directory"], link
    assert all(entry["permissions"] and entry["owner"] and entry["group"] for entry in listing["entries"])
    assert burrow(workspace,"scp","gateway","cd",base+"/dir-link")["path"] == base+"/sub dir"
    failure = burrow(workspace,"scp","gateway","ls",base+"/denied",ok=False)
    assert "permission" in failure.lower(), failure
    tree = burrow(workspace,"scp","gateway","tree",base)
    assert tree["incomplete"] and tree["errors"], tree
    assert any(entry["name"] == "dir-link" for entry in tree["entries"])
    assert not any(entry["path"].startswith(base+"/dir-link/") for entry in tree["entries"])
    command("docker", "exec", container, "sh", "-c",
            "mkdir /tmp/burrow-large-tree; i=0; while [ $i -lt 3001 ]; do "
            ": > /tmp/burrow-large-tree/file-$i; i=$((i+1)); done")
    bounded = burrow(workspace,"scp","gateway","tree","/tmp/burrow-large-tree")
    assert bounded["incomplete"] and 0 < len(bounded["entries"]) <= 3000, bounded
    assert any("3000" in error for error in bounded["errors"]), bounded["errors"]
    assert len(json.dumps(bounded).encode()) < 1 << 20
    assert burrow(workspace,"inspect","gateway")["masterPID"] == state["masterPID"]
    print("PASS real same-master SFTP metadata, navigation, denied paths and symlink tree",flush=True)
