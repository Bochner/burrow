"""Linux amd64 OS fault injection into the unmodified distributable.

Count renameat2 across the whole process: strace's per-thread ordinal injection
can miss a replacement when a Go goroutine migrates between OS threads.
Linux interface: https://man7.org/linux/man-pages/man2/ptrace.2.html
"""
import ctypes
import errno
import os
import signal
import subprocess
import tempfile


class Registers(ctypes.Structure):
    _fields_ = [(name, ctypes.c_ulonglong) for name in
                "r15 r14 r13 r12 rbp rbx r11 r10 r9 r8 rax rcx rdx rsi rdi orig_rax rip cs eflags rsp ss fs_base gs_base ds es fs gs".split()]


def failed_rename(argv, cwd, env, fault):
    libc = ctypes.CDLL(None, use_errno=True)
    libc.ptrace.restype = ctypes.c_long

    def trace(request, pid, address=0, data=0):
        result = libc.ptrace(ctypes.c_uint(request), ctypes.c_uint(pid), ctypes.c_void_p(address),
                            ctypes.c_void_p(data) if isinstance(data, int) else ctypes.byref(data))
        if result == -1:
            raise OSError(ctypes.get_errno(), "ptrace: " + os.strerror(ctypes.get_errno()))

    with tempfile.TemporaryFile() as out, tempfile.TemporaryFile() as errors:
        child = subprocess.Popen(argv, cwd=cwd, env=env, stdout=out, stderr=errors,
                                 preexec_fn=lambda: trace(0, 0))  # PTRACE_TRACEME stops after exec.
        threads, failing = {child.pid}, set()
        count, injected = 0, 0
        try:
            _, status = os.waitpid(child.pid, 0)
            assert os.WIFSTOPPED(status) and os.WSTOPSIG(status) == signal.SIGTRAP, status
            # TRACESYSGOOD | TRACECLONE | EXITKILL: trace every Go thread, and
            # kill tracees if this checker dies unexpectedly.
            trace(0x4200, child.pid, data=1 | 8 | 0x100000)
            trace(24, child.pid)  # PTRACE_SYSCALL
            while threads:
                pid, status = os.waitpid(-1, 0x40000000)  # __WALL includes traced threads.
                if os.WIFEXITED(status) or os.WIFSIGNALED(status):
                    threads.discard(pid)
                    if pid == child.pid:
                        child.returncode = os.waitstatus_to_exitcode(status)
                    continue
                try:
                    sig, event = os.WSTOPSIG(status), status >> 16
                    deliver = 0
                    if event == 3:  # PTRACE_EVENT_CLONE
                        new = ctypes.c_ulong()
                        trace(0x4201, pid, data=new)
                        threads.add(new.value)
                    elif sig == signal.SIGTRAP | 0x80:
                        info = ctypes.create_string_buffer(88)
                        trace(0x420e, pid, ctypes.sizeof(info), info)  # GET_SYSCALL_INFO
                        if info.raw[0] == 1 and int.from_bytes(info.raw[24:32], "little") == 316:
                            count += 1  # renameat2, Linux amd64 release platform.
                            if count == int(fault.rstrip("+")) or (fault.endswith("+") and count > 2):
                                regs = Registers(); trace(12, pid, data=regs)
                                regs.orig_rax = 2**64 - 1  # Skip the actual filesystem mutation.
                                trace(13, pid, data=regs)
                                failing.add(pid); injected += 1
                        elif info.raw[0] == 2 and pid in failing:
                            regs = Registers(); trace(12, pid, data=regs)
                            regs.rax = (2**64) - errno.EACCES
                            trace(13, pid, data=regs); failing.remove(pid)
                    elif sig not in (signal.SIGSTOP, signal.SIGTRAP):
                        deliver = sig
                    trace(24, pid, data=deliver)
                except OSError as error:
                    # Another thread's exit_group can kill a stopped thread
                    # before we resume it. Its exit report is still pending.
                    if error.errno != errno.ESRCH:
                        raise
            assert injected == (2 if fault.endswith("+") else 1), (count, injected)
        finally:
            if child.returncode is None:
                child.kill()
                # Reap traced threads before the leader: waiting only for the
                # leader deadlocks when thread exit reports remain pending.
                while True:
                    try:
                        pid, status = os.waitpid(-1, 0x40000000)
                    except ChildProcessError:
                        break
                    if pid == child.pid and (os.WIFEXITED(status) or os.WIFSIGNALED(status)):
                        child.returncode = os.waitstatus_to_exitcode(status)
        out.seek(0); errors.seek(0)
        return subprocess.CompletedProcess(argv, child.returncode, out.read().decode(), errors.read().decode())
