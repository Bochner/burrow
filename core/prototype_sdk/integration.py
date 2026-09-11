"""Disposable Linux integration proof against an Aspect-built, pinned Hovel binary."""
import json
import os
from pathlib import Path
import signal
import sqlite3
import subprocess
import sys
import tarfile
import tempfile
import time

archive, hovel = map(lambda p: str(Path(p).resolve()), sys.argv[1:])


def wait_for(check):
    for _ in range(200):
        if check():
            return
        time.sleep(0.05)
    raise AssertionError("timed out waiting for lifecycle transition")


for linked in (True, False):
    with tempfile.TemporaryDirectory(prefix="burrow-sdk-proof-") as scratch:
        root = Path(scratch)
        workspace = root / "workspace"
        package = root / "package"
        package.mkdir()
        with tarfile.open(archive) as tar:
            tar.extractall(package, filter="data")
        env = os.environ | {"XDG_CONFIG_HOME": str(root / "config"), "XDG_DATA_HOME": str(root / "data")}
        daemon = None
        pids = []

        def cli(*args, operator=False, input=None):
            prefix = [hovel, "run", "--workspace", str(workspace)]
            if operator:
                prefix += ["--op", "proof", "--chain", "proof"]
            result = subprocess.run(prefix + ["--", *args], cwd=root, env=env,
                                    input=input, text=True, capture_output=True, timeout=20)
            assert result.returncode == 0, (args, result.stdout, result.stderr)
            return result.stdout

        try:
            with (root / "daemon.log").open("w") as log:
                daemon = subprocess.Popen([hovel, "daemon", "serve", "--workspace", str(workspace)],
                                          cwd=root, env=env, stdout=log, stderr=log)
            wait_for(lambda: (workspace / "hoveld.sock").exists() or daemon.poll() is not None)
            assert daemon.poll() is None, (root / "daemon.log").read_text()
            print(cli("module", "check", "--warnings-as-errors", str(package)), flush=True)
            args = ["--link", str(package)] if linked else [archive]
            print(cli("module", "install", *args), flush=True)
            assert "burrow-sdk-prototype" in cli("module", "installed")
            cli("op", "create", "proof")
            cli("chain", "create", "proof", operator=True)
            cli("chain", "add", "burrow-sdk-prototype@0.0.0", operator=True)
            cli("target", "add", "mock://inert", operator=True)
            payload = json.loads(cli("throw", "--now", "--json", operator=True))
            result = payload["results"][0]
            assert result["state"] == "succeeded", result
            session = result["sessions"][0]["id"]
            pid = int(result["summary"].split("pid=")[1])
            pids.append(pid)
            assert Path(f"/proc/{pid}").exists()
            assert session in cli("session", "list", operator=True)
            # Each connect invocation attaches then detaches on input EOF.
            for marker in ("first", "reattached"):
                attachment = subprocess.run([hovel, "session", "connect", session, "--workspace", str(workspace)],
                                            cwd=root, env=env, input="", text=True, capture_output=True, timeout=20, start_new_session=True)
                assert attachment.returncode == 0, (attachment.stdout, attachment.stderr)
                output = attachment.stdout
                assert "Connected to session" in output and "Detached from session" in output, output
                print("ATTACH/EOF DETACH:", output.strip(), flush=True)
                assert Path(f"/proc/{pid}").exists()
                cli("session", "send", session, marker, operator=True)
                output = cli("session", "read", session, operator=True)
                assert f"inert: {marker}" in output, output
            cli("session", "close", session, operator=True)
            wait_for(lambda: not Path(f"/proc/{pid}").exists())
            pids.remove(pid)
            # A second execution remains active while the daemon shuts down.
            payload = json.loads(cli("throw", "--now", "--json", operator=True))
            pid = int(payload["results"][0]["summary"].split("pid=")[1])
            pids.append(pid)
            with sqlite3.connect(workspace / "workspace.db") as db:
                plans = [json.loads(row[0]) for row in db.execute("select plan_json from throw_plans")]
            assert plans and all(p["confirmationId"] for p in plans), plans
            daemon.send_signal(signal.SIGTERM)
            assert daemon.wait(timeout=10) == 0
            wait_for(lambda: not Path(f"/proc/{pid}").exists())
            pids.remove(pid)
            print(f"PASS {'linked' if linked else 'archive'}: discovery/schema, confirmed execution, attach/detach/reattach, close cleanup, daemon shutdown cleanup", flush=True)
        finally:
            if daemon and daemon.poll() is None:
                daemon.terminate()
                try:
                    daemon.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    daemon.kill()
                    daemon.wait(timeout=5)
            for pid in pids:
                try:
                    os.kill(pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
