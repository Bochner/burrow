"""Exercise evidence selection, GitHub trust checks and archive rejection offline."""

import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
from unittest.mock import patch
import zipfile

import ci


def refuses(fn, *args):
    try:
        fn(*args)
    except (ValueError, KeyError, OSError):
        return
    raise AssertionError("accepted invalid evidence")


with tempfile.TemporaryDirectory() as temporary:
    root = Path(temporary) / "repo"
    root.mkdir()
    site = Path(temporary) / "site"
    os.environ.update(GITHUB_REPOSITORY="owner/repo", GITHUB_RUN_ID="200", GITHUB_RUN_ATTEMPT="1")

    def git(*args, input=None):
        return subprocess.check_output(["git", "-C", str(root), *args], input=input, text=True).strip()

    git("init", "-q")
    git("config", "user.name", "CI fixture")
    git("config", "user.email", "fixture@example.invalid")
    (root / ".gitignore").write_text("/.report-input/\n/_site/\n")
    (root / "README.md").write_text("base docs")
    (root / "app.go").write_text("package app")
    git("add", ".")
    git("commit", "-qm", "base")
    base = git("rev-parse", "HEAD")
    (root / "app.go").write_text("package changed")
    git("commit", "-qam", "feature")
    head = git("rev-parse", "HEAD")
    tree = git("rev-parse", "HEAD^{tree}")
    tested = git("commit-tree", tree, "-p", base, "-p", head, input="PR validation\n")
    merged = git("commit-tree", tree, "-p", base, "-p", head, input="Merged PR\n")
    git("checkout", "-q", tested)
    source = ci.snapshot(root)
    run = dict(
        id=100,
        run_attempt=1,
        head_sha=head,
        event="pull_request",
        conclusion="success",
        status="completed",
        path=".github/workflows/ci.yml",
        head_repository={"full_name": "owner/repo"},
        head_branch="feature",
        html_url="https://example.invalid/100",
    )
    receipt = dict(
        schemaVersion=1,
        source=source,
        run={"id": "100", "attempt": "1"},
        validation={"source": source, "run": {"id": "100", "attempt": "1"}},
    )
    ci.write_json(site / "ci.json", receipt)
    for suite in (*ci.SUITES, "coverage"):
        ci.write_json(site / "reports" / suite / "suite.json", {"original": tested})
    git("checkout", "-q", merged)
    event = dict(ref="refs/heads/main", after=merged)
    parents = [base, head]

    def api(path):
        if path.startswith("actions/runs/"):
            return {"jobs": [{"name": "repository", "conclusion": "success"}]}
        if path.startswith("git/commits/"):
            return {"tree": {"sha": tree}, "parents": [{"sha": p} for p in parents]}
        if path.startswith("commits/"):
            return [
                dict(
                    merged_at="now",
                    merge_commit_sha=merged,
                    base={"ref": "main"},
                    head={"sha": head, "repo": {"full_name": "owner/repo"}},
                )
            ]
        raise AssertionError(path)

    def download(selected, name, destination):
        assert selected == run and name == "docs-site"
        shutil.copytree(site, destination, dirs_exist_ok=True)
        return "sha256:" + "a" * 64

    def clear():
        shutil.rmtree(root / ".report-input", ignore_errors=True)

    with (
        patch.object(ci, "api", side_effect=api),
        patch.object(ci, "download", side_effect=download),
        patch.object(ci, "successful_runs", side_effect=lambda sha, event: [run] if event == "pull_request" else []),
    ):
        assert ci.plan(root, event, "push") == "reuse"
        assert json.loads((root / ".report-input/portable/suite.json").read_text()) == {"original": tested}
        ci.stamp(root)
        stamped = json.loads((root / "_site/ci.json").read_text())
        assert stamped["source"]["commit"] == merged and stamped["validation"]["source"]["commit"] == tested
        clear()
        assert ci.plan(root, event, "push", force_full=True) == "full"
        assert ci.plan(root, event | {"forced": True}, "push") == "full"
        assert ci.plan(root, {}, "workflow_dispatch") == "full"
        for key, bad in [
            ("conclusion", "failure"),
            ("status", "in_progress"),
            ("path", ".github/workflows/other.yml"),
            ("head_repository", {"full_name": "fork/repo"}),
            ("run_attempt", 2),
        ]:
            old = run[key]
            run[key] = bad
            assert ci.plan(root, event, "push") == "full", key
            run[key] = old
        parents[:] = [head]
        assert ci.plan(root, event, "push") == "full"
        parents[:] = [base, head]
        receipt["source"]["gitTree"] = "0" * 40
        ci.write_json(site / "ci.json", receipt)
        assert ci.plan(root, event, "push") == "full"
        receipt["source"]["gitTree"] = tree
        ci.write_json(site / "ci.json", receipt)
        with patch.object(ci, "download", side_effect=ValueError("expired or corrupt")):
            assert ci.plan(root, event, "push") == "full"
        with patch.object(ci, "api", return_value={"jobs": [{"name": "repository", "conclusion": "skipped"}]}):
            refuses(ci.verify_run, run, head, "pull_request")
        with patch.object(ci, "api", side_effect=OSError("unavailable")):
            assert ci.plan(root, event, "push") == "full"

    # Docs reuse only an exact main baseline with identical application inputs.
    baseline = ci.snapshot(root)
    (root / "README.md").write_text("new docs")
    git("commit", "-qam", "docs")
    assert ci.documentation_changes(root, merged)
    assert ci.reusable_source(baseline, ci.snapshot(root), documentation=True)
    assert not ci.reusable_source(baseline, ci.snapshot(root))
    receipt.update(source=baseline)
    ci.write_json(site / "ci.json", receipt)
    run.update(event="push", head_sha=merged, head_branch="main")
    with (
        patch.object(ci, "api", side_effect=api),
        patch.object(ci, "download", side_effect=download),
        patch.object(ci, "successful_runs", return_value=[run]),
    ):
        assert ci.plan(root, {"pull_request": {"base": {"ref": "main", "sha": merged}}}, "pull_request") == "docs"
    clear()
    (root / "README.md").unlink()
    git("commit", "-qam", "delete docs")
    assert not ci.documentation_changes(root, merged)
    (root / "app.go").write_text("package modified")
    assert not ci.reusable_source(baseline, ci.snapshot(root), documentation=True)
    refuses(ci.stamp, root)
    for name in [
        ".github/workflows/ci.yml",
        "AGENTS.md",
        "docs/site/src/layouts/Page.astro",
        "docs/tools/docs/report.py",
        "docs/site/public/logo.svg",
        "MODULE.bazel",
        "docs/research/BUILD.bazel",
    ]:
        assert not ci.documentation_path(name), name

    def archive(name):
        data = io.BytesIO()
        with zipfile.ZipFile(data, "w") as output:
            output.writestr(name, "test")
        return data.getvalue()

    valid = archive("reports/test.txt")
    digest = "sha256:" + hashlib.sha256(valid).hexdigest()
    ci.unpack(valid, digest, Path(temporary) / "unpacked")
    assert (Path(temporary) / "unpacked/reports/test.txt").read_text() == "test"
    refuses(ci.unpack, valid, "sha256:" + "0" * 64, root)
    for name in ["../outside", "/absolute", "reports/../../outside", "reports\\outside"]:
        data = archive(name)
        refuses(ci.unpack, data, "sha256:" + hashlib.sha256(data).hexdigest(), root)
print("PASS same-tree merge reuse, doc selection, clean fallback, original identity and archive trust boundaries")
