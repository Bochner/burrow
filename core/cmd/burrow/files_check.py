"""Workspace file roots and local navigation through the production CLI."""

import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import threading
from concurrent.futures import ThreadPoolExecutor

from core.cmd.burrow.file_history_lab import file_rpc, history_events, reopen_files

binary, wheel = [str(Path(p).resolve()) for p in sys.argv[1:]]
with tempfile.TemporaryDirectory(prefix="bf-") as scratch:
    root = Path(scratch)
    workspace = root / "w"
    env = {k: v for k, v in os.environ.items() if not k.startswith(("HOVEL_", "SSH_"))}
    env.update(
        HOME=scratch,
        XDG_CACHE_HOME=str(root / "cache"),
        XDG_CONFIG_HOME=str(root / "config"),
        BURROW_PHASE_TRACE=str(root / "phases.jsonl"),
    )

    def run(*args, ok=True, w=workspace):
        result = subprocess.run(
            [binary, "--workspace", str(w), *args], env=env, capture_output=True, text=True, timeout=40
        )
        assert (result.returncode == 0) == ok, (args, result.stdout, result.stderr)
        return json.loads(result.stdout) if ok else result.stderr

    info = run("--hovel-package", wheel, "status")
    try:
        snapshot = file_rpc(workspace, "Snapshot", {})
        assert "burrow" not in [op["name"] for op in snapshot["State"].get("operations", [])], snapshot
        barrier = threading.Barrier(2)

        def first_write(area):
            barrier.wait(timeout=5)
            return run("lls", area)

        with ThreadPoolExecutor(max_workers=2) as pool:
            assert all(result["entries"] == [] for result in pool.map(first_write, ("upload", "download")))
        assert sorted(run("files-history")) == ["lls download", "lls upload"]
        assert len(history_events(workspace)) == 2, "redundant chain-created event remains"
        published = file_rpc(workspace, "PollLogs", {"Operation": "burrow", "Chain": "files", "Since": 0})["Logs"]
        assert sorted(item["Entry"]["Message"] for item in published) == ["lls download", "lls upload"], published
        assert all(item["Entry"]["Source"] == "burrow-files" for item in published)
        trace = [json.loads(line)["phase"] for line in (root / "phases.jsonl").read_text().splitlines()]
        assert not {"hovel-process:op", "hovel-process:chain"} & set(trace), "history setup still launches Hovel"
        file_rpc(workspace, "DeleteChain", {"Operation": "burrow", "Chain": "files"})
        run("lls", "upload")
        assert run("files-history") == ["lls upload"], "deleted chain was not recreated by append"
        info = reopen_files(workspace, info, lambda: run("--offline", "status"), fault=True)
        assert run("files-history") == ["lls upload"], "acknowledged append lost across daemon restart"
        paths = run("local")
        assert paths["upload"] == str(workspace / "burrow-files/uploads"), paths
        assert paths["download"] == str(workspace / "burrow-files/downloads"), paths
        upload = Path(paths["upload"])
        assert upload.stat().st_mode & 0o777 == 0o700
        (upload / "sub dir").mkdir()
        (upload / "sub dir/α.txt").write_text("retained")
        run("local", "upload", str(root / "alternate"))
        run("--offline", "status")
        assert run("local")["upload"] == str(root / "alternate")
        assert (upload / "sub dir/α.txt").read_text() == "retained"
        run("local", "upload", str(upload))
        listing = run("lls", "upload", "sub dir")
        assert [row["name"] for row in listing["entries"]] == ["α.txt"], listing
        assert run("lcd", "upload", "sub dir")["path"] == str(upload / "sub dir")
        assert run("local")["upload"] == str(upload), "navigation changed root"
        outside = root / "outside"
        outside.mkdir()
        (outside / "private.txt").write_text("OUTSIDE-CANARY")
        (upload / "escape").symlink_to(outside, target_is_directory=True)
        (upload / "inside").symlink_to("sub dir", target_is_directory=True)
        (upload / "absolute-inside").symlink_to(upload / "sub dir", target_is_directory=True)
        (upload / "broken").symlink_to("missing")
        names = {row["name"] for row in run("lls", "upload")["entries"]}
        assert {"escape", "inside", "absolute-inside", "broken"} <= names
        for name in ("inside", "absolute-inside"):
            assert run("lls", "upload", name)["entries"][0]["name"] == "α.txt"
        for path in ("../..", "escape", "broken", str(outside)):
            assert "OUTSIDE-CANARY" not in run("lls", "upload", path, ok=False)
        config = workspace / "burrow-files/config.json"
        before = config.read_bytes()
        run("local", "upload", str(upload / "inside"), ok=False)
        assert config.read_bytes() == before
        run("local", "upload", str(outside / "private.txt"), ok=False)
        assert config.read_bytes() == before
        upload.chmod(0o000)
        try:
            run("local", ok=False)
        finally:
            upload.chmod(0o700)
        assert run("local")["upload"] == str(upload)
        assert run("connections") == [], "local browsing authenticated"
        for invalid in (b"{}", b'{"version":1}', b'{"version":1,"upload":"/tmp"}', b"null"):
            config.write_bytes(invalid)
            run("local", ok=False)
            assert config.read_bytes() == invalid, "invalid roots silently repaired"
        config.write_bytes(before)
        history = run("files-history")
        assert any(line.startswith("lls upload ") for line in history), history
        run("--offline", "status")
        assert run("files-history") == history, "file history lost on reopen"
        assert not any(line.startswith("lls") for line in run("history")), "file history mixed into management"
        other = root / "other"
        other_info = run("--offline", "status", w=other)
        try:
            other_roots = run("local", w=other)
            assert other_roots["upload"] != str(upload)
            assert run("lls", "upload", w=other)["entries"] == []
            assert not any("sub dir" in line for line in run("files-history", w=other))
        finally:
            os.kill(other_info["pid"], signal.SIGTERM)
        hovel = root / "cache/burrow/hovel/0.4.4/hovel"

        def hv(*args):
            p = subprocess.run(
                [
                    str(hovel),
                    "run",
                    "--workspace",
                    str(workspace),
                    "--op",
                    "file-check",
                    "--chain",
                    "file-check",
                    "--",
                    *args,
                ],
                env=env,
                capture_output=True,
                text=True,
                timeout=30,
            )
            assert p.returncode == 0, (args, p.stdout, p.stderr)
            return p.stdout

        hv("op", "create", "file-check")
        hv("chain", "create", "file-check")
        hv("chain", "add", "burrow@0.1.0")
        hv("target", "add", "local")
        hv("chain", "config", "set", "workspace", str(workspace))
        hv("chain", "config", "set", "command", "lls upload 'sub dir'")
        result = hv("throw", "--now", "--allow-dangerous", "--json")
        assert "α.txt" in result or "\\u03b1.txt" in result, result
        assert run("run", "list") == []
        run("run", "prepare", "missing", "--", "true", ok=False)
        assert not list(workspace.glob(".burrow-run-*"))
        hv("chain", "config", "set", "command", "run list")
        result = json.loads(hv("throw", "--now", "--allow-dangerous", "--json"))
        assert result["results"][0]["state"] == "succeeded", result
        assert result["results"][0]["summary"] == "[]", result
        (upload / "history-append-failure").mkdir()
        prior = run("files-history")
        failure = run("lls", "upload", "history-append-failure", ok=False)
        assert "file command completed but history unavailable" in failure, failure
        # Failure is not rollback: the daemon's memory already contains the event.
        # One submission makes one attempt, including on ambiguous persistence.
        after = run("files-history")
        assert after == prior + ["lls upload history-append-failure"], after
        file_rpc(workspace, "DeleteChain", {"Operation": "burrow", "Chain": "files"})
    finally:
        os.kill(info["pid"], signal.SIGTERM)
print("PASS persistent file roots and contained local navigation")
