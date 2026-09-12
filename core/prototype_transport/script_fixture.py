"""Disposable remote supervisor: fixed inert cases, Linux process groups only."""
import base64
import json
import os
from pathlib import Path
import selectors
import signal
import subprocess
import sys
import tempfile
import time

case, marker, keep = sys.argv[1:]
assert case in ('success', 'nonzero', 'cancel', 'timeout', 'loss', 'launch-loss', 'invocation')
assert keep in ('yes', 'no')
stage = None
script = None
request = {}
input_file = None
script_fd = None
if case == 'invocation':
    # Read exactly one bounded JSON line: leave cancellation bytes on fd 0.
    envelope = bytearray()
    while len(envelope) <= 65536:
        byte = os.read(0, 1)
        if byte == b'\n':
            break
        if not byte:
            raise ValueError('missing invocation')
        envelope.extend(byte)
    else:
        raise ValueError('invocation too large')
    request = json.loads(envelope)
    mode = request['mode']
    assert mode in ('staged', 'streamed', 'existing', 'command', 'local')
    args = request['args']
    assert isinstance(args, list) and all(isinstance(a, str) and '\0' not in a for a in args)
    interpreter = request.get('interpreter', '/bin/sh')
    assert isinstance(interpreter, str) and interpreter.startswith('/') and '\0' not in interpreter
    input_file = tempfile.TemporaryFile()
    input_file.write(base64.b64decode(request.get('stdinBase64', ''), validate=True))
    input_file.seek(0)
    if mode in ('staged', 'streamed'):
        source = base64.b64decode(request['scriptBase64'], validate=True)
        assert len(source) <= 4096  # Bounded inert pipe fixture, not a production upload limit.
        if mode == 'staged':
            stage = Path(tempfile.mkdtemp(prefix='burrow-script-', dir=Path(marker).parent))
            script = stage / 'selected-script'
            script.write_bytes(source)
            argv = [interpreter, str(script), *args]
        else:
            script_fd, writer = os.pipe()
            os.write(writer, source)
            os.close(writer)
            argv = [interpreter, '/dev/fd/'+str(script_fd), *args]
    elif mode in ('existing', 'local'):
        argv = [interpreter, request['path'], *args]
    else:
        assert args and args[0].startswith('/')
        argv = args
    p = subprocess.Popen(argv, stdin=input_file, stdout=subprocess.PIPE,
                         stderr=subprocess.PIPE, start_new_session=True,
                         pass_fds=(() if script_fd is None else (script_fd,)))
    if script_fd is not None:
        os.close(script_fd)
    input_file.close()
else:
    stage = Path(tempfile.mkdtemp(prefix='burrow-script-', dir=Path(marker).parent))
    script = stage / 'inert.sh'
    script.write_text('''printf '%s\\n' "$1"
    printf 'fixture stderr\\n' >&2
    head -c 70000 /dev/zero
    sleep 2 &
    child=$!
    printf '%s' "$child" > "$2.child"
    wait "$child"
    printf completed > "$2.finished"
    exit "$3"
    ''')
    # An independent observer uses these paths; Burrow never reads them as results.
    p = subprocess.Popen(['/bin/sh', str(script), "space ' quote ; $(not-executed) ☃", marker,
                          '7' if case == 'nonzero' else '0'],
                         stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
if script is not None:
    Path(marker + '.stage').write_text(str(script))
Path(marker + '.started').write_text(str(p.pid))
print(json.dumps({'state': 'running', 'pid': p.pid, 'stage': str(script)}), flush=True)
sel = selectors.DefaultSelector()
for stream, name in ((p.stdout, 'stdout'), (p.stderr, 'stderr'), (sys.stdin.buffer, 'control')):
    sel.register(stream, selectors.EVENT_READ, name)
output = {'stdout': bytearray(), 'stderr': bytearray()}
discarded = {'stdout': 0, 'stderr': 0}
reason = 'remote-exit'
deadline = time.monotonic() + (0.5 if case == 'timeout' else 8)
terminated = False
while p.poll() is None or any(key.data != 'control' for key in sel.get_map().values()):
    for key, _ in sel.select(0.05):
        chunk = os.read(key.fd, 8192)
        if not chunk:
            sel.unregister(key.fileobj)
            if key.data == 'control' and p.poll() is None:
                reason = 'control-lost'
            continue
        if key.data == 'control':
            reason = 'cancelled'
        else:
            prefix = chunk[:65536-len(output[key.data])]
            output[key.data].extend(prefix)
            discarded[key.data] += len(chunk)-len(prefix)
    if time.monotonic() >= deadline and p.poll() is None and reason == 'remote-exit':
        reason = 'timed-out'
    if reason != 'remote-exit' and not terminated:
        try:
            os.killpg(p.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        terminated = True
    if time.monotonic() >= deadline + 1:
        # Only our group. Escaped/daemonized descendants are outside this proof.
        try:
            os.killpg(p.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        break
p.wait(timeout=2)
cleanup = 'not-needed' if script is None else 'kept'
if script is not None and keep == 'no':
    try:
        if request.get('cleanupFailure'):
            # Independent unexpected content forces a real rmdir failure.
            (stage / 'unrelated').write_text('must survive cleanup')
        script.unlink()
        stage.rmdir()
        cleanup = 'removed'
    except OSError:
        cleanup = 'failed'
if request.get('mode') == 'local' and reason == 'remote-exit':
    reason = 'local-exit'
print(json.dumps({'state': reason, ('localExit' if request.get('mode') == 'local' else 'remoteExit'): p.returncode, 'pid': p.pid,
                  'stage': str(script), 'cleanup': cleanup,
                  **{name+'Base64': base64.b64encode(data).decode() for name, data in output.items()},
                  **{name+'Discarded': count for name, count in discarded.items()}}), flush=True)
