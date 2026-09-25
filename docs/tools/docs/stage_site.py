"""Materialize the declared Astro artifact, following Hovel's docs convention."""
import os
import shutil
import sys
from pathlib import Path
from report import snapshot, write_json

# Copied from Hovel c461ba; Copyright 2026 William Born; Apache-2.0.
# See docs/site/UPSTREAM.md. Aspect runs from the workspace, not the runfiles root.
def resolve_runfile(raw: str) -> Path:
    path = Path(raw)
    if path.is_absolute() and path.exists():
        return path.resolve()
    for root_name in ("RUNFILES_DIR", "TEST_SRCDIR"):
        root = os.environ.get(root_name)
        if not root:
            continue
        for prefix in ("", "_main", "burrow"):
            candidate = Path(root) / prefix / raw
            if candidate.exists():
                return candidate.resolve()
    candidate = Path.cwd() / raw
    if candidate.exists():
        return candidate.resolve()
    raise SystemExit(f"missing assembled site runfile: {raw}")

source = resolve_runfile(sys.argv[1])
workspace = Path(os.environ["BUILD_WORKSPACE_DIRECTORY"]).resolve()
destination = workspace / "_site"
source_identity = snapshot(workspace)
if destination.is_symlink():
    raise SystemExit("Refusing to stage through a symlink at _site")
if destination.exists():
    # copytree preserves Bazel's read-only directory modes; unlink needs write access.
    for root, _, _ in os.walk(destination, followlinks=False):
        directory = Path(root)
        directory.chmod(directory.stat().st_mode | 0o200)
    shutil.rmtree(destination)
shutil.copytree(source, destination, copy_function=shutil.copyfile, ignore=shutil.ignore_patterns(".astro-cache"))
for directory, _, _ in os.walk(destination):
    path = Path(directory)
    path.chmod(path.stat().st_mode | 0o200)
if snapshot(workspace) != source_identity:
    raise SystemExit("Source changed while staging the site")
write_json(destination / ".source.json", source_identity)
print(f"Documentation staged at {destination}")
