"""Exercise check/write behavior with the declared pinned formatter."""

from pathlib import Path
import subprocess
import sys
import tempfile


with tempfile.TemporaryDirectory() as directory:
    root = Path(directory)
    source = root / "probe.go"
    original = "package probe\nfunc example( ){println(1)}\n"
    source.write_text(original)
    command = [
        sys.executable,
        str(Path(__file__).with_name("go_format.py")),
        str(Path(sys.argv[1]).resolve()),
        str(source),
    ]
    checked = subprocess.run(command, capture_output=True, text=True)
    assert checked.returncode == 1, checked
    assert str(source) in checked.stdout, checked.stdout
    assert source.read_text() == original
    subprocess.run(command + ["--write", str(root)], check=True)
    assert source.read_text() == "package probe\n\nfunc example() { println(1) }\n"
    subprocess.run(command, check=True)
    source.write_text("package probe\nfunc broken(\n")
    assert subprocess.run(command, capture_output=True).returncode != 0
