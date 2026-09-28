"""Prepare, verify and retrieve the exact tested distribution for automatic or manual release."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import shutil
import tempfile
import time
import urllib.error
import urllib.request


def artifact_names(version):
    return {f"burrow-v{version}-linux-amd64.tar.gz", f"burrow_ssh-{version}-py3-none-manylinux_2_28_x86_64.whl"}


def checksum(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def sums(directory):
    return "".join(
        f"{checksum(directory / name)}  {name}\n"
        for name in sorted(p.name for p in directory.iterdir() if p.name != "SHA256SUMS")
    )


def manifest(root, version):
    from report import snapshot, write_json

    directory = root / "dist"
    files = {name: checksum(directory / name) for name in artifact_names(version)}
    if json.loads((directory / ".smoke.json").read_text()) != files:
        raise ValueError("bundle differs from the artifacts that passed smoke checks")
    source = snapshot(root)
    source.pop("files")
    write_json(
        directory / "bundle.json",
        {
            "schemaVersion": 1,
            "version": version,
            "source": source,
            "run": {"id": os.environ.get("GITHUB_RUN_ID", ""), "attempt": os.environ.get("GITHUB_RUN_ATTEMPT", "")},
            "files": files,
            "smoke": "passed",
        },
    )
    (directory / ".smoke.json").unlink()
    (directory / "SHA256SUMS").write_text(sums(directory))


def verify_bundle(directory, version, source, run=None):
    expected = artifact_names(version)
    if {path.name for path in directory.iterdir()} != expected | {"bundle.json", "SHA256SUMS"}:
        raise ValueError("release bundle is missing files or contains unexpected files")
    receipt = json.loads((directory / "bundle.json").read_text())
    if (
        receipt["schemaVersion"] != 1
        or receipt["version"] != version
        or receipt["smoke"] != "passed"
        or receipt["source"]["dirty"]
        or source["dirty"]
        or any(receipt["source"][key] != source[key] for key in ("commit", "gitTree"))
        or receipt["files"] != {name: checksum(directory / name) for name in expected}
        or (directory / "SHA256SUMS").read_text() != sums(directory)
    ):
        raise ValueError("release bundle source, smoke evidence or checksum mismatch")
    if run and receipt["run"] != {"id": str(run["id"]), "attempt": str(run["run_attempt"])}:
        raise ValueError("release bundle belongs to another CI run")


def fetch_bundle(root, version, run_id=None):
    from ci import api, successful_runs, verify_run, download
    from report import snapshot

    source = snapshot(root)
    runs = (
        [api("actions/runs/" + run_id)]
        if run_id
        else (run for event in ("push", "workflow_dispatch") for run in successful_runs(source["commit"], event))
    )
    for run in runs:
        event = run["event"]
        if event not in ("push", "workflow_dispatch"):
            raise ValueError("release bundle must come from main validation")
        try:
            verify_run(run, source["commit"], event)
            with tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                download(run, "release-bundle", directory)
                verify_bundle(directory, version, source, run)
                destination = root / "dist"
                if destination.exists() and any(destination.iterdir()):
                    raise ValueError("dist must be empty before fetching a release bundle")
                shutil.copytree(directory, destination, dirs_exist_ok=True)
            print("Using tested release bundle from " + run["html_url"])
            return
        except (ValueError, KeyError, OSError) as error:
            print(f"Cannot use run {run['id']}: {error}")
    raise ValueError(
        "No verified bundle for this commit. If it is still main HEAD, rerun Repository with force_full; otherwise prepare a new release revision on main."
    )


def version_key(version):
    match = re.fullmatch(r"(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:\.dev(0|[1-9]\d*))?", version)
    if not match:
        raise ValueError("VERSION must be X.Y.Z or X.Y.Z.devN")
    major, minor, patch, development = match.groups()
    return (int(major), int(minor), int(patch), development is None, int(development or 0))


def release_notes(root, version):
    entries = re.findall(
        r"^## \[" + re.escape(version) + r"\](?: - \d{4}-\d{2}-\d{2})?\n(.*?)(?=^## |\Z)",
        (root / "CHANGELOG.md").read_text(),
        re.MULTILINE | re.DOTALL,
    )
    if len(entries) != 1 or not entries[0].strip():
        raise ValueError("CHANGELOG.md needs one nonempty entry for " + version)
    return entries[0].strip() + "\n"


def validate(root, version, tag="", publish=False, automatic=False):
    def git(*arguments):
        return subprocess.check_output(["git", "-C", str(root), *arguments], text=True).strip()

    commit = git("rev-parse", "HEAD")
    if publish or automatic:
        if git("status", "--porcelain"):
            raise ValueError("publishing requires a clean checkout")
        subprocess.run(["git", "-C", str(root), "merge-base", "--is-ancestor", commit, "origin/main"], check=True)
    if tag and tag != "v" + version:
        raise ValueError("release tag must match VERSION exactly")
    if automatic:
        previous = git("show", "HEAD^1:VERSION")
        if previous == version:
            return {"commit": commit, "tag": "", "ready": "false", "prerelease": "false"}
        if version_key(version) <= version_key(previous):
            raise ValueError("VERSION must increase from the previous main revision")
        tag = "v" + version
    if publish and not tag:
        raise ValueError("publishing requires an existing v-prefixed tag or automatic version release")
    if tag:
        existing = subprocess.run(
            ["git", "-C", str(root), "rev-parse", "--verify", "--quiet", f"refs/tags/{tag}^{{commit}}"],
            capture_output=True,
            text=True,
        )
        if existing.returncode == 0:
            if existing.stdout.strip() != commit:
                raise ValueError("tag does not identify this checkout; existing tags are never moved")
        elif not automatic:
            raise ValueError("publishing requires an existing v-prefixed tag")
    if publish or automatic:
        release_notes(root, version)
    return {"commit": commit, "tag": tag, "ready": "true", "prerelease": str(".dev" in version).lower()}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "mode",
        choices=["validate", "stage", "manifest", "fetch", "verify-bundle", "prepare-pypi", "verify-pypi", "notes"],
    )
    parser.add_argument("--tag", default="")
    parser.add_argument("--publish", choices=["true", "false"], default="false")
    parser.add_argument("--automatic", action="store_true")
    parser.add_argument("--run-id")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    root = Path(os.environ["BUILD_WORKSPACE_DIRECTORY"])
    version = (root / "VERSION").read_text().strip()
    version_key(version)

    if args.mode == "stage":
        directory = root / "dist"
        # A stale distribution must not silently join a release upload.
        if any(path.name not in artifact_names(version) for path in directory.iterdir()):
            parser.error("dist contains stale files; preserve or remove them before preparing a new bundle")
        shutil.copyfile(
            root / "bazel-bin/core/cmd/burrow/package.tgz", directory / f"burrow-v{version}-linux-amd64.tar.gz"
        )
        return
    if args.mode == "manifest":
        manifest(root, version)
        return
    if args.mode == "fetch":
        fetch_bundle(root, version, args.run_id)
        return
    if args.mode in ("verify-bundle", "prepare-pypi"):
        from report import snapshot

        verify_bundle(root / "dist", version, snapshot(root))
        if args.mode == "prepare-pypi":
            destination = root / "dist/pypi"
            destination.mkdir()
            wheel = next((root / "dist").glob("*.whl"))
            shutil.copyfile(wheel, destination / wheel.name)
        return

    if args.mode == "notes":
        if not args.output:
            parser.error("notes requires --output")
        args.output.write_text(release_notes(root, version))
        return
    if args.mode == "validate":
        metadata = validate(root, version, args.tag, args.publish == "true", args.automatic)
        if output := os.environ.get("GITHUB_OUTPUT"):
            with open(output, "a") as stream:
                stream.writelines(f"{key}={value}\n" for key, value in metadata.items())
        print(f"Release candidate: {version} at {metadata['commit']}; ready={metadata['ready']}")
        return

    wheel = root / "dist" / f"burrow_ssh-{version}-py3-none-manylinux_2_28_x86_64.whl"
    digest = hashlib.sha256(wheel.read_bytes()).hexdigest()
    for attempt in range(6):
        try:
            with urllib.request.urlopen(f"https://pypi.org/pypi/burrow-ssh/{version}/json", timeout=20) as response:
                metadata = json.load(response)
            matches = [entry for entry in metadata["urls"] if entry["filename"] == wheel.name]
            if matches:
                if len(matches) != 1 or matches[0]["digests"]["sha256"] != digest:
                    raise ValueError("PyPI wheel checksum differs from the tested artifact")
                print(f"Verified PyPI SHA256: {wheel.name} {digest}")
                return
        except urllib.error.HTTPError as error:
            if error.code != 404:
                raise
        if attempt < 5:
            time.sleep(5)
    raise SystemExit("published wheel is not visible on PyPI; verification failed")


if __name__ == "__main__":
    main()
