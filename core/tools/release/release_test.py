"""Exercise release authorization guards through the actual validation CLI."""

import os
from pathlib import Path
import subprocess
import sys
import tempfile

script = Path(__file__).with_name("release.py")
with tempfile.TemporaryDirectory() as temporary:
    root = Path(temporary)
    env = os.environ | {"BUILD_WORKSPACE_DIRECTORY": str(root), "GITHUB_OUTPUT": str(root / ".git/output")}

    def git(*args):
        subprocess.run(["git", "-C", str(root), *args], check=True, capture_output=True)

    def validate(*args, ok=False):
        result = subprocess.run(
            [sys.executable, str(script), "validate", *args], env=env, capture_output=True, text=True
        )
        assert (result.returncode == 0) == ok, result

    git("init", "-q")
    (root / "VERSION").write_text("0.2.1.dev0\n")
    git("add", "VERSION")
    git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture")
    git("update-ref", "refs/remotes/origin/main", "HEAD")
    git("tag", "v0.2.1.dev0")
    validate(ok=True)
    validate("--publish", "true")
    validate("--tag", "v0.2.0", "--publish", "true")
    validate("--tag", "v0.2.1.dev0", "--publish", "true", ok=True)
    (root / "dirty").write_text("not committed")
    validate("--tag", "v0.2.1.dev0", "--publish", "true")
    git("add", "dirty")
    git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "unmerged")
    validate("--tag", "v0.2.1.dev0", "--publish", "true")
    git("tag", "-f", "v0.2.1.dev0")
    validate("--tag", "v0.2.1.dev0", "--publish", "true")
    print("PASS build-only mode and publish refusal for missing/mismatched tags, dirty and unmerged source")
