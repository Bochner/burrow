"""Exercise the actual --vt frontend through a controlling terminal."""
import fcntl
import os
import pty
import select
import signal
import struct
import subprocess
import sys
import termios
import time

binary = os.path.abspath(sys.argv[1])
master, slave = pty.openpty()
original = termios.tcgetattr(slave)
def size(cols, rows):
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
size(80,24)
def controlling():
    os.setsid()
    fcntl.ioctl(0, termios.TIOCSCTTY, 0)
p = subprocess.Popen([binary, "--vt"], stdin=slave, stdout=slave, stderr=slave,
    env=dict(os.environ, TERM="xterm-256color", NO_COLOR="1"), preexec_fn=controlling)
seen = bytearray()
def wait(marker):
    deadline=time.monotonic()+8
    while time.monotonic()<deadline:
        if marker in seen:
            seen.clear()
            return
        if select.select([master],[],[],.05)[0]:
            seen.extend(os.read(master,65536))
    raise AssertionError(f"missing {marker!r}; tail {bytes(seen[-1500:])!r}")
def send(data):
    os.write(master,data)
try:
    wait(b"Type help")
    send(b"connect\r")
    wait(b"CONNECTED")
    send(b"shell\r")
    wait(b"local-fixture$")
    send(b"printf '\\033[?1049h\\033[2J\\033[3;5HRESTORE_%s' SCREEN\r")
    wait(b"RESTORE_SCREEN")
    send(b"\x1d")
    wait(b"Returned to management")
    send(b"shell\r")
    wait(b"local-fixture$")
    send(b"printf 'SECOND_%s' SCREEN\r")
    wait(b"SECOND_SCREEN")
    send(b"\x1d")
    wait(b"Returned to management")
    send(b"resume 1\r")
    wait(b"RESTORE_SCREEN")
    send(b"printf '\\033[?1049l'\r")
    wait(b"local-fixture$")
    # Raw NUL must reach dd without Enter; output marker cannot match echoed input.
    send(b"stty raw -echo; printf 'RAW_%s' READY; dd bs=1 count=1 2>/dev/null | od -An -tx1; stty sane\r")
    wait(b"RAW_READY")
    send(b"\x00")
    wait(b" 00")
    send(b"sleep 30\r")
    time.sleep(.1)
    send(b"\x03")
    send(b"printf 'INTERRUPT_%s\\n' OK\r")
    wait(b"INTERRUPT_OK")
    send(b"\x1d")
    wait(b"Returned to management")
    size(100,35)
    send(b"resume 1\r")
    wait(b"local-fixture$")
    send(b"stty size\r")
    wait(b"35 100")
    send(b"\x1d")
    wait(b"Returned to management")
    send(b"quit\r")
    p.wait(timeout=5)
    assert p.returncode == 0
    assert termios.tcgetattr(slave) == original
    print("PASS: executable VT shell switch/redraw, independent shells, raw NUL, Ctrl-C, resize and termios restoration")
finally:
    if p.poll() is None:
        p.kill()
        p.wait(timeout=3)
    os.close(master)
    os.close(slave)
