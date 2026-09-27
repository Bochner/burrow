"""Check/lint declared Python sources, or format their workspace originals."""

import argparse
from pathlib import Path
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("ruff", type=Path)
    parser.add_argument("config", type=Path)
    parser.add_argument("sources", nargs="+", type=Path)
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--write", type=Path, metavar="WORKSPACE")
    mode.add_argument("--lint", action="store_true")
    args = parser.parse_args()
    sources = [args.write / path if args.write else path for path in args.sources]
    return subprocess.run(
        [
            str(args.ruff.resolve()),
            "check" if args.lint else "format",
            "--no-cache",
            "--output-format",
            "concise",
            "--config",
            str(args.config.resolve()),
        ]
        + ([] if args.write or args.lint else ["--check"])
        + list(map(str, sources)),
    ).returncode


if __name__ == "__main__":
    raise SystemExit(main())
