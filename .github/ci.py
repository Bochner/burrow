"""Select verified evidence through GitHub; never turn a cache miss into a pass."""

import argparse
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import subprocess
import tempfile
import urllib.error
import urllib.parse
import urllib.request
import zipfile

from report import SUITES, documentation_path, reusable_source, snapshot, write_json


def api(path):
    request = urllib.request.Request(
        "https://api.github.com/repos/" + os.environ["GITHUB_REPOSITORY"] + "/" + path,
        headers={"Authorization": "Bearer " + os.environ["GH_TOKEN"], "Accept": "application/vnd.github+json"},
    )
    with urllib.request.urlopen(request, timeout=10) as response:
        return json.load(response)


def download(run, name, destination):
    artifacts = api(f"actions/runs/{run['id']}/artifacts?per_page=100")["artifacts"]
    matches = [a for a in artifacts if a["name"] == name and not a["expired"]]
    if len(matches) != 1 or not re.fullmatch(r"sha256:[a-f0-9]{64}", matches[0].get("digest", "")):
        raise ValueError("missing, ambiguous or unverifiable artifact: " + name)
    artifact = matches[0]

    # Obtain the signed URL without forwarding the API token to blob storage.
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            return None

    url = f"https://api.github.com/repos/{os.environ['GITHUB_REPOSITORY']}/actions/artifacts/{artifact['id']}/zip"
    request = urllib.request.Request(url, headers={"Authorization": "Bearer " + os.environ["GH_TOKEN"]})
    try:
        urllib.request.build_opener(NoRedirect).open(request, timeout=10)
    except urllib.error.HTTPError as response:
        if response.code != 302:
            raise
        url = response.headers["Location"]
    else:
        raise ValueError("artifact API did not return a signed download URL")
    if urllib.parse.urlsplit(url).scheme != "https":
        raise ValueError("insecure artifact redirect")
    with urllib.request.urlopen(url, timeout=30) as response:
        content = response.read(512 * 1024 * 1024 + 1)
    unpack(content, artifact["digest"], destination)
    return artifact["digest"]


def unpack(content, expected, destination):
    if len(content) > 512 * 1024 * 1024 or "sha256:" + hashlib.sha256(content).hexdigest() != expected:
        raise ValueError("artifact digest mismatch or archive too large")
    with zipfile.ZipFile(io.BytesIO(content)) as archive:
        names = set()
        if sum(info.file_size for info in archive.infolist()) > 1024 * 1024 * 1024:
            raise ValueError("expanded artifact too large")
        for info in archive.infolist():
            path = PurePosixPath(info.filename)
            if (
                path.is_absolute()
                or ".." in path.parts
                or "\\" in info.filename
                or stat.S_ISLNK(info.external_attr >> 16)
                or info.filename in names
            ):
                raise ValueError("unsafe artifact path")
            names.add(info.filename)
        archive.extractall(destination)


def successful_runs(commit, event):
    if not re.fullmatch(r"[a-f0-9]{40}", commit):
        raise ValueError("invalid source revision")
    query = urllib.parse.urlencode({"head_sha": commit, "event": event, "status": "success", "per_page": 3})
    return api("actions/workflows/ci.yml/runs?" + query)["workflow_runs"]


def eligible(run, commit, event):
    return (
        run["conclusion"] == "success"
        and run["status"] == "completed"
        and run["event"] == event
        and run["head_sha"] == commit
        and run["path"] == ".github/workflows/ci.yml"
        and run["head_repository"]["full_name"] == os.environ["GITHUB_REPOSITORY"]
        and str(run["id"]) != os.environ.get("GITHUB_RUN_ID")
        and (event == "pull_request" or run["head_branch"] == "main")
    )


def verify_run(run, commit, event):
    if not eligible(run, commit, event):
        raise ValueError("ineligible validation run")
    jobs = api(f"actions/runs/{run['id']}/attempts/{run['run_attempt']}/jobs?per_page=100")["jobs"]
    gates = [job for job in jobs if job["name"] == "repository"]
    if len(gates) != 1 or gates[0]["conclusion"] != "success":
        raise ValueError("required repository gate did not pass")


def documentation_changes(root, base):
    changes = subprocess.check_output(
        ["git", "-C", str(root), "diff", "--name-status", "--no-renames", "-z", base, "HEAD"], text=True
    ).split("\0")[:-1]
    return bool(changes) and all(
        status in ("A", "M") and documentation_path(name) for status, name in zip(changes[::2], changes[1::2])
    )


