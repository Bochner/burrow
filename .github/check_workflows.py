"""Check every declared workflow with the pinned actionlint executable."""
import subprocess
import sys

# Optional host-installed linters are not declared inputs to this check.
assert len(sys.argv) > 2, "no declared workflows"
subprocess.run([sys.argv[1], "-shellcheck=", "-pyflakes=", *sys.argv[2:]], check=True)
