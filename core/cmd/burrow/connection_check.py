"""Public production command checks; no operator workspace or SSH state."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile

binary = str(Path(sys.argv[1]).resolve())
makefile = str(Path(sys.argv[2]).resolve())
for target, expected in {
    "run": 'aspect burrow run -- --workspace "$BURROW_MAKE_WORKSPACE" tui',
    "restart": 'aspect burrow run -- --workspace "$BURROW_MAKE_WORKSPACE" restart --yes',
    "clean": "aspect burrow clean",
    "check": "aspect burrow-check ci",
}.items():
    result = subprocess.run(["/usr/bin/make", "--no-print-directory", "-n", "-f", makefile,
                             target, "WORKSPACE=/tmp/path with spaces"],
                            check=True, capture_output=True, text=True)
    assert result.stdout.strip() == expected, result.stdout
print("PASS human Make shortcuts delegate to Aspect without executing cleanup")
with tempfile.TemporaryDirectory(prefix="bc-") as scratch:
    workspace = Path(scratch) / "untouched"
    for name in ("../escape", "a b", "é", "x" * 25):
        result = subprocess.run([binary, "--workspace", str(workspace), "connect", name,
                                 "localhost", "tester", "--yes"], capture_output=True, text=True)
        assert result.returncode != 0 and "name must" in result.stderr, result
        assert not workspace.exists()
    for args, message in [
        (["tunnel", "create", "gateway", "forward", "8080"], "CONNECTION forward LISTEN HOST PORT"),
        (["tunnel", "create", "gateway", "forward", "0", "localhost", "80", "--yes"], "port"),
        (["tunc", "gateway", "l", "8080", "localhost", "0"], "port"),
        (["tunc", "gateway", "l", "*:8080", "localhost", "80"], "literal IP"),
        (["tunc", "gateway", "l", "8080", "bad;command", "80"], "destination"),
        (["tunc", "gateway", "l", "8080", "localhost", "80", "--password", "SYNTHETIC-NOT-A-REAL-SECRET"], "forward options"),
        (["tund", "1", "--yes"], "qualified tunnel ID"),
        (["tunnel", "list", "extra"], "takes no arguments"),
        (["tunnel"], "create|list|check|remove"),
        (["tunnel", "create", "gateway", "reverse", "8080", "localhost", "80"], "not implemented"),
        (["tunc", "gateway", "r", "8080", "localhost", "80"], "not implemented"),
        (["restart", "--yes"], "restart requires terminal input"),
        (["restart", "--invalid"], "expected restart [--yes]"),
        (["restart"], "restart requires terminal input"),
        (["shell"], "expected shell NAME"),
        (["shell", "../escape"], "name must"),
        (["shell", "valid", "--key", "/tmp/key"], "expected shell NAME"),
        (["shells", "other"], "takes no arguments"),
        (["resume"], "expected resume ID"),
        (["resume", "-1"], "positive decimal"),
        (["resume", "01"], "positive decimal"),
        (["shell-close", "0"], "positive decimal"),
        (["shell-close", "1", "2"], "expected shell-close ID"),
        (["connect", "-ip", "localhost", "-user", "tester", "-socket", "valid", "-ssh-key", "relative"], "absolute"),
        (["connect", "--port", "0", "valid", "localhost", "tester"], "port"),
        (["connect", "valid", "localhost", "tester", "-ip", "other"], "duplicate"),
        (["connect", "-socket", "valid", "-ip", "localhost"], "NAME HOST USER"),
        (["connect", "valid", "localhost", "tester", "-ssh-key", "/tmp/key", "--key", "/tmp/other"], "duplicate"),
        (["connect", "valid", "localhost", "tester", "-no-term"], "invalid connection options"),
        (["connect", "valid", "localhost", "tester", "-proxy", "0"], "port"),
        (["connect", "valid", "localhost", "tester", "-proxy", "65536"], "port"),
        (["connect", "valid", "localhost", "tester", "-proxy=bad"], "invalid connection options"),
        (["connect", "valid", "localhost", "tester", "-proxy=1080", "--proxy=9050"], "duplicate"),
        (["connect", "valid", "localhost", "tester", "-shell", "bash"], "invalid connection options"),
        (["connect", "valid", "localhost", "tester", "--ssh-config", "relative"], "absolute"),
        (["connect", "valid", "localhost", "tester", "--jump", "bad;command"], "jump"),
        (["connect", "valid", "localhost", "tester", "--key", "/tmp/key", "--port", "0"], "port"),
        (["connect", "valid", "localhost", "tester", "--trust", "not-a-fingerprint"], "invalid connection options"),
        (["connect", "valid", "localhost", "tester", "--known-hosts", "/tmp/known_hosts"], "invalid connection options"),
        (["connect", "valid", "localhost", "tester", "--key", "relative"], "absolute"),
        (["connect", "valid", "localhost", "tester", "--password", "SYNTHETIC-NOT-A-REAL-SECRET"], "invalid connection options"),
    ]:
        result = subprocess.run([binary, "--workspace", str(workspace), *args], capture_output=True, text=True)
        assert result.returncode != 0 and message in result.stderr, result
        assert "SYNTHETIC-NOT-A-REAL-SECRET" not in result.stdout + result.stderr
        assert not workspace.exists()
    result = subprocess.run([binary, "--workspace", str(Path(scratch) / ("x" * 60)),
                             "connect", "a" * 24, "localhost", "tester", "--key", "/tmp/key"], capture_output=True, text=True)
    assert result.returncode != 0 and "90 bytes" in result.stderr, result
print("PASS invalid names refused before workspace mutation")
