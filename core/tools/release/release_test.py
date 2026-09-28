"""Exercise release authorization guards through the actual validation CLI."""

import os
import json

from release import artifact_names, checksum, manifest, verify_bundle
from report import snapshot
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

with tempfile.TemporaryDirectory() as temporary:
    root = Path(temporary)
    subprocess.run(["git", "init", "-q", str(root)], check=True)
    (root / ".gitignore").write_text("/dist/\n")
    (root / "VERSION").write_text("1.2.3\n")
    subprocess.run(["git", "-C", str(root), "add", "."], check=True)
    subprocess.run(
        [
            "git",
            "-C",
            str(root),
            "-c",
            "user.name=Fixture",
            "-c",
            "user.email=fixture@example.invalid",
            "commit",
            "-qm",
            "fixture",
        ],
        check=True,
    )
    directory = root / "dist"
    directory.mkdir()
    for name in artifact_names("1.2.3"):
        (directory / name).write_text(name)

    def refuse(fn, *args):
        try:
            fn(*args)
        except (ValueError, OSError):
            return
        raise AssertionError("accepted unverified distribution")

    refuse(manifest, root, "1.2.3")
    (directory / ".smoke.json").write_text(
        json.dumps({name: checksum(directory / name) for name in artifact_names("1.2.3")})
    )
    manifest(root, "1.2.3")
    source = snapshot(root)
    verify_bundle(directory, "1.2.3", source)
    refuse(verify_bundle, directory, "1.2.3", source | {"commit": "0" * 40})
    refuse(verify_bundle, directory, "1.2.3", source | {"dirty": True})
    refuse(verify_bundle, directory, "1.2.3", source, {"id": "another-run", "run_attempt": 1})
    wheel = next(directory.glob("*.whl"))
    saved = wheel.read_bytes()
    wheel.write_bytes(b"changed")
    refuse(verify_bundle, directory, "1.2.3", source)
    wheel.write_bytes(saved)
    (directory / "extra.whl").write_text("stale")
    refuse(verify_bundle, directory, "1.2.3", source)
    (directory / "extra.whl").unlink()
    (directory / "SHA256SUMS").write_text("incorrect")
    refuse(verify_bundle, directory, "1.2.3", source)
print("PASS exact source, smoke evidence, run identity, file set and distribution checksum guards")
