"""Throwaway packaging checks in disposable directories, entered through Aspect."""
import json
from pathlib import Path
import tempfile
import shutil
import subprocess
import os
import sys

from core.prototype_agent.install import install_skills, stage


with tempfile.TemporaryDirectory(prefix="burrow-agent-") as temporary:
    root = Path(temporary)
    for host in ("claude", "codex", "opencode"):
        first = stage(root / host / "0.0.1", host, "0.0.1")
        second = stage(root / host / "0.0.2", host, "0.0.2")
        assert (first / "skills/burrow-tunnels/SKILL.md").read_bytes() == (second / "skills/burrow-tunnels/SKILL.md").read_bytes()
        for version in ("0.0.1", "0.0.2"):
            manifest = json.loads((root / host / version / "burrow-agent.json").read_text())
            assert manifest["version"] == version and manifest["host"] == host
        assert not list((root / host).rglob(".mcp.json")), "shared Hovel integration must not be duplicated"
    project = root / "project"
    project.mkdir()
    config = project / "opencode.json"
    sentinel = '{"mcp":{"hovel":{"type":"local","command":["hovel","mcp"]}},"theme":"system"}\n'
    config.write_text(sentinel)
    existing = project / ".opencode/skills/hovel/SKILL.md"
    existing.parent.mkdir(parents=True)
    existing.write_text("unrelated Hovel installation")
    source = root / "opencode/0.0.1/.opencode/skills"
    destination = project / ".opencode/skills"
    install_skills(source, destination)
    install_skills(source, destination)
    install_skills(root / "opencode/0.0.2/.opencode/skills", destination)
    installed = project / ".opencode/skills/burrow-tunnels/SKILL.md"
    installed.write_text("owner-edited skill")
    try:
        install_skills(source, destination)
        raise AssertionError("conflicting skill was overwritten")
    except FileExistsError:
        pass
    assert installed.read_text() == "owner-edited skill"
    assert config.read_text() == sentinel and existing.read_text() == "unrelated Hovel installation"
    print("PASS three host package layouts and version/provenance metadata; portable placement, repeat install, identical-content update and existing Hovel skill/config preservation")

    # Native clients see scratch config mounted at their usual paths. The real
    # config, marketplace and plugin cache stay outside their writable view.
    for host in ("claude", "codex"):
        executable = shutil.which(host)
        if not executable:
            raise RuntimeError("native client prerequisite missing: " + host)
        home = Path.home()
        config = root / (host + "-config")
        config.mkdir()
        sandbox = ["/usr/bin/bwrap", "--die-with-parent", "--ro-bind", "/", "/", "--dev", "/dev", "--tmpfs", "/tmp",
                   "--bind", str(root), str(root),
                   "--bind", str(config), str(home / ("." + host)),
                   "--tmpfs", str(home / ".agents"), "--chdir", str(project)]
        env = {k: v for k, v in os.environ.items() if not k.startswith(("CODEX_", "CLAUDE_", "ANTHROPIC_", "OPENAI_"))}
        def native(*args, installer=False):
            binary = str(Path(sys.argv[1]).resolve()) if installer else executable
            result = subprocess.run(sandbox + [binary, *args], env=env, text=True,
                                    capture_output=True, timeout=60)
            if result.returncode:
                raise AssertionError((host, args, result.stdout, result.stderr))
            return result.stdout
        version = native("--version").strip()
        assert version == {"claude": "2.1.268 (Claude Code)", "codex": "codex-cli 0.154.0"}[host], "client prerequisite changed: " + version
        print("CLIENT", host, version)
        package = root / host / "0.0.1"
        native("agent", "install", host, "--source", str(package), "--dry-run", installer=True)
        assert not list(config.rglob("plugin.json"))
        native("agent", "install", host, "--source", str(package), installer=True)
        native("agent", "install", host, "--source", str(package), installer=True)
        listing = native("plugin", "list", "--json")
        assert "burrow" in listing and "0.0.1" in listing, listing
        print("PASS native", host, "install, repeat install and discovery")
        manifest = package / "plugins/burrow" / (".claude-plugin" if host == "claude" else ".codex-plugin") / "plugin.json"
        data = json.loads(manifest.read_text())
        data["version"] = "0.0.2"
        manifest.write_text(json.dumps(data))
        skill = package / "plugins/burrow/skills/burrow-tunnels/SKILL.md"
        skill.write_text(skill.read_text() + "\nPrototype package revision: 0.0.2.\n")
        if host == "claude":
            native("plugin", "marketplace", "update", "bochner-burrow")
            native("plugin", "update", "burrow@bochner-burrow")
        else:
            native("plugin", "remove", "burrow@bochner-burrow")
            native("plugin", "add", "burrow@bochner-burrow")
        listing = native("plugin", "list", "--json")
        assert "0.0.2" in listing, listing
        print("PASS native", host, "versioned changed-content update")
    print("NOT PROVEN: OpenCode native discovery; external-agent live tunnel creation/traffic and negative cases")
