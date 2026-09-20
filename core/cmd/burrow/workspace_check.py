"""Headless workspace selection uses explicit paths and the real pinned daemon."""
import json
import os
from pathlib import Path
import pty
import select
import signal
import subprocess
import sys
import tempfile

binary, wheel = [str(Path(p).resolve()) for p in sys.argv[1:]]
with tempfile.TemporaryDirectory(prefix="bw-") as scratch:
    root = Path(scratch)
    env = dict(os.environ, HOME=scratch, XDG_CACHE_HOME=str(root / "cache"),
               XDG_DATA_HOME=str(root / "data"), XDG_CONFIG_HOME=str(root / "config"))
    daemons = []

    def cli(*args, ok=True):
        p = subprocess.run([binary, *map(str, args)], env=env, capture_output=True,
                           text=True, timeout=40)
        assert (p.returncode == 0) == ok, (args, p.stdout, p.stderr)
        if ok:
            assert not p.stderr, p.stderr
            return json.loads(p.stdout)
        assert not p.stdout, p.stdout
        error = json.loads(p.stderr)["error"]
        assert error["code"] and error["message"] and error["operation"], error
        return error

    try:
        w, missing = root / "w", root / "missing"
        assert cli("workspace", "open", ok=False)["code"] == "invalid_selection"
        assert cli("--workspace", w, "--workspace", missing, "workspace", "open", ok=False)["code"] == "invalid_selection"
        assert cli("--workspace", missing, "workspace", "inspect", ok=False)["code"] == "unverified"
        listing = cli("workspace", "list", w, missing)
        assert listing["scope"] == "explicit-paths" and listing["registryAvailable"] is False
        assert [item["state"] for item in listing["workspaces"]] == ["unverified", "unverified"]
        assert cli("workspace", "list", ok=False)["code"] == "invalid_selection"
        assert cli("--workspace", w, "workspace", "list", missing, ok=False)["code"] == "invalid_selection"
        assert not list(root.iterdir()), "inspection initialized workspace/cache state"

        info = cli("--workspace", w, "--hovel-package", wheel, "workspace", "open")
        daemons.append(info["pid"])
        assert info["workspacePath"] == str(w)
        assert cli("--workspace", w, "workspace", "inspect") == info
        assert cli("--workspace", w, "--offline", "workspace", "open") == info
        # Actual TTY output uses semantic JSON colors, with a readable NO_COLOR form.
        for plain in (False, True):
            master, slave = pty.openpty()
            color_env = env | {"TERM": "xterm-256color", "COLORTERM": "truecolor", "NO_COLOR": "1" if plain else ""}
            try:
                for command, expected, stream in ((["--workspace", str(w), "workspace", "inspect"], 0, "stdout"),
                                                   (["workspace", "open"], 1, "stderr")):
                    p = subprocess.run([binary, *command], env=color_env, timeout=20,
                                       stdout=slave if stream == "stdout" else subprocess.PIPE,
                                       stderr=slave if stream == "stderr" else subprocess.PIPE)
                    assert p.returncode == expected, p
                    data = bytearray()
                    while select.select([master], [], [], .1)[0]:
                        data.extend(os.read(master, 65536))
                    assert (b"\x1b[38;2;" in data) != plain, data
                    if plain:
                        result = json.loads(data)
                        assert result == info if expected == 0 else result["error"]["code"] == "invalid_selection"
            finally:
                os.close(master)
                os.close(slave)
        listing = cli("workspace", "list", w, missing)
        assert [item["state"] for item in listing["workspaces"]] == ["verified", "unverified"]
        assert not missing.exists()
        review = cli("--workspace", w, "workspace", "retire")
        assert review["state"] == "review" and review["connections"] == []
        assert cli("--workspace", w, "workspace", "retire", "--yes", ok=False)["code"] == "review_required"
        assert cli("--workspace", w, "workspace", "restart", "--yes", "--review", review["digest"], ok=False)["code"] == "review_changed"
        retired = cli("--workspace", w, "workspace", "retire", "--yes", "--review", review["digest"])
        assert retired["state"] == "retired" and retired["owner"] == review["owner"]
        assert cli("--workspace", w, "workspace", "inspect") == info
        assert not (w / "burrow/.manager-v1").exists()

        # Never let a receipt redirect an explicit selection to another workspace.
        receipt = w / "burrow-launch.json"
        original = receipt.read_text()
        altered = json.loads(original)
        altered["workspace"] = str(missing)
        receipt.write_text(json.dumps(altered))
        assert cli("--workspace", w, "workspace", "inspect", ok=False)["code"] == "unverified"
        cli("--workspace", w, "workspace", "retire", ok=False)
        assert receipt.read_text() == json.dumps(altered)
        receipt.write_text(original)
        os.kill(info["pid"], signal.SIGTERM)
        cli("--workspace", w, "workspace", "inspect", ok=False)
        cli("--workspace", w, "--offline", "workspace", "open", ok=False)
        assert receipt.read_text() == original and not missing.exists()
        print("PASS headless workspace selection, scoped discovery, review binding and loss refusal")
    finally:
        for pid in daemons:
            try:
                os.kill(pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
