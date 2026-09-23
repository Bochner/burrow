"""Install the release's operator skills through its public CLI, without clients."""
import json
import hashlib
import os
from pathlib import Path
import subprocess
import shutil
import sys
import tarfile
import tempfile
import zipfile

from core.cmd.burrow.agent_faults import failed_rename


with tempfile.TemporaryDirectory(prefix="burrow-agent-") as temporary:
    root = Path(temporary)
    with tarfile.open(sys.argv[1]) as release:
        release.extractall(root / "release", filter="data")
    binary = str(next((root / "release").rglob("burrow")))
    home, project = root / "home", root / "project"
    home.mkdir(); project.mkdir()
    env = {k: v for k, v in os.environ.items()
           if not k.startswith(("CLAUDE_", "CODEX_", "OPENCODE_", "XDG_"))}
    env.update(HOME=str(home), XDG_CACHE_HOME=str(root / "cache"))
    source = root / "source"
    with zipfile.ZipFile(root / "release/burrow-agent.zip") as bundle:
        bundle.extractall(source)

    def manifest():
        path = source / "burrow-agent.json"
        data = json.loads(path.read_text())
        data["files"] = {str(p.relative_to(source)): hashlib.sha256(p.read_bytes()).hexdigest()
                         for p in (source / "skills").rglob("*") if p.is_file()}
        path.write_text(json.dumps(data))

    def snapshot(path):
        return {str(p.relative_to(path)): p.read_bytes() for p in path.rglob("*") if p.is_file()}

    def cli(*args, ok=True):
        result = subprocess.run([binary, "--offline", "agent", "install", *args],
                                cwd=project, env=env, capture_output=True, text=True, timeout=15)
        assert (result.returncode == 0) == ok, result
        return json.loads(result.stdout) if ok else result.stderr

    locations = {
        ("claude", "user"): home / ".claude/skills",
        ("claude", "project"): project / ".claude/skills",
        ("codex", "user"): home / ".agents/skills",
        ("codex", "project"): project / ".agents/skills",
        ("opencode", "user"): home / ".config/opencode/skills",
        ("opencode", "project"): project / ".opencode/skills",
    }
    for (host, scope), destination in locations.items():
        args = [host, "--scope", scope]
        preview = cli(*args, "--dry-run")
        assert preview["dryRun"] and not destination.exists()
        installed = cli(*args)
        assert installed["bundleSHA256"] == hashlib.sha256((source / "burrow-agent.json").read_bytes()).hexdigest()
        assert {p.parent.name for p in destination.glob("*/SKILL.md")} == {"burrow", "burrow-inspect"}
        before = {p: (p.read_bytes(), p.stat().st_mtime_ns) for p in destination.rglob("*") if p.is_file()}
        repeated = cli(*args)
        assert all(s["action"] == "unchanged" for s in repeated["skills"])
        assert before == {p: (p.read_bytes(), p.stat().st_mtime_ns) for p in before}
        unrelated = destination / "hovel/SKILL.md"
        unrelated.parent.mkdir(); unrelated.write_text("existing Hovel skill")
        config = destination.parent / "config.json"
        config.write_text('{"mcpServers":{"hovel":{"keep":true}}}')

    # A new source release updates every client/scope, retaining old bytes outside
    # discovery. The checks exercise the packaged binary, not installer helpers.
    for path in (source / "skills").glob("*/SKILL.md"):
        path.write_text(path.read_text().replace('"0.1.0"', '"0.1.1"') + "\nUpdated instructions.\n")
    metadata = source / "burrow-agent.json"
    data = json.loads(metadata.read_text()); data["version"] = "0.1.1"
    metadata.write_text(json.dumps(data)); manifest()
    for (host, scope), destination in locations.items():
        args = [host, "--scope", scope, "--source", str(source)]
        old = snapshot(destination)
        updated = cli(*args)
        assert updated["version"] == "0.1.1" and updated["complete"]
        for skill in updated["skills"]:
            assert skill["action"] == "update" and skill["state"] == "applied"
            backup = Path(skill["backup"])
            assert not backup.is_relative_to(destination)
            assert snapshot(backup) == {p[len(skill["name"])+1:]: b for p, b in old.items() if p.startswith(skill["name"] + "/")}
        assert (destination / "hovel/SKILL.md").read_text() == "existing Hovel skill"
        assert (destination.parent / "config.json").read_text() == '{"mcpServers":{"hovel":{"keep":true}}}'
        edited = destination / "burrow-inspect/SKILL.md"
        edited.write_text(edited.read_text() + "\nMy local edit.\n")
        before = snapshot(destination)
        assert "preserved edited/unmanaged" in cli(*args, ok=False)
        assert snapshot(destination) == before
        edited.write_bytes((source / "skills/burrow-inspect/SKILL.md").read_bytes())
        (destination / "burrow-inspect/.burrow-installed.json").unlink()
        # Identical unmanaged content is a true no-op, never silently adopted.
        assert cli(*args)["skills"][1]["action"] == "unchanged"
        assert not (destination / "burrow-inspect/.burrow-installed.json").exists()
        edited.write_text("my unrelated skill")
        assert "preserved edited/unmanaged" in cli(*args, ok=False)
        edited.write_bytes(before["burrow-inspect/SKILL.md"])
        shutil.rmtree(destination / "burrow-inspect")
        cli(*args)
        # Every client/scope rejects symlinked discovery roots and preflights
        # conflicts in the later skill before changing the earlier one.
        hidden = destination.with_name("hidden-skills")
        destination.rename(hidden); destination.symlink_to(hidden, target_is_directory=True)
        cli(*args, ok=False)
        destination.unlink(); hidden.rename(destination)
        prior_source = (source / "skills/burrow/SKILL.md").read_bytes()
        (source / "skills/burrow/SKILL.md").write_bytes(prior_source + b"\nPending revision.\n"); manifest()
        edited.write_text("preserve this local edit")
        prior = snapshot(destination)
        cli(*args, ok=False)
        assert snapshot(destination) == prior, "suite preflight wrote before finding a conflict"
        edited.write_bytes((source / "skills/burrow-inspect/SKILL.md").read_bytes())
        (source / "skills/burrow/SKILL.md").write_bytes(prior_source); manifest()

    # Metadata and every source/destination/backup path preflight before writes.
    args = ["codex", "--scope", "project", "--source", str(source)]
    destination = locations["codex", "project"]
    before = snapshot(destination)
    skill = source / "skills/burrow/SKILL.md"
    valid = skill.read_bytes()
    for malformed in (b"no frontmatter", valid.replace(b"name: burrow", b"name: other"),
                      valid.replace(b"metadata:\n", b"").replace(b"  burrow-", b"burrow-"),
                      valid.replace(b"description: ", b"description: ["),
                      valid.replace(b"description: ", b"description: @"),
                      valid.replace(b"\ncompatibility:", b":\ncompatibility:"),
                      valid.replace(b"description: ", b"description: Inspect\x01 "),
                      valid.replace(b'name: burrow', b'name: "burro\\167"'),
                      valid.replace(b'burrow-cli-contract: "1"', b'burrow-cli-contract: 1'),
                      valid[:valid.index(b"description:")] + b"description: 123\n" + valid[valid.index(b"compatibility:"):]):
        skill.write_bytes(malformed); manifest()
        for (host, scope), candidate in locations.items():
            prior = snapshot(candidate)
            cli(host, "--scope", scope, "--source", str(source), ok=False)
            assert snapshot(candidate) == prior
    skill.write_bytes(valid); manifest()
    reserved = source / "skills/burrow-inspect/.burrow-installed.json"
    reserved.mkdir(); (reserved / "child").write_text("invalid receipt subtree")
    manifest(); cli(*args, ok=False)
    shutil.rmtree(reserved); manifest()
    skill.write_bytes(valid + b"corrupt checksum")
    cli(*args, ok=False)
    skill.write_bytes(valid)
    alias = root / "alias"; alias.symlink_to(source, target_is_directory=True)
    cli("codex", "--scope", "project", "--source", str(alias), ok=False)
    cli("codex", "--scope", "project", "--source", str(alias / "skills/.."), ok=False)
    for path in (source / "skills/burrow/link", destination / "burrow/link"):
        path.symlink_to(skill)
        cli(*args, ok=False)
        path.unlink()
    backup_root = destination.parent / "burrow-skill-backups"
    saved = backup_root.with_name("saved-backups")
    backup_root.rename(saved); backup_root.symlink_to(saved, target_is_directory=True)
    cli(*args, ok=False)
    backup_root.unlink(); saved.rename(backup_root)
    assert snapshot(destination) == before
    # ptrace injects a real OS rename failure into the distributable. No test
    # switch or mock is compiled into production. First rename preserves the
    # old skill, second publishes the replacement, third attempts restoration.
    skill.write_bytes(valid + b"\nNext revision.\n"); manifest()
    for (host, scope), destination in locations.items():
        before = snapshot(destination)
        for fault, restored in (("1", True), ("2", True), ("2+", False)):
            failed = failed_rename([binary, "agent", "install", host, "--scope", scope, "--source", str(source)],
                                   cwd=project, env=env, fault=fault)
            assert failed.returncode == 1 and ("backup failed" if fault == "1" else "replacement failed") in failed.stderr, failed
            report = json.loads(failed.stdout)
            assert not report["complete"] and report["skills"][0]["state"] == "failed", report
            if restored:
                assert ("backup failed" if fault == "1" else "previous version restored") in failed.stderr
                assert snapshot(destination) == before
                assert "backup" not in report["skills"][0]
            else:
                backup = Path(report["skills"][0]["backup"])
                assert str(backup) in failed.stderr and not (destination / "burrow").exists()
                assert snapshot(backup) == {p[len("burrow/"):]: b for p, b in before.items() if p.startswith("burrow/")}
                backup.rename(destination / "burrow")  # Explicit manual recovery.
                assert snapshot(destination) == before
    for host, variable, path in (("claude", "CLAUDE_CONFIG_DIR", root / "custom-claude"),
                                 ("opencode", "XDG_CONFIG_HOME", root / "custom-config")):
        env[variable] = str(path)
        report = cli(host, "--scope", "user")
        expected = path / ("opencode/skills" if host == "opencode" else "skills")
        assert Path(report["destination"]) == expected
        del env[variable]
    assert not list(root.rglob("plugin.json")) and not list(root.rglob("marketplace.json"))
    cli("codex", ok=False)  # Scope is an explicit operator choice.
    assert not (root / "cache").exists(), "skill installation initialized a runtime/download cache"
print("PASS packaged offline skills: six locations, update/backup/conflict, metadata/path refusal, OS failure/restore, coexistence and overrides")
