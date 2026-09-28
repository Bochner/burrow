"""Public history observations and offline fault setup in disposable workspaces."""

import http.client
from contextlib import closing
import json
import os
from pathlib import Path
import signal
import socket
import sqlite3
import tempfile
import time


def file_rpc(workspace, method, body):
    client = http.client.HTTPConnection("localhost", timeout=10)
    client.sock = socket.socket(socket.AF_UNIX)
    client.sock.settimeout(10)
    client.sock.connect(str(workspace / "hoveld.sock"))
    try:
        client.request(
            "POST", "/hovel.daemon.v1.DaemonService/" + method, json.dumps(body), {"Content-Type": "application/json"}
        )
        response = client.getresponse()
        data = response.read()
        assert response.status == 200, (method, response.status, data)
        return json.loads(data)
    finally:
        client.close()


def reopen_files(workspace, info, open_workspace, fault=False):
    """Clean daemon exit, then explicit fixture-only recovery; no automatic adoption."""
    os.kill(info["pid"], signal.SIGTERM)
    deadline = time.monotonic() + 15
    while True:
        try:
            exited = Path(f"/proc/{info['pid']}/stat").read_text().split(") ", 1)[1].startswith("Z")
        except FileNotFoundError:
            exited = True
        if exited:
            break
        assert time.monotonic() < deadline, "clean daemon exit timed out"
        time.sleep(0.05)
    # Preserve dead launch evidence separately. Burrow intentionally refuses
    # stale receipts; this is lab recovery, not a new product restart workflow.
    saved = Path(tempfile.mkdtemp(prefix="file-reopen-", dir=workspace.parent))
    for name in ("burrow-launch.json", "burrow-launch.log", "hoveld.sock", "daemon.json", "daemon.lock", "burrow"):
        path = workspace / name
        if path.exists() or path.is_symlink():
            path.rename(saved / name)
    with closing(sqlite3.connect(workspace / "workspace.db")) as db:
        assert db.execute("pragma integrity_check").fetchone() == ("ok",)
        if fault:
            # Install only while stopped. Public AppendLog mutates/publishes
            # before persistence; this gives a real error with uncertain durability.
            db.execute("""CREATE TRIGGER file_history_failure BEFORE UPDATE ON operator_sessions
                          WHEN instr(NEW.state_json, 'history-append-failure') > 0
                          BEGIN SELECT RAISE(FAIL, 'file history lab persistence failure'); END""")
            db.commit()
    return open_workspace()


def history_events(workspace):
    return file_rpc(workspace, "ActiveLogs", {"Operation": "burrow", "Chain": "files"}) or []


def oversize_history(workspace):
    """Exceed Burrow's bounded ActiveLogs read via the real public daemon API."""
    for _ in range(2):
        file_rpc(
            workspace,
            "AppendLog",
            {
                "Operation": "burrow",
                "Chain": "files",
                "Entries": [
                    {"Kind": "event", "Level": "info", "Source": "history-read-lab", "Message": "x" * (600 * 1024)}
                ],
            },
        )
