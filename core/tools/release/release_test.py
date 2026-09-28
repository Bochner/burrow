"""Exercise release authorization guards through the actual validation CLI."""

import os
import json

from release import (
    artifact_names,
    fetch_bundle,
    checksum,
    manifest,
    verify_bundle,
    validate as validate_source,
    release_notes,
    version_key,
)
from report import snapshot
from pathlib import Path
import subprocess
import sys
import tempfile
import shutil
from unittest.mock import patch

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
    (root / "CHANGELOG.md").write_text("# Changelog\n\n## [0.2.1.dev0]\n- Fixture release.\n")
    git("add", "VERSION", "CHANGELOG.md")
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
    with patch.dict(os.environ, {"GITHUB_RUN_ID": "101", "GITHUB_RUN_ATTEMPT": "1"}):
        manifest(root, "1.2.3")
    source = snapshot(root)
    verify_bundle(directory, "1.2.3", source)
    refuse(verify_bundle, directory, "1.2.3", source | {"commit": "0" * 40})
    refuse(verify_bundle, directory, "1.2.3", source | {"dirty": True})
    refuse(verify_bundle, directory, "1.2.3", source, {"id": "another-run", "run_attempt": 1})
    # Automatic promotion must use the triggering run and all existing trust guards.
    run = {
        "id": 101,
        "run_attempt": 1,
        "event": "push",
        "conclusion": "success",
        "status": "completed",
        "head_sha": source["commit"],
        "path": ".github/workflows/ci.yml",
        "head_branch": "main",
        "head_repository": {"full_name": "fixture/burrow"},
        "html_url": "https://example.invalid/runs/101",
    }
    responses = {
        "actions/runs/101": run,
        "actions/runs/101/attempts/1/jobs?per_page=100": {"jobs": [{"name": "repository", "conclusion": "success"}]},
    }
    stored = root / ".git/bundle"
    directory.rename(stored)
    with (
        patch.dict(os.environ, {"GITHUB_REPOSITORY": "fixture/burrow", "GITHUB_RUN_ID": "202"}),
        patch("ci.api", side_effect=responses.__getitem__),
        patch(
            "ci.download",
            side_effect=lambda run, name, destination: shutil.copytree(stored, destination, dirs_exist_ok=True),
        ),
        patch("ci.successful_runs", side_effect=AssertionError("automatic promotion searched other runs")) as search,
    ):
        fetch_bundle(root, "1.2.3", "101")
        verify_bundle(directory, "1.2.3", source, run)
        shutil.rmtree(directory)
        search.side_effect = lambda commit, event: [run] if event == "push" else []
        fetch_bundle(root, "1.2.3")
        search.assert_called_once_with(source["commit"], "push")
        shutil.rmtree(directory)
        for field, bad in (("conclusion", "failure"), ("head_sha", "0" * 40), ("event", "pull_request")):
            original = run[field]
            run[field] = bad
            refuse(fetch_bundle, root, "1.2.3", "101")
            assert not directory.exists(), "ineligible run staged a bundle"
            run[field] = original
    stored.rename(directory)
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


with tempfile.TemporaryDirectory() as temporary:
    root = Path(temporary)

    def git(*arguments):
        return subprocess.check_output(["git", "-C", str(root), *arguments], text=True).strip()

    def commit(version, notes):
        (root / "VERSION").write_text(version + "\n")
        (root / "CHANGELOG.md").write_text(notes)
        git("add", ".")
        git(
            "-c",
            "user.name=Fixture",
            "-c",
            "user.email=fixture@example.invalid",
            "commit",
            "--allow-empty",
            "-qm",
            "fixture",
        )
        git("update-ref", "refs/remotes/origin/main", "HEAD")

    git("init", "-q")
    commit("0.2.1", "# Changelog\n")
    git("tag", "v0.2.1")
    commit("0.2.1", "# Changelog\n")
    assert validate_source(root, "0.2.1", publish=True, automatic=True)["ready"] == "false"
    commit("0.2.2", "# Changelog\n\n## [0.2.2] - 2026-09-28\n- Automatic tested releases.\n")
    refuse(validate_source, root, "0.2.2", "v0.2.1", True, True)
    candidate = validate_source(root, "0.2.2", publish=True, automatic=True)
    assert candidate == {"commit": git("rev-parse", "HEAD"), "tag": "v0.2.2", "ready": "true", "prerelease": "false"}
    assert release_notes(root, "0.2.2") == "- Automatic tested releases.\n"
    assert not git("tag", "--list", "v0.2.2"), "validation created a tag"
    git("tag", "v0.2.2")
    assert validate_source(root, "0.2.2", publish=True, automatic=True) == candidate
    git("tag", "-f", "v0.2.2", "HEAD^")
    refuse(validate_source, root, "0.2.2", "", True, True)
    git("tag", "-d", "v0.2.2")
    (root / "dirty").write_text("changed")
    refuse(validate_source, root, "0.2.2", "", True, True)
    (root / "dirty").unlink()
    for version, notes in (
        ("0.2.1", "# Changelog\n"),
        ("0.2.3", "# Changelog\n"),
        ("0.2.4", "## [0.2.4]\n\n## [0.2.3]\n- Older.\n"),
    ):
        commit(version, notes)
        refuse(validate_source, root, version, "", True, True)
    commit("0.2.5", "## [0.2.5]\n- One.\n## [0.2.5]\n- Duplicate.\n")
    refuse(validate_source, root, "0.2.5", "", True, True)
    assert version_key("0.2.10") > version_key("0.2.9") > version_key("0.2.9.dev1")
    refuse(version_key, "0.02.1")
print("PASS automatic version selection, changelog, stable tags, reruns and no publication during validation")

repository_version = Path(sys.argv[1]).read_text().strip()
version_key(repository_version)
assert release_notes(Path(sys.argv[2]).parent, repository_version)
print("PASS checked-in VERSION has matching release notes")
