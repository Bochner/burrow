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
            del seen[:seen.index(marker) + len(marker)]
            return
        if select.select([master],[],[],.05)[0]:
            seen.extend(os.read(master,65536))
    raise AssertionError(f"missing {marker!r}; tail {bytes(seen[-1500:])!r}")
def send(data):
    os.write(master,data)
try:
    wait(b"homelab")
    send(b"connect\r")
    # v2 edits DISCONNECTED in place; match the newly appended operation result.
    wait(b"SIMULATED connection established")
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
    send(b"stty raw -echo; printf 'RAW_%s' READY; dd bs=1 count=1 2>/dev/null | od -An -tx1; stty sane; printf 'RAW_%s\\n' DONE\r")
    wait(b"RAW_READY")
    send(b"\x00")
    wait(b" 00")
    wait(b"RAW_DONE")
    send(b"sleep 30\r")
    wait(b"sleep 30")
    time.sleep(.1)
    send(b"\x03")
    # ISIG may flush queued input; await the shell before sending the next command.
    wait(b"local-fixture$")
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
    send(b"\x03")
    wait(b"Quit Burrow?")
    send(b"\x1b")
    time.sleep(.1)
    send(b"\x0b")
    wait(b"BURROW COMMAND MENU")
    send(b"\x1b")
    time.sleep(.1)
    send(b"\x03")
    wait(b"Quit Burrow?")
    send(b"\x1b[D\r")  # Quit is on the left; Keep working is the default on the right.
    p.wait(timeout=5)
    assert p.returncode == 0
    assert termios.tcgetattr(slave) == original
    # Match Aspect: stdin is a controlling TTY, stdout is captured. The UI
    # must still detect dimensions and render color on /dev/tty.
    seen.clear()
    size(120,40)
    p = subprocess.Popen([binary, "--color"], stdin=slave, stdout=subprocess.PIPE, stderr=slave,
        env=dict(os.environ, TERM="xterm-256color", COLORTERM="truecolor", NO_COLOR="1"), preexec_fn=controlling)
    wait(b"11;#282a36")
    wait(b"38;2;189;147;249")
    wait(b"SELECTED CONNECTION")
    send(b"con")
    wait(b"COMPLETION")
    send(b"\x1b[B\t")
    wait(b"required")
    send(b"\x03")
    wait(b"Quit Burrow?")
    send(b"\x03")
    stdout, _ = p.communicate(timeout=5)
    assert p.returncode == 0 and b"Prototype closed" in stdout
    assert b"\x1b[" not in stdout, "terminal control leaked into captured stdout"
    assert termios.tcgetattr(slave) == original
    print("PASS: VT switching, raw input, shell Ctrl-C, dialogs, completion, captured-stdout terminal size/color, and restoration")
finally:
    if p.poll() is None:
        p.kill()
        p.wait(timeout=3)
    os.close(master)
    os.close(slave)
