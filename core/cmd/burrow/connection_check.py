"""Public production command checks; no operator workspace or SSH state."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile

binary = str(Path(sys.argv[1]).resolve())
with tempfile.TemporaryDirectory(prefix="bc-") as scratch:
    workspace = Path(scratch) / "untouched"
    for name in ("../escape", "-option", "a b", "é", "x" * 25):
        result = subprocess.run([binary, "--workspace", str(workspace), "connect", name,
                                 "localhost", "tester", "--yes"], capture_output=True, text=True)
        assert result.returncode != 0 and "name must" in result.stderr, result
        assert not workspace.exists()
    for args, message in [
        (["connect", "valid", "localhost", "tester", "--ssh-config", "relative"], "absolute"),
        (["connect", "valid", "localhost", "tester", "--jump", "bad;command"], "jump"),
        (["connect", "valid", "localhost", "tester", "--key", "/tmp/key", "--port", "0"], "port"),
        (["connect", "valid", "localhost", "tester", "--key", "/tmp/key", "--trust", "not-a-fingerprint"], "fingerprint"),
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
