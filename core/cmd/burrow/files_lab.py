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
import resource
import struct
import subprocess
import termios


def file_ui(binary, env, decoder, workspace, name="gateway", slow=None, transfers=False):
    def cli(*args):
        p = subprocess.run([binary, "--workspace", str(workspace), *args], env=env, capture_output=True, check=True)
        return json.loads(p.stdout)
    if not slow and not transfers:
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
        if transfers:
            target=Path(workspace)/"burrow-files/downloads/ui-destination/large.bin"
            target.parent.mkdir()
            target.write_bytes(b"original survives cancellation")
            os.write(outer,b"get /tmp/burrow-download-check/large.bin ui-destination/\r")
            wait("Download these files?")
            wait("OVERWRITE")
            os.write(outer,b"\t\r")  # Review defaults to Cancel; explicitly choose Download.
            wait("Overall")
            wait("RUNNING")
            records=cli("downloads")["records"]
            transfer=next(d for d in reversed(records) if d["files"][0]["destination"]==str(target))
            os.write(outer,b"\x1b")
            wait("Overall",present=False)
            wait("LISTING /config")
            os.write(outer,b"downloads\r")
            wait("Overall")
            os.write(outer,b"\x1b")
            wait("Overall",present=False)
            os.write(outer,b"\x1bOP")
            wait("FILE BROWSING")
            os.write(outer,b"\x1b")
            wait("FILE BROWSING",present=False)
            os.write(outer,b"ls /tmp/burrow-download-check\r")
            wait("large.bin")
            os.write(outer,b"\x1bb")
            wait("SAVED CONNECTIONS")
            os.write(outer,("shell "+name+"\r").encode())
            wait("local / not recorded")
            before_transfer=cli("downloads",transfer["id"])
            os.write(outer,b"printf '%s%s\\n' DOWNLOAD_ SHELL_LIVE\r")
            wait("DOWNLOAD_SHELL_LIVE")
            deadline=time.monotonic()+5
            while True:
                after=cli("downloads",transfer["id"])
                if after["bytes"]>before_transfer["bytes"]:
                    break
                assert time.monotonic()<deadline, after
                time.sleep(.1)
            cancelled=cli("download-cancel",transfer["id"])
            assert cancelled["state"]=="cancelled",cancelled
            assert target.read_bytes()==b"original survives cancellation"
            partial=Path(cancelled["files"][0]["partial"])
            assert partial.is_file() and 0<partial.stat().st_size<64*1024*1024,cancelled
            size=partial.stat().st_size
            time.sleep(.2)
            assert partial.stat().st_size==size,"cancel acknowledgement preceded cleanup"
            os.write(outer,b"exit\r")
            wait("SAVED CONNECTIONS")
            os.write(outer,b"quit\r")
            wait("Keep running")
            os.write(outer,b"\r")
            assert frontend.wait(timeout=10)==0
            assert termios.tcgetattr(slave)==before
            print("PASS real copy continues through help/listing/shell; acknowledged cancel preserves original and labelled partial",flush=True)
            return
        if slow:
            slow()
            # Entry listing starts the owner's one-second discovery throttle.
            # This case tests a delayed handshake, not the separately tested throttle.
            time.sleep(1.1)
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
        screen = wait((name, "tester", "127.0.0.1"))
        row = next(i for i, line in enumerate(screen.splitlines()) if all(part in line for part in (name, "tester", "127.0.0.1")))
        master = cli("inspect", name)["masterPID"]
        os.write(outer, f"\x1b[<2;32;{row+1}M".encode())
        wait("Enter Shell")
        os.write(outer, b"\x1b[B\r")
        wait("Shell #1")
        wait("local / not recorded")
        os.write(outer, b"printf '%s%s\\n' BURROW_ MENU_SHELL\r")
        wait("BURROW_MENU_SHELL")
        assert cli("inspect", name)["masterPID"] == master
        os.write(outer, b"exit\r")
        wait("SAVED CONNECTIONS")
        print("PASS right-click active connection opens real same-master shell tab", flush=True)
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
    if [ -e /tmp/burrow-file-throttle ]; then
      /usr/lib/ssh/sftp-server -e -l DEBUG3 2>> /tmp/burrow-file-requests |
        (stats=$(mktemp); while :; do
          dd bs=32768 count=1 2>"$stats" || break
          grep -q '^0+0 records in' "$stats" && break
          sleep 0.02
        done; rm "$stats")
      exit
    fi
    exec /usr/lib/ssh/sftp-server -e -l DEBUG3 2>> /tmp/burrow-file-requests
    ;;
  *)
    printf 'exec\\n' >> /tmp/burrow-file-sessions
    [ ! -e /tmp/burrow-file-noexec ] || exit 126
    [ -n "$SSH_ORIGINAL_COMMAND" ] || exec /bin/sh
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
            command("docker","exec",container,"truncate","-s","64M","/tmp/burrow-download-check/large.bin")
            command("docker","exec",container,"chown","tester","/tmp/burrow-download-check/large.bin")
            command("docker","exec",container,"touch","/tmp/burrow-file-throttle")
            file_ui(binary,env,decoder,workspace,"file-observer",transfers=True)
            download_failures(burrow,workspace,container,command,options)
            command("docker","exec",container,"rm","/tmp/burrow-file-throttle")
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
    download_checks(burrow, workspace, state, container, command)


