"""Check the real executable's noninteractive contract, not screen scraping."""
import json
import subprocess
import sys

binary = sys.argv[1]
script = "connect\nscp\ncd /var/log\nls\nget auth.log\nlls /downloads\nback\nproxy 1080\nloss\nconnections\n"
run = subprocess.run([binary, "--json", "--script", "-"], input=script, text=True, capture_output=True, timeout=10)
assert run.returncode == 0, run.stderr
rows = [json.loads(line) for line in run.stdout.splitlines()]
assert len(rows) == 10
assert rows[3]["entries"][0]["name"] == "auth.log"
assert rows[4]["message"].startswith("Transfer COMPLETE · 29 bytes · ")
assert rows[4]["message"].endswith("elapsed")
assert rows[5]["entries"][0]["name"] == "auth.log"
assert "DISCONNECTED" in rows[-1]["message"]
run = subprocess.run([binary, "--json", "scp"], text=True, capture_output=True, timeout=10)
assert run.returncode == 1
assert "connect explicitly" in json.loads(run.stdout)["error"]
run = subprocess.run([binary], input="", text=True, capture_output=True, timeout=10)
assert run.returncode == 2 and "needs a terminal" in run.stderr
print("Noninteractive JSON, transfer results, failure exit and no-TTY refusal passed.")
