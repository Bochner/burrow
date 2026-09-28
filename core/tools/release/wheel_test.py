"""Install the real wheel through pinned pipx outside the checkout, entirely offline."""

import base64
import csv
import hashlib
import io
import json
import os
from pathlib import Path
import signal
import struct
import subprocess
import sys
import tarfile
import tempfile
import zipfile

binary, version_file, license_file, pipx, hovel = [str(Path(p).resolve()) for p in sys.argv[1:6]]
artifact_dir = Path(sys.argv[6]).resolve() if len(sys.argv) == 7 else None
builder = str(Path(__file__).with_name("build_wheel.py"))
with tempfile.TemporaryDirectory(prefix="burrow-wheel-") as temporary:
    root = Path(temporary)
    env = {key: value for key, value in os.environ.items() if not key.startswith(("PIP", "HOVEL_", "PYTHON"))}
    env.update(
        HOME=str(root),
        XDG_DATA_HOME=str(root / "data"),
        XDG_CACHE_HOME=str(root / "cache"),
        XDG_CONFIG_HOME=str(root / "config"),
        PIPX_HOME=str(root / "pipx"),
        PIPX_BIN_DIR=str(root / "bin"),
        PIPX_MAN_DIR=str(root / "man"),
        PIP_NO_INDEX="1",
        PIP_CONFIG_FILE=os.devnull,
        PIP_DISABLE_PIP_VERSION_CHECK="1",
        NO_COLOR="1",
    )

    def run(*args, ok=True):
        result = subprocess.run(list(map(str, args)), cwd=root, env=env, capture_output=True, text=True, timeout=90)
        assert (result.returncode == 0) == ok, (args, result.stdout, result.stderr)
        return result

    def build(version, destination):
        run(sys.executable, builder, binary, version, license_file, "--out-dir", destination)
        return next(destination.glob("*.whl"))

    version = Path(version_file).read_text().strip()
    wheel = (
        (artifact_dir / f"burrow_ssh-{version}-py3-none-manylinux_2_28_x86_64.whl")
        if artifact_dir
        else build(version_file, root / "dist")
    )
    if artifact_dir:
        package = artifact_dir / f"burrow-v{version}-linux-amd64.tar.gz"
        unpacked = root / "archive"
        with tarfile.open(package) as archive:
            archive.extractall(unpacked, filter="data")
        packaged = unpacked / "burrow"
        assert json.loads(run(packaged, "capabilities").stdout)["schemaVersion"] == 1
        assert run(packaged, "--help").returncode == 0
        assert json.loads(run(packaged, "agent", "install", "codex", "--scope", "project").stdout)["complete"]
    executable = Path(binary).read_bytes()
    assert executable[:6] == b"\x7fELF\x02\x01" and struct.unpack_from("<H", executable, 18)[0] == 62
    offset = struct.unpack_from("<Q", executable, 32)[0]
    size, count = struct.unpack_from("<HH", executable, 54)
    assert all(struct.unpack_from("<I", executable, offset + index * size)[0] != 3 for index in range(count)), (
        "release binary needs a dynamic loader"
    )
    repeated = build(version_file, root / "repeat")
    assert wheel.read_bytes() == repeated.read_bytes(), "wheel build is not deterministic"
    with zipfile.ZipFile(wheel) as archive:
        names = archive.namelist()
        record = next(name for name in names if name.endswith("/RECORD"))
        rows = list(csv.reader(io.StringIO(archive.read(record).decode())))
        assert {row[0] for row in rows} == set(names)
        for name, digest, size in rows:
            if name == record:
                assert digest == size == ""
                continue
            content = archive.read(name)
            assert int(size) == len(content)
            assert (
                digest == "sha256=" + base64.urlsafe_b64encode(hashlib.sha256(content).digest()).rstrip(b"=").decode()
            )
        assert archive.getinfo("burrow_ssh/bin/burrow").external_attr >> 16 & 0o777 == 0o755
        metadata = archive.read(next(name for name in names if name.endswith("/METADATA"))).decode()
        assert "Name: burrow-ssh\n" in metadata and "Requires-Dist:" not in metadata

    pipx_command = [sys.executable, pipx]
    run(*pipx_command, "install", "--skip-maintenance", "--backend", "pip", "--python", sys.executable, wheel)
    command = root / "bin/burrow"
    assert command.exists()
    assert json.loads(run(command, "capabilities").stdout)["schemaVersion"] == 1
    assert run(command, "--help").returncode == 0
    assert run(command, "--definitely-invalid", ok=False).returncode != 0
    installed = json.loads(run(command, "agent", "install", "codex", "--scope", "project").stdout)
    assert installed["complete"] and installed["skills"]
    evidence = root / "w/evidence.txt"
    daemon = None
    try:
        info = json.loads(run(command, "--workspace", root / "w", "--hovel-package", hovel, "workspace", "open").stdout)
        daemon = info["pid"]
        evidence.write_text("preserve workspace across pipx upgrade and uninstall")
        # Exercise the cached, self-copied module from the installed Go executable.
        assert json.loads(run(command, "--workspace", root / "w", "profiles").stdout) is not None
        upgraded_version = root / "VERSION"
        upgraded_version.write_text("999.0.0\n")
        upgraded = build(upgraded_version, root / "upgrade")
        run(*pipx_command, "install", "--skip-maintenance", "--backend", "pip", "--upgrade", upgraded)
        assert (
            json.loads(run(command, "--workspace", root / "w", "--offline", "workspace", "open").stdout)["pid"]
            == daemon
        )
        run(*pipx_command, "uninstall", "burrow-ssh")
        assert not command.exists()
        assert evidence.read_text() == "preserve workspace across pipx upgrade and uninstall"
        assert (root / "cache/burrow/hovel/0.4.4/hovel").exists()
        print("PASS deterministic wheel, offline pipx install/upgrade/uninstall, CLI, embedded skills and pinned Hovel")
        if artifact_dir:
            (artifact_dir / ".smoke.json").write_text(
                json.dumps({file.name: hashlib.sha256(file.read_bytes()).hexdigest() for file in (wheel, package)})
            )
    finally:
        if daemon:
            try:
                os.kill(daemon, signal.SIGTERM)
            except ProcessLookupError:
                pass