def download_checks(burrow, workspace, state, container, command):
    """Exercise the public reviewed command seam and observe actual file bytes."""
    base = "/tmp/burrow-download-check"
    command("docker", "exec", container, "mkdir", "-p", base)
    command("docker", "exec", container, "sh", "-c",
            "printf 'download fixture\\n' > /tmp/burrow-download-check/one.txt; "
            ": > /tmp/burrow-download-check/empty.txt; "
            "chown -R tester /tmp/burrow-download-check")
    root = Path(workspace) / "burrow-files/downloads"
    review = burrow(workspace, "scp", "gateway", "get", base + "/one.txt")
    assert review["files"][0]["destination"] == str(root / "one.txt"), review
    assert review["files"][0]["size"] == 17, review
    assert not (root / "one.txt").exists(), "review wrote a destination"
    started = burrow(workspace, "scp", "gateway", "get", base + "/one.txt",
                     "--review", review["digest"], "--yes")
    deadline = time.monotonic() + 20
    while True:
        result = burrow(workspace, "downloads", started["id"])
        if result["state"] != "running":
            break
        assert time.monotonic() < deadline, result
        time.sleep(.1)
    assert result["state"] == "complete", result
    assert (root / "one.txt").read_bytes() == b"download fixture\n"
    assert result["files"][0]["bytes"] == 17, result
    assert burrow(workspace, "inspect", "gateway")["masterPID"] == state["masterPID"]
    print("PASS reviewed same-master download, exact bytes and retrievable completion", flush=True)
    def finish(started):
        deadline = time.monotonic() + 20
        while True:
            result = burrow(workspace, "downloads", started["id"])
            if result["state"] != "running":
                return result
            assert time.monotonic() < deadline, result
            time.sleep(.1)
    def approved(*args):
        plan = burrow(workspace, "scp", "gateway", *args)
        return finish(burrow(workspace, "scp", "gateway", *args, "--review", plan["digest"], "--yes"))
    batch = approved("mget", base + "/*.txt", "batch")
    assert batch["state"] == "complete" and len(batch["files"]) == 2, batch
    assert (root / "batch/one.txt").read_bytes() == b"download fixture\n"
    assert (root / "batch/empty.txt").read_bytes() == b""
    before = burrow(workspace, "downloads")
    assert before["files"] == 3 and before["bytes"] == 34, before
    assert burrow(workspace, "downloads")["files"] == 3, "observation duplicated totals"
    old = root / "replace.txt"
    old.write_bytes(b"original")
    plan = burrow(workspace, "scp", "gateway", "get", base + "/one.txt", "replace.txt")
    assert plan["files"][0]["existing"], plan
    failure = burrow(workspace, "scp", "gateway", "get", base + "/one.txt", "replace.txt", "--yes", ok=False)
    assert "review" in failure and old.read_bytes() == b"original", failure
    old.write_bytes(b"changed since review")
    burrow(workspace, "scp", "gateway", "get", base + "/one.txt", "replace.txt", "--review", plan["digest"], "--yes", ok=False)
    assert old.read_bytes() == b"changed since review"
    outside = Path(workspace) / "outside"
    outside.mkdir()
    (root / "escape").symlink_to(outside, target_is_directory=True)
    for destination in ("../outside/a", str(outside / "a"), "escape/a"):
        burrow(workspace, "scp", "gateway", "get", base + "/one.txt", destination, ok=False)
    assert not list(outside.iterdir())
    no_match = burrow(workspace,"scp","gateway","mget",base+"/*.absent")
    assert no_match["files"] == [], no_match
    unusual = base + "/α 'quoted file'.dat"
    command("docker", "exec", container, "cp", base + "/one.txt", unusual)
    command("docker", "exec", container, "ln", "-s", unusual, base + "/link.dat")
    for source in (unusual, base + "/link.dat"):
        copied = approved("get", source)
        assert copied["state"] == "complete", copied
        assert Path(copied["files"][0]["destination"]).read_bytes() == b"download fixture\n"
    args = ("get", base + "/one.txt", "changed-source.txt")
    plan = burrow(workspace, "scp", "gateway", *args)
    command("docker", "exec", container, "truncate", "-s", "18", base + "/one.txt")
    burrow(workspace, "scp", "gateway", *args, "--review", plan["digest"], "--yes", ok=False)
    assert not (root / "changed-source.txt").exists()
    args = ("mget", base + "/*.txt", "changed-discovery")
    plan = burrow(workspace, "scp", "gateway", *args)
    command("docker", "exec", container, "touch", base + "/new.txt")
    burrow(workspace, "scp", "gateway", *args, "--review", plan["digest"], "--yes", ok=False)
    assert not (root / "changed-discovery").exists()
    args = ("get", base + "/one.txt", "changed-root.txt")
    plan = burrow(workspace, "scp", "gateway", *args)
    alternate = Path(workspace) / "alternate-downloads"
    burrow(workspace, "local", "download", str(alternate))
    burrow(workspace, "scp", "gateway", *args, "--review", plan["digest"], "--yes", ok=False)
    assert not list(alternate.iterdir())
    burrow(workspace, "local", "download", str(root))
    print("PASS sequential batch/empty files, no-write review, overwrite binding, containment and deduplicated totals", flush=True)
    print("PASS actual Unicode/quoted/symlink copies and changed source/discovery/root refusal", flush=True)


