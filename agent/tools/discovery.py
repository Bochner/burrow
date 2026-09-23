"""Opt-in native skill discovery, isolated from user config and the network."""
import json
import os
from pathlib import Path
import select
import shutil
import subprocess
import tempfile
import time
import sys
import tarfile

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
    with tarfile.open(sys.argv[1]) as release:
        release.extractall(root/'release', filter='data')
    burrow = str(root/'release/burrow')
    env = {k: v for k, v in os.environ.items() if not k.startswith(('CODEX_', 'CLAUDE_', 'ANTHROPIC_', 'OPENAI_', 'OPENCODE_', 'XDG_'))}
    for host, scope in ((host, scope) for host in ('codex', 'claude', 'opencode') for scope in ('project', 'user')):
        binary = shutil.which(host)
        if not binary:
            print('UNVERIFIED '+host+' '+scope+': client absent; placement tests do not prove native discovery')
            continue
        binary = str(Path(binary).resolve())
        scratch = root/(host+'-'+scope)
        scratch.mkdir()
        project = scratch/'project'; project.mkdir()
        config = scratch/'home'; config.mkdir()
        env.update(XDG_DATA_HOME=str(scratch/'data'), XDG_STATE_HOME=str(scratch/'state'), XDG_CACHE_HOME=str(scratch/'cache'))
        # A writable empty home works even when the real user has no skill/config
        # directories yet. Restore only installed executable dependencies read-only.
        sandbox = ['/usr/bin/bwrap', '--die-with-parent', '--unshare-net', '--ro-bind', '/', '/', '--dev', '/dev', '--tmpfs', '/tmp', '--bind', str(root), str(root), '--bind', str(config), str(home), '--chdir', str(project)]
        tool_dirs = set()
        for executable in (binary, shutil.which('node')):
            if not executable:
                continue
            path = Path(executable).resolve()
            if path.is_relative_to(home):
                directory = path.parent
                # npm shims may load platform binaries from sibling packages.
                for parent in path.parents:
                    if parent.name == 'node_modules':
                        directory = parent
                tool_dirs.add(directory)
        for directory in sorted(tool_dirs):
            sandbox += ['--ro-bind', str(directory), str(directory)]
        skill_root = {'codex': '.agents', 'claude': '.claude', 'opencode': '.opencode' if scope == 'project' else '.config/opencode'}[host]
        install = subprocess.run(sandbox+[burrow, 'agent', 'install', host, '--scope', scope], env=env, capture_output=True, text=True)
        assert install.returncode == 0, install.stderr
        visible = (project if scope == 'project' else home)/skill_root/'skills'
        if host == 'opencode':
            result = subprocess.run(sandbox+[binary, 'debug', 'skill'], env=env, capture_output=True, text=True, timeout=30)
            assert result.returncode == 0, result.stderr
            skills = {s['name']: s for s in json.loads(result.stdout)}
            assert {'burrow', 'burrow-inspect'} <= skills.keys(), skills
            assert all(Path(skills[n]['location']).is_relative_to(visible) for n in ('burrow', 'burrow-inspect')), skills
            print('PASS native opencode '+scope+' discovery without model calls or network access')
            continue
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
                    found = {s['name']: s for s in skills if s['name'] in ('burrow', 'burrow-inspect')}
                    assert set(found) == {'burrow', 'burrow-inspect'}, response
                    assert all(Path(s['path']).is_relative_to(visible) for s in found.values()), found
                else:
                    response = exchange(process, {'type': 'control_request', 'request_id': 'init', 'request': {'subtype': 'initialize'}}, lambda r: r.get('type') == 'control_response')
                    commands = response['response']['response']['commands']
                    assert {'burrow', 'burrow-inspect'} <= {c['name'] for c in commands}, response
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
    print('Native discovery checks complete; any UNVERIFIED clients above remain unverified.')
