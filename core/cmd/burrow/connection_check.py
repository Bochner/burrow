"""Public production command checks; no operator workspace or SSH state."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile

binary = str(Path(sys.argv[1]).resolve())
makefile = str(Path(sys.argv[2]).resolve())
help_result = subprocess.run([binary, "--help"], capture_output=True, text=True)
assert help_result.returncode == 0, help_result
for example in ["chain connect target 192.168.10.50 --user alice --password",
                "chain connect target 192.168.10.50 --user alice --key",
                "connect target 192.168.10.50 --user alice --password"]:
    assert example in help_result.stderr, ("CLI help omitted a concrete example", example)
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
    accepted = subprocess.run([binary, "--workspace", str(workspace), "chain", "connect", "named",
                               "192.0.2.1", "--user", "tester", "--password"], capture_output=True, text=True)
    assert accepted.returncode != 0 and "invalid connection" not in accepted.stderr, accepted
    assert not workspace.exists(), "validation started a workspace"
    for password_args in (["--password", "SYNTHETIC-NOT-A-REAL-SECRET"],
                          ["--password=SYNTHETIC-NOT-A-REAL-SECRET"],
                          ["--password", "quoted spaces"], ["--password="],
                          ["--password=-leading-dash"]):
        result = subprocess.run([binary, "--workspace", str(workspace), "chain", "connect", "named",
                                 "192.0.2.1", "--user", "tester", *password_args], capture_output=True, text=True)
        assert result.returncode != 0 and "invalid connection" not in result.stderr and "terminal" not in result.stderr, result
        assert "SYNTHETIC-NOT-A-REAL-SECRET" not in result.stdout + result.stderr
        assert not workspace.exists(), "validation started a workspace"
    for name in ("../escape", "a b", "é", "x" * 25):
        result = subprocess.run([binary, "--workspace", str(workspace), "connect", name,
                                 "localhost", "tester", "--yes"], capture_output=True, text=True)
        assert result.returncode != 0 and "name must" in result.stderr, result
        assert not workspace.exists()
    for args, message in [
        (["chain"], "chain select CONNECTION"),
        (["chain", "select", "../escape"], "name must"),
        (["chain", "http", "gateway", "gateway/" + "a" * 32, "http://user:SYNTHETIC-NOT-A-REAL-SECRET@example.test/"], "without credentials"),
        (["chain", "http", "gateway", "gateway/" + "a" * 32, "http://example.test/?token=SYNTHETIC-NOT-A-REAL-SECRET"], "without credentials"),
        (["chain", "http", "gateway", "gateway/" + "a" * 32, "https://example.test/"], "http://HOST"),
        (["chain", "http", "gateway", "other/" + "a" * 32, "http://example.test/"], "complete tunnel"),
        (["chain", "connect", "gateway", "localhost", "tester", "-proxy"], "connection-only"),
        (["chain", "connect", "gateway", "localhost", "tester", "--password", "SYNTHETIC-NOT-A-REAL-SECRET", "--key", "/tmp/key"], "choose --password or --key"),
        (["chain", "connect", "gateway", "localhost", "--password", "SYNTHETIC-NOT-A-REAL-SECRET"], "required NAME HOST --user USER"),
        (["connect", "gateway", "localhost", "--password", "SYNTHETIC-NOT-A-REAL-SECRET"], "required NAME HOST --user USER"),
        (["chain", "connect", "gateway", "localhost", "--user", "tester", "--password=SYNTHETIC-NOT-A-REAL-SECRET", "--agent", "/tmp/agent"], "choose --password or --key"),
        (["chain", "connect", "gateway", "localhost", "--user", "tester", "--key", "/tmp/key", "--password"], "choose --password or --key"),
        (["run"], "run prepare CONNECTION"),
        (["run", "prepare", "gateway", "--local", "--", "relative-tool"], "absolute executable"),
        (["run", "prepare", "gateway", "--local", "--script", "a.sh", "--mode", "stage", "--interpreter", "/bin/sh", "--"], "does not stage"),
        (["run", "prepare", "gateway", "--local", "--local", "--", "/bin/true"], "run prepare CONNECTION"),
        (["run", "prepare", "gateway", "--local", "--"], "absolute executable"),
        (["run", "prepare", "gateway", "--timeout", "0s", "--", "true"], "timeout must"),
        (["run", "prepare", "gateway", "--timeout", "garbage", "--", "true"], "timeout must"),
        (["run", "prepare", "gateway", "--script", "a.sh", "--", "x"], "explicit --mode"),
        (["run", "prepare", "gateway", "--script", "a.sh", "--mode", "stream", "--", "x"], "absolute --interpreter"),
        (["run", "prepare", "gateway", "--script", "a.sh", "--mode", "stream", "--interpreter", "/bin/sh", "--stdin", "empty", "--"], "stream mode owns stdin"),
        (["run", "prepare", "gateway", "--mode", "stage", "--", "true"], "require --script"),
        (["run", "prepare", "gateway", "--keep", "--", "true"], "explicitly staged"),
        (["run", "prepare", "gateway", "--budget", "0", "--", "true"], "positive byte count"),
        (["run", "prepare", "../escape", "--", "true"], "name must"),
        (["run", "now", "../escape", "--yes", "--", "true"], "name must"),
        (["run", "prepare", "gateway", "--", ""], "command and positive"),
        (["run", "prepare", "gateway", "--password", "SYNTHETIC-NOT-A-REAL-SECRET", "--", "true"], "run prepare CONNECTION"),
        (["run", "launch", "id", "--password", "SYNTHETIC-NOT-A-REAL-SECRET"], "run prepare CONNECTION"),
        (["run", "output", "id", "stdout", "-1"], "run prepare CONNECTION"),
        (["run", "follow"], "expected run follow ID"),
        (["run", "follow", "id", "other"], "expected run follow ID"),
        (["run", "follow", "id", "stderr", "-1"], "expected run follow ID"),
        (["run", "follow", "id", "stdout", "9223372036854775808"], "expected run follow ID"),
        (["run", "follow", "id", "stdout", "0", "extra"], "expected run follow ID"),
        (["run", "follow", "id"], "run follow requires a terminal"),
        (["run", "list", "extra"], "run prepare CONNECTION"),
        (["tunnel", "create", "gateway", "forward", "8080"], "CONNECTION forward|reverse LISTEN HOST PORT"),
        (["tunnel", "create", "gateway", "forward", "0", "localhost", "80", "--yes"], "port"),
        (["tunc", "gateway", "l", "8080", "localhost", "0"], "port"),
        (["tunc", "gateway", "l", "*:8080", "localhost", "80"], "literal IP"),
        (["tunc", "gateway", "l", "8080", "bad;command", "80"], "destination"),
        (["tunc", "gateway", "l", "8080", "localhost", "80", "--password", "SYNTHETIC-NOT-A-REAL-SECRET"], "forward options"),
        (["tund", "1", "--yes"], "qualified tunnel ID"),
        (["tunnel", "list", "extra"], "takes no arguments"),
        (["proxy"], "required"),
        (["proxy", "create", "gateway"], "LISTEN"),
        (["proxy", "create", "gateway", "0"], "port"),
        (["proxy", "create", "gateway", "65536"], "port"),
        (["proxy", "create", "gateway", "localhost:1080"], "literal IP"),
        (["proxy", "create", "gateway", "127.0.0.1:1080\u001b"], "port"),
        (["proxy", "create", "../escape", "1080"], "name must"),
        (["proxy", "create", "gateway", "1080", "--password", "SYNTHETIC-NOT-A-REAL-SECRET"], "forward options"),
        (["proxy", "remove", "gateway", "--review", "bad"], "recap digest"),
        (["proxy", "inspect", "gateway", "--yes"], "only CONNECTION"),
        (["proxy", "unknown", "gateway"], "create|inspect|remove"),
        (["tunnel"], "create|list|check|remove"),
        (["tunnel", "create", "gateway", "reverse", "8080"], "explicit local destination"),
        (["tunc", "gateway", "r", "0", "localhost", "0"], "port"),
        (["tunc", "gateway", "r", "00", "localhost", "80"], "port"),
        (["tunc", "gateway", "r", "0", "host\u001b]52;c;bad", "80"], "destination"),
        (["tunc", "gateway", "r", "*:0", "localhost", "80"], "literal IP"),
        (["tunc", "gateway", "r", "0", "localhost", "80", "--password", "SYNTHETIC-NOT-A-REAL-SECRET"], "forward options"),
        (["tunnel", "create", "gateway", "other", "8080", "localhost", "80"], "direction"),
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
        (["connect", "-socket", "valid", "-ip", "localhost"], "NAME HOST --user USER"),
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
        (["connect", "valid", "localhost", "tester", "--password", "SYNTHETIC-NOT-A-REAL-SECRET\n"], "invalid password"),
        (["connect", "valid", "localhost", "tester", "--password=" + "x" * 4097], "invalid password"),
    ]:
        result = subprocess.run([binary, "--workspace", str(workspace), *args], capture_output=True, text=True)
        assert result.returncode != 0 and message in result.stderr, result
        assert "SYNTHETIC-NOT-A-REAL-SECRET" not in result.stdout + result.stderr
        assert not workspace.exists()
    result = subprocess.run([binary, "--workspace", str(Path(scratch) / ("x" * 60)),
                             "connect", "a" * 24, "localhost", "tester", "--key", "/tmp/key"], capture_output=True, text=True)
    assert result.returncode != 0 and "90 bytes" in result.stderr, result
print("PASS invalid names refused before workspace mutation")
