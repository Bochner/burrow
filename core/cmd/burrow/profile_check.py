"""Saved collection behavior through the production command/Hovel seam."""
import json
import http.client
import os
from pathlib import Path
import signal
import socket
import concurrent.futures
import subprocess
import sys
import tempfile

binary, wheel = [str(Path(p).resolve()) for p in sys.argv[1:]]
with tempfile.TemporaryDirectory(prefix="bp-") as scratch:
    root = Path(scratch)
    w = root / "w"
    env = {k: v for k, v in os.environ.items() if not k.startswith(("HOVEL_", "SSH_"))}
    env.update(HOME=scratch, XDG_CACHE_HOME=str(root / "cache"), XDG_CONFIG_HOME=str(root / "config"))
    def run(*args, ok=True):
        p = subprocess.run([binary, "--workspace", str(w), *args], env=env, capture_output=True, text=True, timeout=40)
        assert (p.returncode == 0) == ok, (args, p.stdout, p.stderr)
        return json.loads(p.stdout) if ok else p.stderr
    info = run("--hovel-package", wheel, "status")
    try:
        assert run("profiles")["profiles"] == []
        template = w / "burrow-profiles.json"
        initial = json.loads(template.read_text())
        assert initial["_comment"] and initial["_example"]["name"] == "example"
        assert initial["profiles"] == [] and template.stat().st_mode & 0o777 == 0o600
        run("profile", "create", "gateway", "127.0.0.1", "tester", "--port", "1")
        saved = run("profiles")
        assert saved["profiles"][0]["name"] == "gateway", saved
        assert run("connections") == [], "saving authenticated"
        assert run("profile", "select", "gateway")["host"] == "127.0.0.1"
        assert run("connections") == [], "selecting authenticated"
        backup = root / "backup.json"
        run("profile", "backup", str(backup))
        assert json.loads(backup.read_text())["_example"] == initial["_example"]
        alternate = root / "other.json"
        run("profile", "collection", str(alternate))
        assert run("profiles")["profiles"] == []
        run("profile", "create", "router", "192.0.2.1", "admin")
        assert run("profiles")["path"] == str(alternate)
        run("--load", str(backup), "profiles")
        assert [p["name"] for p in run("profiles")["profiles"]] == ["gateway"]
        run("profile", "edit", "gateway", "192.0.2.20", "operator", "--yes")
        assert run("profile", "select", "gateway")["host"] == "192.0.2.20"
        assert json.loads(template.read_text())["profiles"][0]["host"] == "127.0.0.1"
        assert json.loads(alternate.read_text())["profiles"][0]["name"] == "router"
        assert "review" in run("profile", "delete", "gateway")
        run("profile", "delete", "gateway", "--yes")
        assert run("profiles")["profiles"] == []
        assert "profile delete gateway --yes" in run("history")
        # Load must not resolve SSH config (Match exec) or touch the network.
        marker = root / "config-was-evaluated"
        config = root / "ssh-config"
        config.write_text(f"Match exec \"touch {marker}\"\n User nobody\n")
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0)); listener.listen(); listener.settimeout(.2)
            run("profile", "create", "passive", "127.0.0.1", "-", "--port", str(listener.getsockname()[1]), "--ssh-config", str(config))
            run("profile", "load", str(backup))
            run("profile", "select", "passive")
            assert not marker.exists(), "load evaluated executable SSH configuration"
            try:
                listener.accept()
                raise AssertionError("load attempted a network connection")
            except TimeoutError:
                pass
        current = backup.read_bytes()
        bad = root / "bad.json"
        for data in [b"{", b'{"version":99,"profiles":[]}',
                     b'{"version":1,"profiles":[{"name":"old","host":"host","user":"user","knownHosts":"/tmp/known_hosts"}]}',
                     b'{"version":1,"profiles":[{"name":"bad","host":"host","user":"user","password":"SECRET-CANARY"}]}']:
            bad.write_bytes(data); bad.chmod(0o600)
            error=run("profile", "load", str(bad), ok=False)
            assert "SECRET-CANARY" not in error
            assert run("profiles")["path"] == str(backup) and backup.read_bytes()==current
        run("profile", "backup", str(backup), ok=False)
        assert backup.read_bytes()==current
        run("profile", "create", "bad", "host", "user", "--password", "SECRET-CANARY", ok=False)
        assert "SECRET-CANARY" not in str(run("history"))
        # Two independent commands must preserve unrelated additions.
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            futures=[pool.submit(run,"profile","create",n,"host","user") for n in ["one","two"]]
            for f in futures:
                try: f.result()
                except AssertionError as e:
                    assert "changed" in str(e), e  # Explicit stale refusal is acceptable; silent loss is not.
        assert run("profile","select","passive")["host"]=="127.0.0.1"
        revision=run("profiles")["revision"]
        run("profile","create","newer","host","user")
        assert "changed" in run("profile","delete","passive","--yes","--revision",revision,ok=False)
        assert run("profile","select","passive")
        readonly = root / "readonly"
        readonly.mkdir(mode=0o700)
        locked=readonly / "profiles.json"
        locked.write_bytes(backup.read_bytes());locked.chmod(0o600)
        run("profile","load",str(locked))
        before=locked.read_bytes();readonly.chmod(0o500)
        try:
            run("profile","create","denied","host","user",ok=False)
            assert locked.read_bytes()==before
        finally: readonly.chmod(0o700)
        run("profile","load",str(backup))
        link=root / "link.json";link.symlink_to(backup)
        run("profile","load",str(link),ok=False)
        assert run("profiles")["path"]==str(backup)
        history=run("history")
        run("--offline","status")
        assert run("profiles")["path"]==str(backup)
        assert run("history")==history
        # Registered public SDK module reaches the same command/file, with no SSH.
        hovel=root / "cache/burrow/hovel/0.4.2/hovel"
        def hv(*args):
            p=subprocess.run([str(hovel),"run","--workspace",str(w),"--op","profile-check","--chain","profile-check","--",*args],env=env,capture_output=True,text=True,timeout=30)
            assert p.returncode==0,(p.stdout,p.stderr)
            return p.stdout
        hv("op","create","profile-check");hv("chain","create","profile-check")
        hv("chain","add","burrow@0.1.0");hv("target","add","local")
        hv("chain","config","set","workspace",str(w))
        hv("chain","config","set","command","profile create sdk-profile host user")
        result=json.loads(hv("throw","--now","--allow-dangerous","--json"))
        assert result["results"][0]["state"]=="succeeded",result
        assert run("profile","select","sdk-profile")["host"]=="host"
        assert not marker.exists()
        assert run("connections")==[]

        # Unrelated retained output must not make a tiny saved collection unreadable.
        with socket.socket(socket.AF_UNIX) as sock:
            sock.connect(str(w / "hoveld.sock"))
            client = http.client.HTTPConnection("localhost")
            client.sock = sock
            body = {"Operation":"profile-check", "Chain":"profile-check", "Entries":[
                {"Kind":"event","Level":"info","Source":"profile-regression","Message":"x"*65536}
                for _ in range(17)]}
            client.request("POST", "/hovel.daemon.v1.DaemonService/AppendLog", json.dumps(body), {"Content-Type":"application/json"})
            response = client.getresponse()
            assert response.status == 200, response.status
            response.read()
        assert run("profiles")["path"] == str(backup)

    finally:
        os.kill(info["pid"], signal.SIGTERM)
print("PASS production saved collection commands")
