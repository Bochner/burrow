"""Validate an explicit release source and verify the published wheel digest."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import time
import urllib.error
import urllib.request


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["validate", "verify-pypi"])
    parser.add_argument("--tag", default="")
    parser.add_argument("--publish", choices=["true", "false"], default="false")
    args = parser.parse_args()
    root = Path(os.environ["BUILD_WORKSPACE_DIRECTORY"])
    version = (root / "VERSION").read_text().strip()
    if not re.fullmatch(r"\d+\.\d+\.\d+(?:\.dev\d+)?", version):
        parser.error("VERSION must be X.Y.Z or X.Y.Z.devN")

    def git(*arguments):
        return subprocess.check_output(["git", "-C", str(root), *arguments], text=True).strip()

    if args.mode == "validate":
        commit = git("rev-parse", "HEAD")
        if args.publish == "true" and not args.tag:
            parser.error("publishing requires an existing v-prefixed tag")
        if args.tag:
            if args.tag != "v" + version:
                parser.error("release tag must match VERSION exactly")
            if git("rev-parse", f"refs/tags/{args.tag}^{{commit}}") != commit:
                parser.error("tag does not identify this checkout")
        if args.publish == "true":
            if git("status", "--porcelain"):
                parser.error("publishing requires a clean checkout")
            subprocess.run(["git", "-C", str(root), "merge-base", "--is-ancestor", commit, "origin/main"], check=True)
        if output := os.environ.get("GITHUB_OUTPUT"):
            with open(output, "a") as stream:
                stream.write(f"commit={commit}\n")
        print(f"Validated {'publication' if args.publish == 'true' else 'build-only rehearsal'}: {version} at {commit}")
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