def download_failures(burrow, workspace, container, command, options):
    root=Path(workspace)/"burrow-files/downloads"
    remote="/tmp/burrow-download-check/large.bin"
    def start(name,source,destination,op="get"):
        p=burrow(workspace,"scp",name,op,source,destination)
        return burrow(workspace,"scp",name,op,source,destination,"--review",p["digest"],"--yes")
    def observe(d,predicate,timeout=40):
        deadline=time.monotonic()+timeout
        while time.monotonic()<deadline:
            value=burrow(workspace,"downloads",d["id"])
            if predicate(value):return value
            time.sleep(.1)
        raise AssertionError(value)
    target=root/"stalled.bin"
    target.write_bytes(b"keep original")
    d=start("file-observer",remote,"stalled.bin")
    observe(d,lambda v:v["bytes"]>0)
    command("docker","exec",container,"sh","-c","pkill -STOP -f '^/usr/lib/ssh/sftp-server'")
    try:
        time.sleep(1.3)
        burrow(workspace,"downloads",d["id"])
        time.sleep(1.3)
        stalled=burrow(workspace,"downloads",d["id"])
        assert stalled["state"]=="running" and stalled["rate"]==0 and stalled["eta"] is None,stalled
        cancelled=burrow(workspace,"download-cancel",d["id"])
        assert cancelled["state"]=="cancelled" and target.read_bytes()==b"keep original",cancelled
    finally:
        command("docker","exec",container,"sh","-c","pkill -CONT -f '^/usr/lib/ssh/sftp-server' || true")
    owner=burrow(workspace,"inspect","file-observer")["ownerPID"]
    limit=resource.prlimit(owner,resource.RLIMIT_FSIZE)
    target=root/"write-failure.bin"
    target.write_bytes(b"preserve on write failure")
    try:
        resource.prlimit(owner,resource.RLIMIT_FSIZE,(4096,limit[1]))
        failed=observe(start("file-observer",remote,"write-failure.bin"),lambda v:v["state"]!="running")
        assert failed["state"]=="failed" and 0<failed["bytes"]<=4096,failed
        assert target.read_bytes()==b"preserve on write failure"
    finally:
        resource.prlimit(owner,resource.RLIMIT_FSIZE,limit)
    growing="/tmp/burrow-download-check/growing.bin"
    command("docker","exec",container,"truncate","-s","4M",growing)
    command("docker","exec",container,"chown","tester",growing)
    target=root/"growing.bin";target.write_bytes(b"original")
    d=start("file-observer",growing,target.name)
    observe(d,lambda v:v["bytes"]>0)
    command("docker","exec",container,"truncate","-s","64M",growing)
    failed=observe(d,lambda v:v["state"]!="running")
    assert failed["state"]=="failed" and failed["bytes"]<=4*1024*1024,failed
    assert target.read_bytes()==b"original"
    base="/tmp/burrow-download-mixed"
    command("docker","exec",container,"sh","-c",
            "mkdir /tmp/burrow-download-mixed; truncate -s 4M /tmp/burrow-download-mixed/a.bin; "
            "printf denied > /tmp/burrow-download-mixed/b.bin; printf final > /tmp/burrow-download-mixed/c.bin; "
            "chown -R tester /tmp/burrow-download-mixed")
    mixed=start("file-observer",base+"/*.bin","mixed","mget")
    command("docker","exec",container,"chmod","000",base+"/b.bin")
    mixed=observe(mixed,lambda v:v["state"]!="running")
    assert mixed["state"]=="partial" and [f["state"] for f in mixed["files"]]==["complete","failed","complete"],mixed
    assert (root/"mixed/a.bin").read_bytes()==bytes(4*1024*1024)
    assert (root/"mixed/c.bin").read_bytes()==b"final"
    untouched=(root/"mixed/a.bin").stat().st_mtime_ns
    command("docker","exec",container,"chmod","600",base+"/b.bin")
    retry=observe(start("file-observer",base+"/b.bin","mixed/b.bin"),lambda v:v["state"]!="running")
    assert retry["state"]=="complete" and (root/"mixed/b.bin").read_bytes()==b"denied",retry
    assert (root/"mixed/a.bin").stat().st_mtime_ns==untouched
    for name,loss in (("download-close",False),("download-loss",True)):
        burrow(workspace,"connect",name,"127.0.0.1","tester",*options)
        deadline=time.monotonic()+10
        while True:
            state=burrow(workspace,"inspect",name)
            if state["state"]=="connected":break
            assert time.monotonic()<deadline,state
            time.sleep(.1)
        target=root/(name+".bin");target.write_bytes(b"original")
        transfer=start(name,remote,target.name)
        observe(transfer,lambda v:v["bytes"]>0)
        if loss:os.kill(state["masterPID"],signal.SIGKILL)
        else:burrow(workspace,"close",name,"--yes")
        ended=observe(transfer,lambda v:v["state"]!="running")
        assert ended["state"] in ("failed","cancelled") and target.read_bytes()==b"original",ended
        assert Path(ended["files"][0]["partial"]).is_file()
        if loss:burrow(workspace,"close",name,"--yes")
        assert burrow(workspace,"inspect","gateway")["state"]=="connected"
    print("PASS stalled rate/unknown ETA, real write failure, mixed batch, selected restart retry, transfer close/loss and sibling retention",flush=True)
