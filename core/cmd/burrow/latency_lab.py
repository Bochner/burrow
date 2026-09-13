"""Production CLI phases; untraced timings, executable-start traces and per-phase totals."""
import fcntl
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import pty
import re
import select
import signal
import statistics
import struct
import subprocess
import termios
import time


def phase_totals(path, offset, low, high):
    """Sum traced phases that began inside one monotonic window, by name."""
    totals = {}
    with open(path, "rb") as stream:
        stream.seek(offset)
        for line in stream.read().splitlines():
            record = json.loads(line)
            if low <= record["begin"] <= high:
                entry = totals.setdefault(record["phase"], {"count": 0, "ns": 0})
                entry["count"] += 1
                entry["ns"] += record["ns"]
    return totals


def measure(binary, root, env, container, port, key, command, wait):
    secret = "synthetic-latency-" + os.urandom(16).hex()
    subprocess.run(["docker", "exec", "-i", container, "chpasswd"], input=f"tester:{secret}\n".encode(), check=True, capture_output=True)
    records = []
    tracing = ["strace", "--kill-on-exit", "-f", "-e", "trace=process", "-o"]
    phase_file = root / "phases.jsonl"
    phase_env = dict(env, BURROW_PHASE_TRACE=str(phase_file))
    provenance = {"machine": platform.platform(), "load": os.getloadavg(), "binary_sha256": hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
                  "build": "Aspect fastbuild", "synthetic_response_delay_s": .02,
                  "submission": "production CLI process launch; recap approval supplied by --yes",
                  "prompt": "first private CLI prompt received on PTY with echo disabled",
                  "process_counts": "successful execve starts, frontend plus daemon descendants; includes observer-induced SSH checks",
                  "phases": "BURROW_PHASE_TRACE totals per phase name across frontend, daemon-launched adapter and manager processes; nested phases overlap, so totals are not additive"}
    print("PROVENANCE " + json.dumps(provenance), flush=True)

    def starts(path):
        return len(re.findall(r'(?:execve\(.*|<\.\.\. execve resumed>.*)= 0$', path.read_text(), re.M))

    for mode, count in (("timed", 20), ("strace", 2), ("phases", 4)):
        traced = mode == "strace"
        phased = mode == "phases"
        for index in range(count):
            w = root / mode / str(index)
            w.parent.mkdir(exist_ok=True)
            output = root / "setup-output.json"
            daemon_trace = root / "daemon.trace"
            front_trace = root / "frontend.trace"
            sample_env = phase_env if phased else env
            setup = None
            info = None
            began = time.monotonic()
            began_ns = time.monotonic_ns()
            offset = phase_file.stat().st_size if phased and phase_file.exists() else 0
            try:
                if traced:
                    with output.open("w") as stream:
                        setup = subprocess.Popen([*tracing, str(daemon_trace), binary, "--workspace", str(w), "--offline", "status"], env=env, stdout=stream, stderr=subprocess.DEVNULL)
                    def ready():
                        try:
                            return json.loads(output.read_text())
                        except json.JSONDecodeError:
                            return None
                    info = wait(ready)
                else:
                    info = json.loads(command(binary, "--workspace", w, "--offline", "status", env=sample_env))
                setup_s = time.monotonic() - began
                setup_phases = phase_totals(phase_file, offset, began_ns, time.monotonic_ns()) if phased else None
                for temperature in ("cold", "warm"):
                    password = index % 2 == 1
                    args = [binary, "--workspace", str(w), "connect", temperature, "127.0.0.1", "tester", "--port", str(port), "--yes"]
                    args += ["--prompt"] if password else ["--key", str(key)]
                    if traced:
                        args = [*tracing, str(front_trace), *args]
                    before = starts(daemon_trace) if traced else 0
                    offset = phase_file.stat().st_size if phased else 0
                    prompt = None
                    submitted = time.monotonic_ns()
                    if password:
                        outer, slave = pty.openpty()
                        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 120, 0, 0))
                        def controlling():
                            os.setsid()
                            fcntl.ioctl(0, termios.TIOCSCTTY, 0)
                        p = subprocess.Popen(args, env=sample_env, stdin=slave, stdout=subprocess.PIPE, stderr=slave, preexec_fn=controlling)
                        transcript = bytearray()
                        answered = saved = False
                        try:
                            deadline = time.monotonic() + 50
                            while p.poll() is None:
                                assert time.monotonic() < deadline, "private prompt measurement timed out"
                                if select.select([outer], [], [], .02)[0]:
                                    transcript.extend(os.read(outer, 65536))
                                if not answered and b"SSH password" in transcript:
                                    assert not termios.tcgetattr(slave)[3] & termios.ECHO
                                    prompt = time.monotonic_ns()
                                    time.sleep(.02)
                                    os.write(outer, secret.encode() + b"\r")
                                    answered = True
                                if not saved and b"Save profile as" in transcript:
                                    os.write(outer, b"\x1b")
                                    saved = True
                            assert p.returncode == 0 and secret.encode() not in transcript, "private CLI connect failed"
                            state = json.loads(p.stdout.read())
                        finally:
                            if p.poll() is None:
                                p.kill()
                                p.wait()
                            os.close(outer)
                            os.close(slave)
                    else:
                        state = json.loads(command(*args, env=sample_env))
                    def connected():
                        s = json.loads(command(binary, "--workspace", w, "inspect", temperature, env=env))
                        assert s["state"] != "lost", s
                        return s if s["state"] == "connected" else None
                    state = wait(connected)
                    item = {"temperature": temperature, "auth": "password" if password else "key", "traced": traced, "phased": phased,
                            "setup_s": setup_s, "dispatch_s": (state["dispatch"] - submitted) / 1e9,
                            "connected_s": (state["connected"] - submitted) / 1e9,
                            "prompt_s": (prompt - state["dispatch"]) / 1e9 if prompt else None,
                            "executable_starts": starts(front_trace) + starts(daemon_trace) - before if traced else None,
                            "setup_phases": setup_phases,
                            "phases": phase_totals(phase_file, offset, submitted, state["connected"]) if phased else None}
                    assert item["connected_s"] >= item["dispatch_s"] > 0
                    if phased:
                        assert secret.encode() not in phase_file.read_bytes()
                    records.append(item)
                    print("SAMPLE " + json.dumps(item), flush=True)
                    command(binary, "--workspace", w, "close", temperature, "--yes", env=env)
            finally:
                if info:
                    try:
                        os.kill(info["pid"], signal.SIGTERM)
                    except ProcessLookupError:
                        pass
                if setup:
                    try:
                        setup.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        setup.kill()
                        setup.wait()
    groups = {}
    for temperature in ("cold", "warm"):
        groups[temperature] = {}
        for phase in ("dispatch_s", "connected_s", "prompt_s"):
            values = sorted(r[phase] for r in records if not r["traced"] and not r["phased"] and r["temperature"] == temperature and r[phase] is not None)
            groups[temperature][phase] = {"n": len(values), "p50": statistics.median(values), "p95": values[math.ceil(.95 * len(values)) - 1]}
    phases = {}
    for temperature in ("cold", "warm", "setup"):
        samples = [r["setup_phases"] if temperature == "setup" else r["phases"] for r in records if r["phased"] and (temperature == "setup" or r["temperature"] == temperature)]
        names = sorted({name for sample in samples for name in sample})
        phases[temperature] = {name: {"count_p50": statistics.median(s.get(name, {"count": 0})["count"] for s in samples),
                                      "ms_p50": statistics.median(s.get(name, {"ns": 0})["ns"] for s in samples) / 1e6} for name in names}
    print("RESULT " + json.dumps({"provenance": provenance, "groups": groups, "phases": phases, "samples": records, "final_load": os.getloadavg()}), flush=True)
    print("PASS production phase measurements; no historical hard latency cutoff", flush=True)
