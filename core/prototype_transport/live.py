"""Authorized live-server checks for the accepted OpenSSH/subsystem-pipe path."""
from pathlib import Path
import fcntl
import hashlib
import os
import pty
import re
import select
import shlex
import socket
import struct
import subprocess
import sys
import tempfile
import termios
import time

probe, host, user, *collect = sys.argv[1:]
if not re.fullmatch(r"[A-Za-z0-9.-]+", host) or not re.fullmatch(r"[A-Za-z0-9_-]+", user):
    raise SystemExit("Expected a host/IP and username, without SSH options")
if any(not path.startswith("/") or any(ord(c) < 32 for c in path) for path in collect):
    raise SystemExit("Collection paths must be absolute and contain no control characters")

with tempfile.TemporaryDirectory(prefix="burrow-live-") as local:
    control = str(Path(local) / "master")
    config = Path(local) / "ssh_config"
    config.write_text(f"Host target\n HostName {host}\n User {user}\n StrictHostKeyChecking yes\n ConnectTimeout 5\n")
    os.chmod(config, 0o600)
    base = ["/usr/bin/ssh", "-F", str(config), "-S", control]
    reused = base + ["-o", "ProxyCommand=/bin/false"]
    shells = []
    remote = None

    def run(args, **kwargs):
        return subprocess.run(args, check=True, timeout=15, **kwargs)

    def command(text):
        return subprocess.check_output(reused + ["target", text], timeout=15).decode().strip()

    def resize(fd, cols, rows):
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))

    def read_until(fd, marker):
        data = bytearray()
        deadline = time.monotonic() + 8
        while time.monotonic() < deadline:
            if marker in data:
                return data
            if select.select([fd], [], [], .1)[0]:
                data.extend(os.read(fd, 65536))
        raise AssertionError(f"remote shell did not produce {marker!r}")

    try:
        # OpenSSH reads the password from /dev/tty. Never save it or pass it as an argument.
        subprocess.run(base + ["-M", "-f", "-N", "-o", "ControlPersist=no", "target"], check=True, timeout=60)
        print("PASS existing host-key trust and authenticated named OpenSSH master", flush=True)
        remote = command("mktemp -d /tmp/burrow-design.XXXXXX")
        if not re.fullmatch(r"/tmp/burrow-design\.[A-Za-z0-9]+", remote):
            raise RuntimeError("unexpected remote scratch directory")
        run([probe, "pipe-sftp", control, remote, str(config)])

        # Optional read-only collection of owner-selected existing files. Names are
        # passed as argv to OpenSSH SFTP-mode scp; shell metadata paths are quoted.
        sizes = [int(command("stat -Lc %s -- " + shlex.quote(path))) for path in collect]
        batch_started = time.monotonic()
        collected = 0
        for index, (path, size) in enumerate(zip(collect, sizes)):
            destination = Path(local) / f"collection-{index}"
            started = time.monotonic()
            process = subprocess.Popen(["/usr/bin/scp", "-q", "-F", str(config),
                "-o", "ControlPath=" + control, "-o", "ProxyCommand=/bin/false",
                "-o", "BatchMode=yes", "target:" + path, str(destination)])
            try:
                while process.poll() is None:
                    elapsed = time.monotonic() - started
                    if elapsed > 300:
                        raise TimeoutError("collection exceeded five minutes")
                    copied = destination.stat().st_size if destination.exists() else 0
                    speed = copied / max(.001, elapsed)
                    eta = f"{(size - copied) / speed:.1f}s" if speed else "unknown"
                    print(f"COLLECT {index+1}/{len(collect)} · {Path(path).name} · "
                          f"{copied}/{size} bytes · {speed/1048576:.1f} MiB/s · "
                          f"elapsed {elapsed:.1f}s · ETA {eta} · "
                          f"overall {collected+copied}/{sum(sizes)} bytes", flush=True)
                    time.sleep(1)
                if process.returncode:
                    raise RuntimeError(f"collection failed with exit {process.returncode}")
            finally:
                if process.poll() is None:
                    process.terminate()
                    process.wait(timeout=5)
            assert destination.stat().st_size == size
            with destination.open("rb") as stream:
                actual_hash = hashlib.file_digest(stream, "sha256").hexdigest()
            expected_hash = command("sha256sum -- " + shlex.quote(path)).split()[0]
            assert actual_hash == expected_hash, "collected bytes differ from remote file"
            collected += size
            print(f"PASS collected {Path(path).name}: {size} bytes, SHA-256 verified", flush=True)
        if collect:
            print(f"PASS batch collection: {len(collect)} files, {collected} bytes, "
                  f"{time.monotonic()-batch_started:.1f}s including verification; originals unchanged", flush=True)

        for _ in range(2):
            master, slave = pty.openpty()
            resize(slave, 80, 24)
            def controlling():
                os.setsid()
                fcntl.ioctl(0, termios.TIOCSCTTY, 0)
            process = subprocess.Popen(reused + ["-tt", "target", "exec /bin/sh -i"],
                stdin=slave, stdout=slave, stderr=slave, preexec_fn=controlling)
            os.close(slave)
            shells.append((process, master))
            os.write(master, b"stty -echo; stty size; printf 'SIZE_%s\\n' END\n")
            assert b"24 80" in read_until(master, b"SIZE_END")
        first, second = shells[0][1], shells[1][1]
        os.write(first, b"sleep 0.2; printf 'BACKGROUND_%s\\n' DONE\n")
        os.write(second, b"printf 'SECOND_%s\\n' ACTIVE\n")
        read_until(second, b"SECOND_ACTIVE")
        read_until(first, b"BACKGROUND_DONE")
        resize(first, 120, 40)
        os.write(first, b"stty size; printf 'RESIZE_%s\\n' END\n")
        assert b"40 120" in read_until(first, b"RESIZE_END")
        print("PASS two live SSH shells, background execution and resize 80x24 → 120x40", flush=True)

        # Forward to the lab's existing SSH listener; no remote service installation.
        with socket.socket() as reservation:
            reservation.bind(("127.0.0.1", 0))
            port = reservation.getsockname()[1]
        mapping = f"127.0.0.1:{port}:127.0.0.1:22"
        run(base + ["-O", "forward", "-L", mapping, "target"])
        with socket.create_connection(("127.0.0.1", port), timeout=5) as forwarded:
            assert forwarded.recv(256).startswith(b"SSH-")
        run(base + ["-O", "cancel", "-L", mapping, "target"])
        with socket.socket() as removed:
            removed.settimeout(5)
            assert removed.connect_ex(("127.0.0.1", port)) != 0
        reverse = command("printf live")  # master is still usable after removing the forward.
        assert reverse == "live"
        allocated = subprocess.check_output(base + ["-O", "forward", "-R", "127.0.0.1:0:127.0.0.1:1", "target"], timeout=5).decode().strip()
        assert allocated.isdigit() and 0 < int(allocated) < 65536
        # Cancel by the original request, including port 0, not its allocated port.
        run(base + ["-O", "cancel", "-R", "127.0.0.1:0:127.0.0.1:1", "target"])
        check_removed = f"import socket; s=socket.socket(); s.settimeout(5); assert s.connect_ex(('127.0.0.1',{allocated})) != 0"
        command("python3 -c " + shlex.quote(check_removed))
        print("PASS local tunnel data exchange and remote listener allocation/removal", flush=True)
    finally:
        for process, fd in shells:
            os.close(fd)
            if process.poll() is None:
                process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
        if Path(control).exists():
            try:
                if remote and re.fullmatch(r"/tmp/burrow-design\.[A-Za-z0-9]+", remote):
                    command("rm -f -- " + shlex.quote(remote + "/roundtrip") + " " + shlex.quote(remote + "/partial") + "; rmdir -- " + shlex.quote(remote))
            finally:
                run(base + ["-O", "exit", "target"])
    assert not Path(control).exists()
    print("PASS test shells, tunnels, remote scratch files and master cleaned up", flush=True)
