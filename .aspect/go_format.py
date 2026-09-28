"""Check declared production sources, or format their workspace originals."""

import argparse
from pathlib import Path
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("gofmt", type=Path)
    parser.add_argument("sources", nargs="+", type=Path)
    parser.add_argument("--write", type=Path, metavar="WORKSPACE")
    args = parser.parse_args()
    sources = [args.write / path if args.write else path for path in args.sources]
    result = subprocess.run(
        [str(args.gofmt.resolve()), "-w" if args.write else "-l", *map(str, sources)],
        capture_output=True,
        text=True,
    )
    if result.stdout:
        print(result.stdout, end="")
    if result.stderr:
        print(result.stderr, end="")
    if result.returncode:
        return result.returncode
    if not args.write and result.stdout:
        print("Run aspect burrow format to fix production Go formatting.")
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
