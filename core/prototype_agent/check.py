"""Portable direct-skill install/update checks; no agent clients or model calls."""
from pathlib import Path
import os
import shutil
import tempfile
import subprocess
import sys
from unittest.mock import patch

from core.prototype_agent.install import RECEIPT, install_skills, skill_destination, snapshot


def refused(action, exception):
    try:
        action()
    except exception:
        return
    raise AssertionError('unsafe installation was accepted')


with tempfile.TemporaryDirectory(prefix='burrow-skills-') as temporary:
    root = Path(temporary)
    home, project = root / 'home', root / 'project'
    home.mkdir(); project.mkdir()
    executable = str(Path(sys.argv[1]).resolve())
    cli = subprocess.run([executable, 'agent', 'install', 'codex', '--scope', 'project'], cwd=project, capture_output=True, text=True)
    assert cli.returncode == 0, cli.stderr
    assert (project/'.agents/skills/burrow/SKILL.md').is_file()
    shutil.rmtree(project/'.agents')
    source = root / 'source'
    shutil.copytree(Path(__file__).parent / 'agent/skills', source)
    expected = {
        ('claude', 'user'): home / '.claude/skills',
        ('claude', 'project'): project / '.claude/skills',
        ('codex', 'user'): home / '.agents/skills',
        ('codex', 'project'): project / '.agents/skills',
        ('opencode', 'user'): home / '.config/opencode/skills',
        ('opencode', 'project'): project / '.opencode/skills',
    }
    with patch.object(Path, 'home', return_value=home), patch.object(Path, 'cwd', return_value=project), patch.dict(os.environ, {}, clear=True):
        for (host, scope), destination in expected.items():
            assert skill_destination(host, scope) == destination
            install_skills(source, destination, dry_run=True)
            assert not destination.exists()
            install_skills(source, destination)
            # Check the documented discovery layout and metadata, independently
            # of installer state. Native client loading has a separate opt-in check.
            assert {p.parent.name for p in destination.glob('*/SKILL.md')} == {'burrow', 'burrow-tunnels'}
            before = {p: p.read_bytes() for p in destination.rglob('*') if p.is_file()}
            install_skills(source, destination)
            assert before == {p: p.read_bytes() for p in destination.rglob('*') if p.is_file()}
        with patch.dict(os.environ, {'CLAUDE_CONFIG_DIR': str(root/'claude-custom'), 'XDG_CONFIG_HOME': str(root/'config-custom')}):
            assert skill_destination('claude', 'user') == root/'claude-custom/skills'
            assert skill_destination('opencode', 'user') == root/'config-custom/opencode/skills'

    destination = expected['codex', 'project']
    unrelated = destination / 'hovel/SKILL.md'
    unrelated.parent.mkdir(); unrelated.write_text('existing Hovel skill')
    config = destination.parent / 'config.toml'
    config.write_text('existing client configuration')
    old = snapshot(destination/'burrow-tunnels')
    (source/'burrow-tunnels/SKILL.md').write_text((source/'burrow-tunnels/SKILL.md').read_text()+'\nChanged upstream instructions.\n')
    install_skills(source, destination)
    assert snapshot(destination/'burrow-tunnels') == snapshot(source/'burrow-tunnels')
    backups = list((destination.parent/'burrow-skill-backups').glob('*/burrow-tunnels'))
    assert len(backups) == 1 and snapshot(backups[0]) == old
    assert unrelated.read_text() == 'existing Hovel skill' and config.read_text() == 'existing client configuration'
    edited = destination/'burrow-tunnels/SKILL.md'
    edited.write_text('owner edited this skill')
    (source/'burrow/SKILL.md').write_text((source/'burrow/SKILL.md').read_text()+'\nAnother update.\n')
    before = snapshot(destination/'burrow')
    refused(lambda: install_skills(source, destination), FileExistsError)
    assert edited.read_text() == 'owner edited this skill'
    assert snapshot(destination/'burrow') == before, 'suite preflight must precede all writes'
    # An unmanaged different skill, symlinked destination/source and malformed
    # discovery metadata all refuse without overwriting files outside the suite.
    unmanaged = root/'unmanaged/burrow'
    unmanaged.mkdir(parents=True); (unmanaged/'SKILL.md').write_text('owner skill')
    refused(lambda: install_skills(source, unmanaged.parent), FileExistsError)
    alias = root/'alias'; alias.symlink_to(unmanaged.parent, target_is_directory=True)
    refused(lambda: install_skills(source, alias), ValueError)
    (source/'burrow/escape').symlink_to(config)
    refused(lambda: install_skills(source, root/'bad-source'), ValueError)
    (source/'burrow/escape').unlink()
    (source/'burrow/SKILL.md').write_text('missing frontmatter')
    refused(lambda: install_skills(source, root/'bad-metadata'), ValueError)
    assert config.read_text() == 'existing client configuration'
    assert not list(root.rglob('plugin.json')) and not list(root.rglob('marketplace.json'))
    print('PASS six direct skill locations, discovery metadata, dry-run/no-op, changed-content update, preserved previous version, edited/unmanaged conflict refusal, symlink refusal and Hovel/config coexistence')
