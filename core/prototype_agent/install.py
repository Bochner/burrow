"""Disposable direct-skill installer; trusted local sources, no plugin packaging."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import tempfile


RECEIPT = '.burrow-installed.json'


def skill_destination(host, scope):
    home = Path.home()
    if scope == 'project':
        return Path.cwd() / {'claude': '.claude', 'codex': '.agents', 'opencode': '.opencode'}[host] / 'skills'
    return {
        'claude': Path(os.environ.get('CLAUDE_CONFIG_DIR', home / '.claude')) / 'skills',
        'codex': home / '.agents/skills',
        'opencode': Path(os.environ.get('XDG_CONFIG_HOME', home / '.config')) / 'opencode/skills',
    }[host]


def snapshot(root):
    if root.is_symlink() or not root.is_dir():
        raise ValueError(f'expected a real skill directory: {root}')
    result = {}
    for path in sorted(root.rglob('*')):
        if path.is_symlink() or not (path.is_dir() or path.is_file()):
            raise ValueError(f'unsupported skill entry: {path}')
        if path.is_file() and path != root / RECEIPT:
            result[str(path.relative_to(root))] = hashlib.sha256(path.read_bytes()).hexdigest()
    return result


def install_skills(source, destination, dry_run=False):
    destination = destination.absolute()
    if any(p.is_symlink() for p in (destination, *destination.parents)):
        raise ValueError('skill destination contains a symlink')
    if source.is_symlink() or not source.is_dir():
        raise ValueError('source must be a directory of skill folders')
    plans = []
    # Preflight the whole suite so an edited skill refuses before any install.
    for skill in sorted(source.iterdir()):
        if not re.fullmatch(r'burrow(?:-[a-z0-9]+)*', skill.name):
            raise ValueError(f'expected a Burrow skill folder: {skill.name}')
        incoming = snapshot(skill)
        if (skill / RECEIPT).exists():
            raise ValueError('source contains installer state')
        text = (skill / 'SKILL.md').read_text()
        frontmatter = text.split('---', 2)
        if len(frontmatter) != 3 or frontmatter[0] or not re.search(r'^name: ' + re.escape(skill.name) + r'\s*$', frontmatter[1], re.M) or not re.search(r'^description: \S.+$', frontmatter[1], re.M):
            raise ValueError(f'missing matching name/description frontmatter: {skill}')
        target = destination / skill.name
        previous = snapshot(target) if target.exists() or target.is_symlink() else None
        receipt = target / RECEIPT
        if previous is not None and previous != incoming:
            if not receipt.is_file() or json.loads(receipt.read_text()) != previous:
                raise FileExistsError(f'preserved edited/unmanaged skill; move it aside before updating: {target}')
        plans.append((skill, target, incoming, previous))
    if not plans:
        raise ValueError('source contains no skills')
    for skill, target, incoming, previous in plans:
        action = 'unchanged' if previous == incoming else 'install' if previous is None else 'update'
        print(f'{action}: {target / "SKILL.md"}')
        if dry_run:
            continue
        destination.mkdir(parents=True, exist_ok=True)
        if action == 'unchanged':
            # Adopt identical pre-existing files without overwriting their contents.
            if not (target / RECEIPT).exists():
                (target / RECEIPT).write_text(json.dumps(incoming, sort_keys=True) + '\n')
            continue
        with tempfile.TemporaryDirectory(prefix='.burrow-stage-', dir=destination.parent) as temporary:
            staged = Path(temporary) / skill.name
            shutil.copytree(skill, staged)
            if snapshot(staged) != incoming:
                raise ValueError('source changed during installation')
            (staged / RECEIPT).write_text(json.dumps(incoming, sort_keys=True) + '\n')
            backup = None
            if previous is not None:
                if snapshot(target) != previous:
                    raise FileExistsError(f'skill changed during installation: {target}')
                backup_root = destination.parent / 'burrow-skill-backups'
                if backup_root.is_symlink():
                    raise ValueError('backup directory contains a symlink')
                backup_root.mkdir(exist_ok=True)
                backup = Path(tempfile.mkdtemp(prefix=skill.name + '-', dir=backup_root)) / skill.name
                target.rename(backup)
            try:
                staged.rename(target)
            except OSError:
                if backup is not None and not target.exists():
                    backup.rename(target)
                raise
            if backup is not None:
                print(f'previous version preserved: {backup}')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='command', required=True)
    agent = sub.add_parser('agent').add_subparsers(dest='action', required=True)
    install = agent.add_parser('install')
    install.add_argument('host', choices=('claude', 'codex', 'opencode'))
    install.add_argument('--scope', choices=('user', 'project'), default='user')
    install.add_argument('--source', type=Path, help='directory containing Burrow skill folders (defaults to bundled skills)')
    install.add_argument('--dry-run', action='store_true')
    args = parser.parse_args()
    try:
        with tempfile.TemporaryDirectory(prefix='burrow-bundled-skills-') as temporary:
            source = args.source
            if source is None:
                # Bazel exposes bundled resources as runfile symlinks. Materialize
                # only our bundled tree; external sources still reject symlinks.
                source = Path(temporary) / 'skills'
                shutil.copytree(Path(__file__).parent / 'agent/skills', source)
            install_skills(source, skill_destination(args.host, args.scope), args.dry_run)
    except (OSError, ValueError) as error:
        parser.exit(1, f'{error}\n')
    print('Restart the agent if needed, then verify burrow and burrow-tunnels in its skills list.')


if __name__ == '__main__':
    main()
