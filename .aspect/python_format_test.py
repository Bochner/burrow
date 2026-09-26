"""Exercise nonmutating check and explicit workspace write with pinned Ruff."""

import ast
from pathlib import Path
import subprocess
import sys
import tempfile


with tempfile.TemporaryDirectory() as directory:
    root = Path(directory)
    workspace = root / "workspace"
    workspace.mkdir()
    source = root / "probe.py"
    original = "#!/usr/bin/env python3\nvalue={ 'x':1}\nassert value['x']==1,'failure'\n"
    source.write_text(original)
    checkout = workspace / source.name
    checkout.write_text(original)
    checkout.chmod(0o755)
    command = [
        sys.executable,
        str(Path(__file__).with_name("python_format.py")),
        str(Path(sys.argv[1]).resolve()),
        str(Path(sys.argv[2]).resolve()),
        source.name,
    ]
    checked = subprocess.run(command, cwd=root, capture_output=True, text=True)
    assert checked.returncode == 1, checked
    assert source.name in checked.stdout, checked.stdout
    assert source.read_text() == checkout.read_text() == original
    subprocess.run(command + ["--write", str(workspace)], cwd=root, check=True)
    assert source.read_text() == original, "write changed the runfile instead of the checkout"
    assert checkout.read_text() == '#!/usr/bin/env python3\nvalue = {"x": 1}\nassert value["x"] == 1, "failure"\n'
    assert checkout.stat().st_mode & 0o777 == 0o755
    assert ast.dump(ast.parse(checkout.read_text())) == ast.dump(ast.parse(original))
    subprocess.run(command, cwd=workspace, check=True)
    checkout.write_text("def broken(\n")
    assert subprocess.run(command, cwd=workspace, capture_output=True).returncode != 0
