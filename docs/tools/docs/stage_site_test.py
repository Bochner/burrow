"""Exercise repeat staging of read-only build output through the actual script."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile

script = Path(sys.argv[1]).resolve()
with tempfile.TemporaryDirectory() as scratch:
    root = Path(scratch)
    source = root / "artifact"
    (source / "assets").mkdir(parents=True)
    (source / "assets" / "site.css").write_text("body {}")
    (source / ".astro-cache").mkdir()
    workspace = root / "workspace"
    workspace.mkdir()
    subprocess.run(["git", "init", "-q", str(workspace)], check=True)
    (workspace / ".gitignore").write_text("/_site/\n/previous/\n")
    subprocess.run(["git", "-C", str(workspace), "add", ".gitignore"], check=True)
    subprocess.run(["git", "-C", str(workspace), "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture"], check=True)
    for directory in (source, source / "assets", source / ".astro-cache"):
        directory.chmod(0o555)

    def stage():
        return subprocess.run([sys.executable, str(script), str(source)],
                              env=os.environ | {"BUILD_WORKSPACE_DIRECTORY": str(workspace), "PYTHONPATH": str(Path(sys.argv[1]).absolute().parent)},
                              capture_output=True, text=True, timeout=10)

    result = stage()
    assert result.returncode == 0, result.stderr
    destination = workspace / "_site"
    assert not (destination / ".astro-cache").exists()
    # Model stale content and a nested symlink in a previous read-only staging tree.
    destination.chmod(0o755)
    (destination / "obsolete.html").write_text("stale")
    (destination / "external").symlink_to(source, target_is_directory=True)
    destination.chmod(0o555)
    result = stage()
    assert result.returncode == 0, result.stderr
    assert (destination / "assets" / "site.css").read_text() == "body {}"
    assert not (destination / "obsolete.html").exists()
    assert not (destination / "external").exists()
    assert source.stat().st_mode & 0o777 == 0o555, "staging changed source permissions"
    # Keep the existing guard against staging through a top-level symlink.
    destination.rename(workspace / "previous")
    destination.symlink_to(source, target_is_directory=True)
    result = stage()
    assert result.returncode != 0 and "Refusing to stage through a symlink" in result.stderr
    assert (source / "assets" / "site.css").read_text() == "body {}"
