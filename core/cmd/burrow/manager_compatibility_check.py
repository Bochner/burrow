"""Upgrade refusal through the production CLI and public retained-session API."""
import http.client
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import tempfile

binary, wheel, fixture, manifest = [str(Path(p).resolve()) for p in sys.argv[1:5]]
with tempfile.TemporaryDirectory(prefix="bm-") as scratch:
    root = Path(scratch)
    w = root / "w"
    env = {k: v for k, v in os.environ.items() if not k.startswith(("HOVEL_", "SSH_"))}
    env.update(HOME=scratch, XDG_CACHE_HOME=str(root / "cache"), XDG_CONFIG_HOME=str(root / "config"))

    def run(*args):
        return subprocess.run([binary, "--workspace", str(w), *args], env=env,
                              capture_output=True, text=True, timeout=30, start_new_session=True)

    info = json.loads(run("--hovel-package", wheel, "status").stdout)
    hovel = root / "cache/burrow/hovel/0.4.2/hovel"

    def hv(*args):
        p = subprocess.run([str(hovel), "run", "--workspace", str(w), "--op", "old", "--chain", "old", "--", *args],
                           env=env, capture_output=True, text=True, timeout=30)
        assert p.returncode == 0, p.stderr
        return p.stdout

    def rpc(method, data):
        c = http.client.HTTPConnection("localhost", timeout=10)
        c.sock = socket.socket(socket.AF_UNIX)
        c.sock.connect(str(w / "hoveld.sock"))
        c.request("POST", "/hovel.daemon.v1.DaemonService/" + method, json.dumps(data), {"Content-Type": "application/json"})
        r = c.getresponse()
        raw = r.read()
        c.close()
        assert r.status == 200, raw
        return json.loads(raw)

    try:
        package = root / "old"
        package.mkdir()
        (package / "burrow").symlink_to(fixture)
        (package / "hovel-module.yaml").write_text(Path(manifest).read_text().replace('"module"', '"old-manager"'))
        hv("module", "install", "--link", str(package), "--replace", "--no-scripts")
        hv("op", "create", "old")
        hv("chain", "create", "old")
        hv("chain", "add", "burrow@0.1.0")
        hv("target", "add", "local")
        hv("chain", "config", "set", "workspace", str(w))
        record = json.loads(hv("throw", "--now", "--allow-dangerous", "--json"))["results"][0]
        assert record["state"] == "succeeded", record
        owner = json.loads(record["summary"])
        assert run("--offline", "status").returncode == 0
        # Older managers remain usable for compatible key-only chain exports.
        args = ["connect", "target", "127.0.0.1", "--user", "tester", "--password"]
        chain = run("chain", "connect", "key-control", "127.0.0.1", "--user", "tester", "--jump", "tester@127.0.0.1:2222")
        assert chain.returncode == 0, chain.stderr
        preview = json.loads(json.loads(chain.stdout)["spec"]["config"]["request"])["preview"]
        assert " ProxyCommand /usr/bin/ssh -F " in preview, "compatible key jump changed its reviewed SSH command"
        # Bare --password needs /dev/tty. Use a private PTY
        # to reach export without ever answering a password or launching SSH.
        import fcntl
        import pty
        import termios
        master, slave = pty.openpty()
        def controlling():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)
        p = subprocess.Popen([binary, "--workspace", str(w), "chain", *args], env=env,
                             stdin=slave, stdout=subprocess.PIPE, stderr=subprocess.PIPE, preexec_fn=controlling)
        try:
            try:
                out, err = p.communicate(timeout=5)
            except subprocess.TimeoutExpired:
                p.terminate()
                out, err = p.communicate(timeout=5)
            assert b"retained manager does not support password authentication" in err, (out, err)
            assert not out, "incompatible chain was exported"
        finally:
            if p.poll() is None: p.kill(); p.wait()
            os.close(master); os.close(slave)
        for command in [["chain", *args, "INLINE-COMPATIBILITY-CANARY"], [*args, "INLINE-COMPATIBILITY-CANARY"], [*args, "INLINE-COMPATIBILITY-CANARY", "--yes"]]:
            refused = run(*command)
            assert refused.returncode != 0 and "retained manager does not support password authentication" in refused.stderr, refused.stderr
            assert not refused.stdout and "INLINE-COMPATIBILITY-CANARY" not in refused.stderr
        result = rpc("RunSessionCommand", {"SessionID": owner["session"], "Request": {"Command": "attempts"}})
        assert json.loads(result.get("stdout", result.get("Stdout"))) == 0
        assert json.loads(run("connections").stdout) == []
        rpc("CloseSession", {"SessionID": owner["session"]})
        print("PASS old manager password refusal before export/dispatch; compatible key export and explicit close remain available")
    finally:
        os.kill(info["pid"], signal.SIGTERM)
