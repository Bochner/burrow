"""Exercise offline discovery through the shipped binary, without a workspace."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

binary = str(Path(sys.argv[1]).resolve())
with tempfile.TemporaryDirectory(prefix="burrow-api-") as scratch:
    root = Path(scratch)
    env = dict(os.environ, HOME=str(root / "home"), XDG_CACHE_HOME=str(root / "cache"))
    result = subprocess.run([binary, "capabilities"], env=env, cwd=root,
                            capture_output=True, text=True, timeout=15)
    assert result.returncode == 0, result
    contract = json.loads(result.stdout)
    assert contract["schemaVersion"] == 1
    assert contract["module"]["name"] == "burrow"
    assert contract["provenance"]["binarySHA256"]
    operations = {op["id"]: op for op in contract["operations"]}
    assert len(operations) == len(contract["operations"])
    assert {"workspace", "connection", "profile", "shell", "tunnel", "files",
            "transfer", "run", "report", "chain", "logs", "installation"} <= {
                op["category"] for op in operations.values()}
    for op in operations.values():
        for key in ("id", "summary", "human", "agent", "scope", "effects", "review", "evidence"):
            assert op[key], (op["id"], key)
        assert op["result"] in contract["results"], op
        for name in op["inputs"]:
            assert name in contract["inputs"], (op["id"], name)
    # Independently inspect the human command list. Shared execution already
    # refuses unregistered operations; help must not advertise an orphan route.
    help_result = subprocess.run([binary, "--help"], env=env, cwd=root,
                                 capture_output=True, text=True, timeout=15)
    assert help_result.returncode == 0
    patterns = [pattern for op in operations.values() for pattern in op["patterns"]]
    for syntax in re.findall(r"^([a-z][^\n]*?) {2,}\S", help_result.stderr, re.M):
        tokens = syntax.split()
        assert any(len(pattern) <= len(tokens) and all(part == "*" or part == token for part, token in zip(pattern, tokens))
                   for pattern in patterns), ("human command has no capability inventory entry", syntax)
    assert operations["workspace.open"]["effects"].startswith("Initializes")
    for name in ("open", "inspect", "list", "restart", "retire"):
        assert operations["workspace." + name]["agent"]["status"] == "supported"
    assert "--review HASH" in operations["profile.connect"]["agent"]["syntax"]
    assert "--review HASH" in operations["connection.close"]["agent"]["syntax"]
    assert contract["errors"]["workspace"]["encoding"] == "JSON/stderr"
    assert operations["shell.resume"]["agent"]["status"] == "unsupported"
    assert operations["installation.skills"]["agent"]["status"] == "supported"
    assert operations["installation.skills"]["result"] == "SkillInstallation"
    assert operations["logs.view"]["agent"]["status"] == "terminal-only"
    assert operations["logs.follow"]["agent"]["status"] == "supported"
    assert operations["logs.follow"]["result"] == "Activity"
    assert {"workspacePath", "kind", "source", "time", "observedAt"} <= set(contract["results"]["Activity"]["required"])
    assert contract["results"]["Activity"]["properties"]["data"]["type"] == "string"
    assert contract["errors"]["encoding"] == "text/stderr"
    # Reachability only: valid examples must pass the production parser and
    # reach the same missing-workspace refusal, without starting local state.
    workspace = str(root / "untouched")
    baseline = subprocess.run([binary, "--workspace", workspace, "connections"],
                              env=env, capture_output=True, text=True, timeout=15)
    assert baseline.returncode == 1
    for op in operations.values():
        if not op["dispatch"] or op["agent"]["status"] != "supported":
            continue
        argv = [workspace if token == "/absolute/workspace" else token
                for token in op["agent"]["example"][1:]]
        route = subprocess.run([binary, *argv], env=env, capture_output=True,
                               text=True, timeout=15)
        error = route.stderr
        if op["id"] in ("connection.close", "profile.connect"):
            error = "Burrow: " + json.loads(error)["error"]["message"] + "\n"
        assert route.returncode == 1 and error == baseline.stderr, (op["id"], route, baseline)
    for verb in ("prepare", "now"):
        assert any("-- [ARG...]" in variant for variant in operations[f"run.{verb}"]["agent"]["variants"])
        script = subprocess.run([binary, "--workspace", workspace, "run", verb, "target",
                                 "--script", "check.sh", "--mode", "stream", "--interpreter", "/bin/sh", "--"],
                                env=env, capture_output=True, text=True, timeout=15)
        assert script.returncode == 1 and script.stderr == baseline.stderr, script
    for args in (["capabilities", "run.output"], ["--offline", "capabilities", "run.output"]):
        selected = subprocess.run([binary, *args], env=env, cwd=root,
                                  capture_output=True, text=True, timeout=15)
        assert selected.returncode == 0, selected
        assert json.loads(selected.stdout)["operations"] == [operations["run.output"]]
    bad = subprocess.run([binary, "capabilities", "not-an-operation"], env=env,
                         capture_output=True, text=True, timeout=15)
    assert bad.returncode == 1 and "unknown capability" in bad.stderr, bad
    assert not list(root.iterdir()), "discovery initialized local state"
print("PASS offline binary discovery, stable selection, route limitations and contract references")
