"""Disposable local SSH handoff; plain-output replay, not a VT screen emulator."""
import fcntl
import json
import os
import pty
import select
import signal
import struct
import subprocess
import sys
import termios
import tty

config, control = sys.argv[1:]
shells = {}
active = None
dirty = True
stopping = False
LIMIT = 65536


def changed(*_):
    global dirty
    dirty = True


def stop(*_):
    global stopping
    stopping = True


with open('/dev/tty', 'r+b', buffering=0) as terminal:
    fd = terminal.fileno()
    original = termios.tcgetattr(fd)

    def size():
        rows, cols, _, _ = struct.unpack('HHHH', fcntl.ioctl(fd, termios.TIOCGWINSZ, bytes(8)))
        if not (0 < rows <= 65535 and 0 < cols <= 65535):
            raise ValueError('terminal geometry unavailable')
        return rows, cols

    def geometry(shell):
        rows, cols = size()
        fcntl.ioctl(shell['fd'], termios.TIOCSWINSZ, struct.pack('HHHH', rows, cols, 0, 0))

    def manage():
        state = {key: {'pid': s['process'].pid, 'buffer': len(s['tail']), 'discarded': s['discarded'],
                       'exited': s['process'].poll() is not None} for key, s in shells.items()}
        terminal.write(('\x1b[H\x1b[2JMANAGEMENT ' + json.dumps(state) + '\r\n1/2: shell; q: quit; Ctrl-]: management\r\n').encode())

    def attach(key):
        global active
        rows, cols = size()
        if key not in shells or shells[key]['process'].poll() is not None:
            if key in shells:
                os.close(shells.pop(key)['fd'])
            master, slave = pty.openpty()
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', rows, cols, 0, 0))
            def controlling_terminal():
                os.setsid()
                fcntl.ioctl(0, termios.TIOCSCTTY, 0)
            try:
                process = subprocess.Popen(['/usr/bin/ssh', '-F', config, '-S', control,
                    '-o', 'ProxyCommand=/bin/false', '-tt', 'target', 'exec /bin/sh'],
                    stdin=slave, stdout=slave, stderr=slave, preexec_fn=controlling_terminal)
            except BaseException:
                os.close(master)
                raise
            finally:
                os.close(slave)
            shells[key] = {'fd': master, 'process': process, 'tail': bytearray(), 'discarded': 0, 'eof': False}
        shell = shells[key]
        geometry(shell)
        active = key
        terminal.write(f'\x1b[H\x1b[2JSHELL {key} {cols}x{rows}\r\n'.encode())
        # ponytail: bounded byte replay is only a plain-output prototype; full-screen redraw needs a VT screen model.
        if shell['discarded']:
            terminal.write(b'[older output discarded]\r\n')
        terminal.write(shell['tail'])

    signal.signal(signal.SIGWINCH, changed)
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGHUP, stop)
    try:
        tty.setraw(fd)
        terminal.write(b'\x1b[?1049h')
        while not stopping:
            if dirty:
                dirty = False
                if active is None:
                    manage()
                else:
                    try:
                        geometry(shells[active])
                    except ValueError:
                        terminal.write(b'\r\nERROR terminal geometry unavailable\r\n')
            readers = [fd] + [s['fd'] for s in shells.values() if not s['eof']]
            for ready in select.select(readers, [], [], .03)[0]:
                if ready == fd:
                    data = os.read(fd, 1)
                    if not data:
                        stopping = True
                    elif active is not None:
                        if data == b'\x1d':
                            active = None
                            dirty = True
                        else:
                            try:
                                os.write(shells[active]['fd'], data)
                            except OSError:
                                active = None
                                dirty = True
                    elif data == b'q':
                        stopping = True
                    elif data in (b'1', b'2'):
                        try:
                            attach(data.decode())
                        except ValueError:
                            terminal.write(b'ERROR terminal geometry unavailable\r\n')
                else:
                    key, shell = next((k,s) for k,s in shells.items() if s['fd'] == ready)
                    try:
                        data = os.read(ready, 4096)
                    except OSError:
                        data = b''
                    if not data:
                        shell['eof'] = True
                        shell['process'].wait(timeout=3)
                        if active == key:
                            active = None
                            dirty = True
                        continue
                    shell['tail'].extend(data)
                    extra = max(0, len(shell['tail']) - LIMIT)
                    if extra:
                        del shell['tail'][:extra]
                        shell['discarded'] += extra
                    if active == key:
                        terminal.write(data)
    finally:
        for shell in shells.values():
            os.close(shell['fd'])
            process = shell['process']
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=3)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=3)
        try:
            terminal.write(b'\x1b[?25h\x1b[?1049l')
        finally:
            termios.tcsetattr(fd, termios.TCSADRAIN, original)
    terminal.write(b'Frontend exited; local shells ended\r\n')
