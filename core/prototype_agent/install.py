"""Disposable local package/install proof; never a production installer.

Layout follows Hovel agent/tools/package_agent.py at c461ba282a8aecc7aa3a079a4613bf5e2640c388.
Native clients own plugin installation. This proof owns only its package tree.
"""
import json
from pathlib import Path
import shutil
import argparse
import subprocess


def write_json(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2) + "\n")


def stage(root, host, version):
    if root.exists():
        raise FileExistsError(root)
    if host not in ("claude", "codex", "opencode") or version not in ("0.0.1", "0.0.2"):
        raise ValueError("unsupported fixture host/version")
    root.mkdir(parents=True)
    write_json(root / "burrow-agent.json", {
        "name": "burrow", "version": version, "host": host,
        "hovelSource": "c461ba282a8aecc7aa3a079a4613bf5e2640c388",
    })
    if host == "opencode":
        plugin = root / ".opencode"
    else:
        plugin = root / "plugins/burrow"
    shutil.copytree(Path(__file__).parent / "agent/skills", plugin / "skills")
    if host == "opencode":
        return plugin
    manifest = {"name": "burrow", "version": version,
                "description": "Disposable Burrow tunnel operator proof"}
    if host == "codex":
        manifest["skills"] = "./skills/"
        write_json(plugin / ".codex-plugin/plugin.json", manifest)
        marketplace = root / ".agents/plugins/marketplace.json"
        entry = {"name": "burrow", "source": {"source": "local", "path": "./plugins/burrow"},
                 "policy": {"installation": "AVAILABLE", "authentication": "ON_INSTALL"},
                 "category": "Productivity"}
        body = {"name": "bochner-burrow", "interface": {"displayName": "Burrow"}, "plugins": [entry]}
    else:
        write_json(plugin / ".claude-plugin/plugin.json", manifest)
        marketplace = root / ".claude-plugin/marketplace.json"
        body = {"name": "bochner-burrow", "owner": {"name": "Bochner"},
                "plugins": [{"name": "burrow", "source": "./plugins/burrow"}]}
    write_json(marketplace, body)
    return plugin


def install_skills(source, destination):
    """Mirror Hovel's identical-content no-op and conflict refusal, without MCP."""
    if any(p.is_symlink() for p in (destination, *destination.parents)):
        raise ValueError("skill destination contains a symlink")
    for skill in sorted(source.iterdir()):
        target = destination / skill.name
        if target.exists() or target.is_symlink():
            if target.is_symlink() or any(p.is_symlink() for p in target.rglob("*")):
                raise ValueError("installed skill contains a symlink")
            old = sorted(p.relative_to(target) for p in target.rglob("*") if p.is_file())
            new = sorted(p.relative_to(skill) for p in skill.rglob("*") if p.is_file())
            if old != new or any((target / p).read_bytes() != (skill / p).read_bytes() for p in new):
                raise FileExistsError("existing skill differs; preserve it for owner review: " + str(target))
        else:
            shutil.copytree(skill, target)


def main():
    parser = argparse.ArgumentParser(description="Disposable Burrow agent installer; local packages only")
    sub = parser.add_subparsers(dest="command", required=True)
    agent = sub.add_parser("agent").add_subparsers(dest="action", required=True)
    install = agent.add_parser("install")
    install.add_argument("host", choices=("claude", "codex", "opencode"))
    install.add_argument("--scope", choices=("user", "project"), default="user")
    install.add_argument("--source", type=Path, required=True)
    install.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    source = args.source.resolve(strict=True)
    metadata = json.loads((source / "burrow-agent.json").read_text())
    if metadata.get("name") != "burrow" or metadata.get("host") != args.host:
        parser.error("package identity/host mismatch")
    commands = []
    if args.host == "claude":
        commands = [["claude", "plugin", "marketplace", "add", str(source), "--scope", args.scope],
                    ["claude", "plugin", "install", "burrow@bochner-burrow", "--scope", args.scope]]
    elif args.host == "codex" and args.scope == "user":
        commands = [["codex", "plugin", "marketplace", "add", str(source)],
                    ["codex", "plugin", "add", "burrow@bochner-burrow"]]
    else:
        skills = source / (".opencode/skills" if args.host == "opencode" else "plugins/burrow/skills")
        if args.host == "codex":
            destination = Path.cwd() / ".agents/skills"
        elif args.scope == "project":
            destination = Path.cwd() / ".opencode/skills"
        else:
            destination = Path.home() / ".config/opencode/skills"
        print("Install skills:", skills, "->", destination)
        if not args.dry_run:
            install_skills(skills, destination)
    for command in commands:
        print(command)
        if not args.dry_run:
            subprocess.run(command, check=True)


if __name__ == "__main__":
    main()
