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


def measure_shells(
    binary, root, env, workspace, burrow, options, wait, decoder, count, image, wheel, processes, growing
):
    from core.cmd.burrow.sessions_lab import session_startup
    from core.cmd.burrow.shell_lab import shell_checks

    report = {
        "provenance": {
            "machine": platform.platform(),
            "cpu_count": os.cpu_count(),
            "load_start": os.getloadavg(),
            "binary_sha256": hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
            "wheel_sha256": hashlib.sha256(Path(wheel).read_bytes()).hexdigest(),
            "image": image,
            "build": "Aspect fastbuild",
            "phases": "BURROW_PHASE_TRACE" in env,
            "process_trace": processes,
            "growing_tabs": growing,
            "windows_path_entries": sum(bool(re.match(r"/mnt/[a-z]/", p)) for p in env["PATH"].split(os.pathsep)),
            "workload": "local disposable SSH; key authentication; one approved master; sequential CLI shells closed after each; TUI first shell retained as background observer; later shells closed after each unless growing_tabs=true",
            "geometry": "CLI 80x24; TUI outer 160x40, shell 98x35",
            "human_wait": "CLI review-to-submission includes synthetic 200ms; TUI shell command has no review dialog",
            "observation": "CLOCK_MONOTONIC ns; CLI snapshot prompt is not paint; TUI decoded VT prompt cells include up to 50ms read/drain polling and decoder overhead",
            "p95": "nearest rank, sorted[ceil(0.95*n)-1]; n=1 is descriptive only",
            "timeouts": "CLI 60s; connection transition 20s; TUI prompt 15s; whole lab 1800s",
        },
        "connections": [],
        "samples": [],
        "failures": [],
        "timeouts": 0,
    }
    source = Path(os.environ["BUILD_WORKSPACE_DIRECTORY"])
    report["provenance"].update(
        source_revision=subprocess.check_output(["git", "-C", source, "rev-parse", "HEAD"], text=True).strip(),
        tracked_diff_sha256=hashlib.sha256(
            subprocess.check_output(["git", "-C", source, "diff", "HEAD", "--", "core", ".aspect"])
        ).hexdigest(),
        pins={
            name: (source / name).read_text()
            for name in ("MODULE.bazel", ".bazelversion", ".aspect/version.axl", ".bazelrc")
        },
        cpu_model=next(
            (
                line.split(":", 1)[1].strip()
                for line in Path("/proc/cpuinfo").read_text().splitlines()
                if line.startswith("model name")
            ),
            "unknown",
        ),
        memory=Path("/proc/meminfo").read_text().splitlines()[:3],
        directory_shape="fresh disposable workspace; zero retained shell records before CLI; CLI closed records kept by Hovel; TUI previous shells retained",
    )
    try:
        if processes:
            report["initialization"] = []
            for executable, arguments in (
                (binary, ["--help"]),
                (str(root / "cache/burrow/hovel/0.4.2/hovel"), ["version"]),
            ):
                for path_mode in ("inherited", "linux-only"):
                    diagnostic_env = dict(env, GODEBUG="inittrace=1")
                    if path_mode == "linux-only":
                        diagnostic_env["PATH"] = os.pathsep.join(
                            p for p in env["PATH"].split(os.pathsep) if not re.match(r"/mnt/[a-z]/", p)
                        )
                    for _ in range(3):
                        begin = time.monotonic_ns()
                        result = subprocess.run(
                            [executable, *arguments], env=diagnostic_env, capture_output=True, text=True, timeout=15
                        )
                        assert result.returncode == 0
                        # Only Go runtime metadata; no arbitrary stderr or output.
                        initialization = re.findall(
                            r"^init (\S+) @([\d.]+) ms, ([\d.]+) ms clock, (\d+) bytes, (\d+) allocs$",
                            result.stderr,
                            re.M,
                        )
                        assert initialization
                        report["initialization"].append(
                            {
                                "executable": Path(executable).name,
                                "path": path_mode,
                                "ns": time.monotonic_ns() - begin,
                                "packages": [
                                    {
                                        "package": p,
                                        "at_ms": float(at),
                                        "ms": float(ms),
                                        "bytes": int(b),
                                        "allocs": int(a),
                                    }
                                    for p, at, ms, b, a in initialization
                                ],
                            }
                        )
        for name, temperature in (("cold", "cold-manager"), ("gateway", "warm-manager")):
            item = {"case": temperature, "review_begin": time.monotonic_ns()}
            report["connections"].append(item)
            review = burrow(workspace, "connect", name, "127.0.0.1", "tester", *options[:-1])
            item["review_ready"] = time.monotonic_ns()
            assert review["review"]
            time.sleep(0.2)
            item["begin"] = time.monotonic_ns()
            burrow(workspace, "connect", name, "127.0.0.1", "tester", *options)
            item["dispatch_return"] = time.monotonic_ns()

            def connected():
                state = burrow(workspace, "inspect", name)
                assert state["state"] != "lost"
                return state if state["state"] == "connected" else None

            state = wait(connected)
            item["connected"] = state["connected"]
            item["connected_observed"] = time.monotonic_ns()
            if name == "cold":
                burrow(workspace, "close", name, "--yes")
        session_startup(burrow, workspace, report["samples"], count, wait)
        shell_checks(
            binary,
            workspace,
            env,
            decoder,
            burrow,
            state,
            options,
            startup=report["samples"],
            samples=count,
            growing=growing,
        )
        burrow(workspace, "close", "gateway", "--yes")
        assert burrow(workspace, "connections") == []
        correlate_shell_phases(report, root)
        report["cleanup"] = "shell clients reaped; master unchanged during samples; all connections explicitly closed"
    except BaseException as error:
        # No exception arguments: subprocess failures can contain private input.
        report["failures"].append(
            {
                "type": type(error).__name__,
                "completed_samples": sum("visible" in r or "prompt_observed" in r for r in report["samples"]),
            }
        )
        report["timeouts"] += int(
            isinstance(error, (TimeoutError, subprocess.TimeoutExpired))
            or "timed out" in str(error)
            or "stalled" in str(error)
        )
        raise
    finally:
        report["load_end"] = os.getloadavg()
        groups = {}
        for item in report["connections"] + report["samples"]:
            for end, start in (
                ("review_ready", "review_begin"),
                ("begin", "review_ready"),
                ("dispatch_return", "begin"),
                ("connected", "begin"),
                ("prompt_observed", "begin"),
                ("visible", "begin"),
            ):
                if end in item and start in item:
                    groups.setdefault(item["case"] + ":" + end + "-" + start, []).append(
                        (item[end] - item[start]) / 1e6
                    )
            for phase, total in item.get("phase_totals", {}).items():
                groups.setdefault(item["case"] + ":phase-total:" + phase, []).append(total["ns"] / 1e6)
        report["summary_ms"] = {
            name: {"n": len(v), "median": statistics.median(v), "p95": sorted(v)[math.ceil(0.95 * len(v)) - 1]}
            for name, v in groups.items()
        }
        output = Path(os.environ.get("TEST_UNDECLARED_OUTPUTS_DIR", root)) / "shell-startup.json"
        output.write_text(json.dumps(report, indent=2) + "\n")
        print(
            "SHELL_STARTUP "
            + json.dumps(
                {"artifact": str(output), "summary_ms": report["summary_ms"], "failures": report["failures"]}
                if "TEST_UNDECLARED_OUTPUTS_DIR" in os.environ
                else report
            ),
            flush=True,
        )


