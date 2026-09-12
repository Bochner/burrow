"""Opt-in native skill discovery, isolated from user config and the network."""
import json
import os
from pathlib import Path
import select
import shutil
import subprocess
import tempfile
import time

from core.prototype_agent.install import install_skills


def exchange(process, message, matches):
    process.stdin.write(json.dumps(message)+'\n'); process.stdin.flush()
    deadline = time.monotonic()+20
    while time.monotonic() < deadline:
        if select.select([process.stdout], [], [], 0.2)[0]:
            line = process.stdout.readline()
            if not line:
                raise AssertionError('client exited before discovery response')
            reply = json.loads(line)
            if matches(reply):
                return reply
    raise AssertionError('client discovery timed out')


with tempfile.TemporaryDirectory(prefix='burrow-discovery-') as temporary:
    root = Path(temporary)
    home = Path.home()
    source = root/'source'
    shutil.copytree(Path(__file__).parent/'agent/skills', source)
    env = {k: v for k, v in os.environ.items() if not k.startswith(('CODEX_', 'CLAUDE_', 'ANTHROPIC_', 'OPENAI_'))}
    for host, scope in ((host, scope) for host in ('codex', 'claude') for scope in ('project', 'user')):
        binary = shutil.which(host)
        if not binary:
            raise RuntimeError('optional discovery requires installed '+host)
        scratch = root/(host+'-'+scope)
        scratch.mkdir()
        project = scratch/'project'; project.mkdir()
        config = scratch/'config'; config.mkdir()
        # Hide every user skill root and client config; networking is disabled.
        sandbox = ['/usr/bin/bwrap', '--die-with-parent', '--unshare-net', '--ro-bind', '/', '/', '--dev', '/dev', '--tmpfs', '/tmp', '--bind', str(root), str(root), '--chdir', str(project)]
        for name in ('.agents', '.codex', '.claude', '.config'):
            directory = config/name; directory.mkdir()
            sandbox += ['--bind', str(directory), str(home/name)]
        if (home/'.claude.json').exists():
            empty = scratch/'claude.json'; empty.write_text('{}')
            sandbox += ['--bind', str(empty), str(home/'.claude.json')]
        skill_root = '.agents' if host == 'codex' else '.claude'
        install_skills(source, (project if scope == 'project' else config)/skill_root/'skills')
        visible = (project if scope == 'project' else home)/skill_root/'skills'
        command = [binary, 'app-server'] if host == 'codex' else [binary, '--print', '--input-format', 'stream-json', '--output-format', 'stream-json', '--verbose', '--settings', '{"disableAllHooks":true}']
        with (scratch/'stderr').open('w') as errors:
            process = subprocess.Popen(sandbox+command, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=errors, text=True, bufsize=1, env=env)
            try:
                if host == 'codex':
                    exchange(process, {'id': 1, 'method': 'initialize', 'params': {'clientInfo': {'name': 'burrow-discovery', 'version': '0.0.0'}}}, lambda r: r.get('id') == 1)
                    process.stdin.write('{"method":"initialized"}\n'); process.stdin.flush()
                    response = exchange(process, {'id': 2, 'method': 'skills/list', 'params': {'cwds': [str(project)], 'forceReload': True}}, lambda r: r.get('id') == 2)
                    assert 'error' not in response, response
                    skills = response['result']['data'][0]['skills']
                    found = {s['name']: s for s in skills if s['name'] in ('burrow', 'burrow-tunnels')}
                    assert set(found) == {'burrow', 'burrow-tunnels'}, response
                    assert all(Path(s['path']).is_relative_to(visible) for s in found.values()), found
                else:
                    response = exchange(process, {'type': 'control_request', 'request_id': 'init', 'request': {'subtype': 'initialize'}}, lambda r: r.get('type') == 'control_response')
                    commands = response['response']['response']['commands']
                    assert {'burrow', 'burrow-tunnels'} <= {c['name'] for c in commands}, response
                print('PASS native '+host+' discovers both standalone '+scope+' skills without model calls or network access')
            except Exception:
                print((scratch/'stderr').read_text()[-4000:])
                raise
            finally:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill(); process.wait(timeout=5)
    print('OpenCode: documented layout is checked portably; native discovery requires an installed client.')
