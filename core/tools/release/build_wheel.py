"""Build Burrow's Linux wheel from the declared Go binary, without a Python SDK."""

# Adapted from Hovel core/tools/release/build_hovel_wheel.py at 541e78ada0af.
# Copyright 2026 William Born; Apache-2.0. See UPSTREAM.md for modifications.
import argparse
import base64
import csv
import hashlib
import io
from pathlib import Path
import re
import stat
import zipfile

LAUNCHER = '''"""Execute the packaged Go application with its arguments and terminal intact."""
import os
from pathlib import Path
import sys


def main():
    binary = str(Path(__file__).with_name("bin") / "burrow")
    os.execv(binary, [binary, *sys.argv[1:]])


if __name__ == "__main__":
    main()
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path)
    parser.add_argument("version_file", type=Path)
    parser.add_argument("license", type=Path)
    parser.add_argument("--out-dir", type=Path, required=True)
    args = parser.parse_args()
    version = args.version_file.read_text().strip()
    if not re.fullmatch(r"\d+\.\d+\.\d+(?:\.dev\d+)?", version):
        parser.error("VERSION must be X.Y.Z or X.Y.Z.devN")
    tag = "py3-none-manylinux_2_28_x86_64"
    info = f"burrow_ssh-{version}.dist-info"
    files = {
        "burrow_ssh/__init__.py": (f'__version__ = "{version}"\n'.encode(), 0o644),
        "burrow_ssh/__main__.py": (LAUNCHER.encode(), 0o644),
        "burrow_ssh/bin/burrow": (args.binary.read_bytes(), 0o755),
        f"{info}/licenses/LICENSE-HOVEL": (args.license.read_bytes(), 0o644),
        f"{info}/METADATA": (
            f"Metadata-Version: 2.4\nName: burrow-ssh\nVersion: {version}\n"
            "Summary: Burrow SSH workspace manager\nRequires-Python: >=3.10\n"
            "License-File: LICENSE-HOVEL\n"
            "Project-URL: Repository, https://github.com/Bochner/burrow\n"
            "Project-URL: Documentation, https://bochner.github.io/burrow/\n"
            "\nLinux amd64 SSH manager with pinned, managed Hovel.\n".encode(),
            0o644,
        ),
        f"{info}/WHEEL": (
            f"Wheel-Version: 1.0\nGenerator: burrow-release\nRoot-Is-Purelib: false\nTag: {tag}\n".encode(),
            0o644,
        ),
        f"{info}/entry_points.txt": (b"[console_scripts]\nburrow = burrow_ssh.__main__:main\n", 0o644),
    }
    # Embedded skills and the canonical Hovel module manifest already travel
    # inside the Go executable; no second copy or install-time Go build.
    rows = []
    for name, (data, _) in files.items():
        digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).rstrip(b"=").decode()
        rows.append([name, "sha256=" + digest, str(len(data))])
    rows.append([f"{info}/RECORD", "", ""])
    record = io.StringIO()
    csv.writer(record, lineterminator="\n").writerows(rows)
    files[f"{info}/RECORD"] = (record.getvalue().encode(), 0o644)
    args.out_dir.mkdir(parents=True, exist_ok=True)
    wheel = args.out_dir / f"burrow_ssh-{version}-{tag}.whl"
    with zipfile.ZipFile(wheel, "w") as archive:
        for name, (data, mode) in files.items():
            entry = zipfile.ZipInfo(name, (1980, 1, 1, 0, 0, 0))
            entry.external_attr = (stat.S_IFREG | mode) << 16
            entry.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(entry, data)
    print(wheel)


if __name__ == "__main__":
    main()
