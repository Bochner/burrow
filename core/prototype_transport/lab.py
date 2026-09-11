"""Optional authorized lab: existing host trust, interactive password, no remote files."""
from pathlib import Path
import shlex
import subprocess
import sys
import tempfile
import time

probe = str(Path(sys.argv[1]).resolve())
host, user = sys.argv[2:]
assert host and user and not host.startswith("-") and not user.startswith("-")
with tempfile.TemporaryDirectory(prefix="burrow-lab-") as scratch:
    control = str(Path(scratch) / "lab")
    base = ["/usr/bin/ssh", "-F", "/dev/null", "-S", control, "-o", "StrictHostKeyChecking=yes",
            "-o", "ConnectTimeout=5", "-o", "ControlPersist=no"]
    target = user + "@" + host
    fixture = None
    log = Path(scratch) / "master.log"
    base += ["-E", str(log)]
    try:
        subprocess.run(base + ["-M", "-f", "-N", "-o", "PreferredAuthentications=password",
                              "-o", "PubkeyAuthentication=no", target], check=True, timeout=60)
        print("Authenticated through existing host trust; temporary master socket:", control, flush=True)
        external = subprocess.check_output(base + ["-o", "ProxyCommand=/bin/false", target,
                                                   "printf '%s' \"$SSH_CONNECTION\""], timeout=10)
        bridged = subprocess.check_output([probe, "exec", control], timeout=10)
        assert external == bridged and external
        print("PASS lab external OpenSSH and Go channel share one SSH connection", flush=True)
        subprocess.run([probe, "pty", control], check=True, timeout=25)
        info = subprocess.run(base + ["-o", "ProxyCommand=/bin/false", target, "uname -sr; ssh -V"],
                              capture_output=True, text=True, check=True, timeout=10)
        print("VM:", info.stdout.strip(), info.stderr.strip(), flush=True)
        # Hold a VM port bound but NOT listening: a real, deterministic refusal.
        # No files, services or account settings are changed on the VM.
        code = "import socket,sys; s=socket.socket(); s.bind(('127.0.0.1',0)); print(s.getsockname()[1],flush=True); sys.stdin.read()"
        fixture = subprocess.Popen(base + ["-o", "ProxyCommand=/bin/false", target,
                                           "python3 -u -c " + shlex.quote(code)],
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        port = int(fixture.stdout.readline())
        refused = f"127.0.0.1:{port}"
        subprocess.run(base + ["-O", "check", target], check=True, timeout=5)
        ordinary = subprocess.run(base + ["-o", "ProxyCommand=/bin/false", "-W", refused, target],
                                  capture_output=True, text=True, timeout=10)
        assert ordinary.returncode != 0
        subprocess.run(base + ["-O", "check", target], check=True, timeout=5)
        print("PASS VM refusal through normal OpenSSH returns failure and retains master", flush=True)
        remote = subprocess.run([probe, "remote", control], capture_output=True, text=True, timeout=12)
        assert remote.returncode == 2 and "probe timed out" in remote.stderr, remote
        subprocess.run(base + ["-O", "check", target], check=True, timeout=5)
        print("REPRODUCED against VM: Go remote-listener request timed out", flush=True)
        # Normal control command allocates/removes a VM listener on the same master.
        remote_port = subprocess.check_output(base + ["-O", "forward", "-R", "127.0.0.1:0:127.0.0.1:1", target], timeout=5).decode().strip()
        assert int(remote_port) > 0
        subprocess.run(base + ["-O", "cancel", "-R", f"127.0.0.1:{remote_port}:127.0.0.1:1", target], check=True, timeout=5)
        print("PASS VM remote-listener allocation/removal through normal OpenSSH controls", flush=True)
        result = subprocess.run([probe, "refused", control, refused], capture_output=True, text=True, timeout=12)
        assert result.returncode == 0 and "visible" in result.stdout, result
        deadline = time.monotonic() + 5
        while Path(control).exists() and time.monotonic() < deadline:
            time.sleep(.05)
        assert not Path(control).exists(), log.read_text()
        assert "no remote_id" in log.read_text()
        print("REPRODUCED against VM: closing Go bridge after clean refusal terminates its OpenSSH master (no remote_id)", flush=True)
    finally:
        if Path(control).exists():
            subprocess.run(base + ["-O", "exit", target], timeout=5, check=True)
        if fixture is not None:
            fixture.stdin.close()
            try:
                fixture.wait(timeout=5)
            except subprocess.TimeoutExpired:
                fixture.kill()
                fixture.wait(timeout=5)
    assert not Path(control).exists()
    print("PASS lab master closed; no remote files created", flush=True)