def import_evidence(root, site, run, mode, current, artifact_digest):
    receipt = json.loads((site / "ci.json").read_text())
    basis = receipt["source"]
    if (
        receipt["schemaVersion"] != 1
        or receipt["run"] != {"id": str(run["id"]), "attempt": str(run["run_attempt"])}
        or not reusable_source(basis, current, documentation=mode == "docs")
    ):
        raise ValueError("CI artifact does not prove the selected source")
    commit = api("git/commits/" + basis["commit"])
    if commit["tree"]["sha"] != basis["gitTree"]:
        raise ValueError("reported tree differs from GitHub source")
    if run["event"] == "pull_request":
        parents = [parent["sha"] for parent in commit["parents"]]
        actual = subprocess.check_output(
            ["git", "-C", str(root), "show", "-s", "--format=%P", "HEAD"], text=True
        ).split()
        if parents != actual:
            raise ValueError("PR tested a different merge base or head")
    elif basis["commit"] != run["head_sha"]:
        raise ValueError("main report does not belong to run revision")
    reuse = {
        "mode": mode,
        "validation": receipt["validation"],
        "basisRunID": str(run["id"]),
        "basisRunAttempt": str(run["run_attempt"]),
        "artifactDigest": artifact_digest,
    }
    # Original suite files/logs are copied byte-for-byte. Reports revalidate them.
    evidence = root / ".report-input"
    for suite in (*SUITES, "coverage"):
        shutil.copytree(site / "reports" / suite, evidence / suite)
    if mode == "reuse" and (site / "reports/documentation").is_dir():
        shutil.copytree(site / "reports/documentation", evidence / "documentation")
    write_json(evidence / "reuse.json", reuse)


def stamp(root):
    source = snapshot(root)
    source.pop("files")
    if source["dirty"]:
        raise ValueError("CI artifact preparation requires a clean source checkout")
    run = {"id": os.environ.get("GITHUB_RUN_ID", ""), "attempt": os.environ.get("GITHUB_RUN_ATTEMPT", "")}
    reuse = root / ".report-input/reuse.json"
    validation = json.loads(reuse.read_text())["validation"] if reuse.exists() else {"source": source, "run": run}
    write_json(root / "_site/ci.json", {"schemaVersion": 1, "source": source, "run": run, "validation": validation})


def plan(root, event, event_name, force_full=False):
    current = snapshot(root)
    candidates = []
    if force_full or current["dirty"]:
        return "full"
    try:
        if event_name == "pull_request":
            pr = event["pull_request"]
            if pr["base"]["ref"] != "main" or not documentation_changes(root, pr["base"]["sha"]):
                return "full"
            candidates = [(r, pr["base"]["sha"], "push", "docs") for r in successful_runs(pr["base"]["sha"], "push")]
        elif event_name == "push" and event["ref"] == "refs/heads/main":
            if event.get("forced") or event["after"] != current["commit"]:
                return "full"
            # A repeated push can reuse an already completed main run as well.
            candidates = [(r, current["commit"], "push", "reuse") for r in successful_runs(current["commit"], "push")]
            pulls = api(f"commits/{current['commit']}/pulls?per_page=100")
            for pr in pulls:
                if (
                    pr["merged_at"]
                    and pr["merge_commit_sha"] == current["commit"]
                    and pr["base"]["ref"] == "main"
                    and (pr["head"].get("repo") or {}).get("full_name") == os.environ["GITHUB_REPOSITORY"]
                ):
                    candidates += [
                        (r, pr["head"]["sha"], "pull_request", "reuse")
                        for r in successful_runs(pr["head"]["sha"], "pull_request")
                    ]
        # Bound lookup work; older/unavailable evidence simply means fresh checks.
        for run, commit, trigger, mode in candidates[:3]:
            try:
                verify_run(run, commit, trigger)
                with tempfile.TemporaryDirectory() as temporary:
                    site = Path(temporary)
                    artifact_digest = download(run, "docs-site", site)
                    import_evidence(root, site, run, mode, current, artifact_digest)
                print(f"Reuse {run['html_url']} ({mode}); original execution identity preserved")
                return mode
            except (ValueError, KeyError, TypeError, AttributeError, OSError, zipfile.BadZipFile) as error:
                # No source evidence is accepted on errors. Fresh jobs replace it.
                for suite in (*SUITES, "coverage", "documentation", "reuse.json"):
                    path = root / ".report-input" / suite
                    if path.is_dir():
                        shutil.rmtree(path)
                    else:
                        path.unlink(missing_ok=True)
                print(f"Cannot reuse run {run['id']}: {error}")
    except (ValueError, KeyError, TypeError, AttributeError, OSError) as error:
        print(f"Evidence lookup unavailable: {error}")
    print("Run full verification: no eligible evidence")
    return "full"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("plan", "stamp"))
    parser.add_argument("--force-full", action="store_true")
    args = parser.parse_args()
    root = Path(os.environ["BUILD_WORKSPACE_DIRECTORY"])
    if args.mode == "stamp":
        stamp(root)
        return
    mode = plan(
        root,
        json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text()),
        os.environ["GITHUB_EVENT_NAME"],
        args.force_full,
    )
    if output := os.environ.get("GITHUB_OUTPUT"):
        with open(output, "a") as stream:
            stream.write("mode=" + mode + "\n")


if __name__ == "__main__":
    main()