def correlate_shell_phases(report, root):
    phase_file = root / "phases.jsonl"
    if report["provenance"]["phases"]:
        assert phase_file.exists(), "requested startup phases missing"
    if phase_file.exists():
        phases = [json.loads(line) for line in phase_file.read_text().splitlines()]
        report["phase_trace_sha256"] = hashlib.sha256(phase_file.read_bytes()).hexdigest()
        for item in report["connections"] + report["samples"]:
            if "begin" not in item:
                continue
            high = item.get("visible", item.get("prompt_observed", item.get("connected_observed", item["begin"])))
            window = [p for p in phases if item["begin"] <= p["begin"] <= high]
            throws = [p for p in window if p["phase"].startswith("manager-throw:")]
            # Keep raw boundary/setup spans and aggregate other caller RPCs.
            # Background polling overlaps startup; these are not additive costs.
            setup = {
                "call:" + name
                for name in ("CreateOperation", "CreateChain", "AddModule", "AddTarget", "SetChainConfig")
            }
            item["phases"] = [
                p
                for p in window
                if p["phase"].startswith(("shell-", "module", "manager-throw:", "hovel-", "dispatch-lock"))
                or (
                    p["phase"] in setup
                    and any(p["pid"] == t["pid"] and t["begin"] <= p["begin"] < t["begin"] + t["ns"] for t in throws)
                )
            ]
            item["caller_rpc_totals"] = {}
            for p in window:
                if p["phase"].startswith("call:") and throws and p["pid"] == throws[0]["pid"]:
                    entry = item["caller_rpc_totals"].setdefault(p["phase"], {"count": 0, "ns": 0})
                    entry["count"] += 1
                    entry["ns"] += p["ns"]
            item["phase_totals"] = {}
            for p in item["phases"]:
                entry = item["phase_totals"].setdefault(p["phase"], {"count": 0, "ns": 0})
                entry["count"] += 1
                entry["ns"] += p["ns"]
            if item["case"] in ("cli", "tui-first", "tui-additional") and not report["failures"]:
                names = [t["phase"] for t in throws]
                assert names == ["manager-throw:shell-prepare", "manager-throw:shell-start"], names
                for name in (
                    "shell-sdk-allocation",
                    "shell-adoption-observation",
                    "shell-ssh-start",
                    "shell-first-output",
                ):
                    assert item["phase_totals"][name]["count"] == 1, (item["case"], name)
                prepare, start = throws
                assert prepare["begin"] + prepare["ns"] <= start["begin"]
                adoption = next(p for p in item["phases"] if p["phase"] == "shell-adoption-observation")
                ssh = next(p for p in item["phases"] if p["phase"] == "shell-ssh-start")
                output = next(p for p in item["phases"] if p["phase"] == "shell-first-output")
                assert (
                    prepare["begin"] + prepare["ns"]
                    <= adoption["begin"]
                    < adoption["begin"] + adoption["ns"]
                    <= ssh["begin"]
                )
                assert ssh["pid"] == output["pid"] == item["owner_pid"]
                assert ssh["begin"] + ssh["ns"] <= output["begin"] + output["ns"] <= high


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
    subprocess.run(
        ["docker", "exec", "-i", container, "chpasswd"],
        input=f"tester:{secret}\n".encode(),
        check=True,
        capture_output=True,
    )
    records = []
    tracing = ["strace", "--kill-on-exit", "-f", "-e", "trace=process", "-o"]
    phase_file = root / "phases.jsonl"
    phase_env = dict(env, BURROW_PHASE_TRACE=str(phase_file))
    provenance = {
        "machine": platform.platform(),
        "load": os.getloadavg(),
        "binary_sha256": hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
        "build": "Aspect fastbuild",
        "synthetic_response_delay_s": 0.02,
        "submission": "production CLI process launch; recap approval supplied by --yes",
        "prompt": "first private CLI prompt received on PTY with echo disabled",
        "process_counts": "successful execve starts, frontend plus daemon descendants; includes observer-induced SSH checks",
        "phases": "BURROW_PHASE_TRACE totals per phase name across frontend, daemon-launched adapter and manager processes; nested phases overlap, so totals are not additive",
    }
    print("PROVENANCE " + json.dumps(provenance), flush=True)

    def starts(path):
        return len(re.findall(r"(?:execve\(.*|<\.\.\. execve resumed>.*)= 0$", path.read_text(), re.M))

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
                        setup = subprocess.Popen(
                            [*tracing, str(daemon_trace), binary, "--workspace", str(w), "--offline", "status"],
                            env=env,
                            stdout=stream,
                            stderr=subprocess.DEVNULL,
                        )

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
                    args = [
                        binary,
                        "--workspace",
                        str(w),
                        "connect",
                        temperature,
                        "127.0.0.1",
                        "tester",
                        "--port",
                        str(port),
                        "--yes",
                    ]
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

                        p = subprocess.Popen(
                            args,
                            env=sample_env,
                            stdin=slave,
                            stdout=subprocess.PIPE,
                            stderr=slave,
                            preexec_fn=controlling,
                        )
                        transcript = bytearray()
                        answered = saved = False
                        try:
                            deadline = time.monotonic() + 50
                            while p.poll() is None:
                                assert time.monotonic() < deadline, "private prompt measurement timed out"
                                if select.select([outer], [], [], 0.02)[0]:
                                    transcript.extend(os.read(outer, 65536))
                                if not answered and b"SSH password" in transcript:
                                    assert not termios.tcgetattr(slave)[3] & termios.ECHO
                                    prompt = time.monotonic_ns()
                                    time.sleep(0.02)
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
                    item = {
                        "temperature": temperature,
                        "auth": "password" if password else "key",
                        "traced": traced,
                        "phased": phased,
                        "setup_s": setup_s,
                        "dispatch_s": (state["dispatch"] - submitted) / 1e9,
                        "connected_s": (state["connected"] - submitted) / 1e9,
                        "prompt_s": (prompt - state["dispatch"]) / 1e9 if prompt else None,
                        "executable_starts": starts(front_trace) + starts(daemon_trace) - before if traced else None,
                        "setup_phases": setup_phases,
                        "phases": phase_totals(phase_file, offset, submitted, state["connected"]) if phased else None,
                    }
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
            values = sorted(
                r[phase]
                for r in records
                if not r["traced"] and not r["phased"] and r["temperature"] == temperature and r[phase] is not None
            )
            groups[temperature][phase] = {
                "n": len(values),
                "p50": statistics.median(values),
                "p95": values[math.ceil(0.95 * len(values)) - 1],
            }
    phases = {}
    for temperature in ("cold", "warm", "setup"):
        samples = [
            r["setup_phases"] if temperature == "setup" else r["phases"]
            for r in records
            if r["phased"] and (temperature == "setup" or r["temperature"] == temperature)
        ]
        names = sorted({name for sample in samples for name in sample})
        phases[temperature] = {
            name: {
                "count_p50": statistics.median(s.get(name, {"count": 0})["count"] for s in samples),
                "ms_p50": statistics.median(s.get(name, {"ns": 0})["ns"] for s in samples) / 1e6,
            }
            for name in names
        }
    print(
        "RESULT "
        + json.dumps(
            {
                "provenance": provenance,
                "groups": groups,
                "phases": phases,
                "samples": records,
                "final_load": os.getloadavg(),
            }
        ),
        flush=True,
    )
    print("PASS production phase measurements; no historical hard latency cutoff", flush=True)
