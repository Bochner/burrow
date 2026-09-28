"""Opt-in one-byte remote echo through the existing shared-shell PTY lab."""

import base64
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import statistics
import subprocess
import time

from core.cmd.burrow.shell_lab import shell_checks


def measure_interactions(binary, root, env, workspace, burrow, options, wait, decoder, count, image, wheel):
    source = Path(os.environ["BUILD_WORKSPACE_DIRECTORY"])
    report = {
        "provenance": {
            "source_revision": subprocess.check_output(["git", "-C", source, "rev-parse", "HEAD"], text=True).strip(),
            "tracked_diff_sha256": hashlib.sha256(
                subprocess.check_output(["git", "-C", source, "diff", "HEAD", "--", "core", ".aspect"])
            ).hexdigest(),
            "lab_sources_sha256": {
                name: hashlib.sha256((source / "core/cmd/burrow" / name).read_bytes()).hexdigest()
                for name in ("interaction_latency.py", "shell_lab.py", "connection_lab.py")
            },
            "binary_sha256": hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
            "wheel_sha256": hashlib.sha256(Path(wheel).read_bytes()).hexdigest(),
            "pins": {
                name: (source / name).read_text()
                for name in ("MODULE.bazel", ".bazelversion", ".aspect/version.axl", ".bazelrc")
            },
            "image": image,
            "build": "Aspect fastbuild",
            "machine": platform.platform(),
            "cpu_count": os.cpu_count(),
            "cpu_model": next(
                line.split(":", 1)[1].strip()
                for line in Path("/proc/cpuinfo").read_text().splitlines()
                if line.startswith("model name")
            ),
            "memory": Path("/proc/meminfo").read_text().splitlines()[:3],
            "load_start": os.getloadavg(),
            "phases": "BURROW_PHASE_TRACE" in env,
            "workload": "one approved local SSH master, one retained shell and one TUI attachment; raw remote dd bs=1 with terminal echo disabled; sequential harmless bytes, no background output or faults; TUI controller followed by CLI controller with TUI observer",
            "geometry": "160x40 outer TUI, 98x35 retained shell",
            "clock": "CLOCK_MONOTONIC ns, same boot across processes",
            "observation": "begin immediately before PTY write or CLI Popen; visible at decoded VT cells; existing decoder uses up to 50ms wait/drain polling; CLI return observed independently at decoder-loop cadence",
            "correlation": "one outstanding byte, one shell; serial monotonic windows and frontend/owner PIDs; phases overlap and are not additive; no private input or screen content in evidence",
            "p95": "nearest rank sorted[ceil(0.95*n)-1]",
            "timeouts": "15s per interaction, 20s connection transitions, 60s CLI setup, 1800s whole lab",
        },
        "samples": [],
        "failures": [],
        "timeouts": 0,
    }
    tokens = []

    def interact(send, view, visible_wait, frontend_pid, shell):
        def private(action, request):
            result = subprocess.run(
                [binary, "--workspace", str(workspace), "session", action, "gateway", shell["id"], "--request-stdin"],
                input=json.dumps(request),
                capture_output=True,
                text=True,
                env=env,
                timeout=60,
            )
            assert result.returncode == 0, "private control failed"
            return json.loads(result.stdout)

        for case in ("tui-key", "cli-input-tui-observer"):
            # Split the marker so terminal echo of this setup is never readiness.
            marker = "TUI" if case == "tui-key" else "CLI"
            setup = f"stty raw -echo; printf '\\033[2J\\033[HECHO_{marker}_%s' READY; dd bs=1 count={count} 2>/dev/null; stty sane; printf '\\nDONE_%s\\n' ECHO\n"
            token = None
            if case == "tui-key":
                send("\x1b[200~" + setup.rstrip("\n") + "\x1b[201~\r")
            else:
                current = burrow(workspace, "session", "inspect", "gateway", shell["id"])
                token = private("takeover", {"generation": current["controlGeneration"], "label": "latency-lab"})[
                    "token"
                ]
                tokens.append(token)
                visible_wait("OBSERVE")
                private("input", {"token": token, "data": base64.b64encode(setup.encode()).decode()})
            visible_wait(f"ECHO_{marker}_READY")
            expected = f"ECHO_{marker}_READY"
            for index in range(count):
                byte = chr(ord("a") + index % 26)
                expected += byte
                # Vary arrival relative to the existing samplers; no cadence change.
                time.sleep((index % 7) * 0.011)
                item = {
                    "case": case,
                    "index": index,
                    "frontend_pid": frontend_pid,
                    "owner_pid": shell["ownerPID"],
                    "begin": time.monotonic_ns(),
                }
                report["samples"].append(item)
                process = None
                try:
                    if token is None:
                        send(byte)
                    else:
                        process = subprocess.Popen(
                            [
                                binary,
                                "--workspace",
                                str(workspace),
                                "session",
                                "input",
                                "gateway",
                                shell["id"],
                                "--request-stdin",
                            ],
                            stdin=subprocess.PIPE,
                            stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE,
                            env=env,
                        )
                        item["cli_pid"] = process.pid
                        process.stdin.write(
                            json.dumps({"token": token, "data": base64.b64encode(byte.encode()).decode()}).encode()
                        )
                        process.stdin.close()
                    deadline = time.monotonic() + 15
                    while "visible" not in item or (process is not None and "cli_return_observed" not in item):
                        assert time.monotonic() < deadline, "interaction timed out"
                        screen = view()
                        now = time.monotonic_ns()
                        if expected in screen:
                            item.setdefault("visible", now)
                        if process is not None and process.poll() is not None:
                            item.setdefault("cli_return_observed", now)
                    if process is not None:
                        assert process.returncode == 0, "private input failed"
                        result = json.loads(process.stdout.read())
                        assert (
                            result["acceptedBytes"] == 1
                            and not result.get("backpressure")
                            and not result.get("inputError")
                        )
                    item["end"] = time.monotonic_ns()
                finally:
                    if process is not None:
                        if process.poll() is None:
                            process.kill()
                            process.wait(timeout=5)
                        process.stdout.close()
                        process.stderr.close()
            visible_wait("DONE_ECHO")
            if token is not None:
                assert private("release", {"token": token})["released"]
        current = burrow(workspace, "session", "inspect", "gateway", shell["id"])
        assert current["state"] == "running" and current["controller"] == ""
        assert (current["columns"], current["rows"]) == (98, 35)

    try:
        burrow(workspace, "connect", "gateway", "127.0.0.1", "tester", *options)
        first = wait(lambda: s if (s := burrow(workspace, "inspect", "gateway"))["state"] == "connected" else None)
        shell_checks(binary, workspace, env, decoder, burrow, first, options, interaction=interact)
        burrow(workspace, "close", "gateway", "--yes")
        assert burrow(workspace, "connections") == []
        if report["provenance"]["phases"]:
            trace = root / "phases.jsonl"
            phases = [json.loads(line) for line in trace.read_text().splitlines()]
            assert all(token not in trace.read_text() for token in tokens), "private token in phase trace"
            assert all(set(p) == {"pid", "phase", "begin", "ns"} for p in phases)
            report["phase_trace_sha256"] = hashlib.sha256(trace.read_bytes()).hexdigest()
            for item in report["samples"]:
                item["phases"] = [
                    p for p in phases if p["begin"] <= item["end"] and p["begin"] + p["ns"] >= item["begin"]
                ]
                names = {p["phase"] for p in item["phases"]}
                required = {
                    "shell-private:input",
                    "shell-pty-write",
                    "shell-output-drain",
                    "shell-snapshot",
                    "shell-frontend-sample",
                }
                if item["case"] == "tui-key":
                    required |= {"shell-frontend-input", "shell-input-queue", "shell-input-rpc"}
                assert required <= names, ("missing interaction phases", required - names)

                # A serial, quiet one-shell workload gives temporal correlation
                # without adding IDs or payloads to the production protocol.
                def phase(name, pid, after):
                    return min(
                        (p for p in item["phases"] if p["phase"] == name and p["pid"] == pid and p["begin"] >= after),
                        key=lambda p: p["begin"],
                    )

                owner, frontend = item["owner_pid"], item["frontend_pid"]
                written = phase("shell-pty-write", owner, item["begin"])
                drained = phase("shell-output-drain", owner, written["begin"])
                snapshot = phase("shell-snapshot", owner, drained["begin"] + drained["ns"])
                refreshed = next(
                    p
                    for p in item["phases"]
                    if p["phase"] == "shell-refresh"
                    and p["pid"] == frontend
                    and p["begin"] <= snapshot["begin"]
                    and p["begin"] + p["ns"] >= snapshot["begin"] + snapshot["ns"]
                )
                sampled = phase("shell-frontend-sample", frontend, refreshed["begin"] + refreshed["ns"])
                accepted = phase("shell-frontend-accept", frontend, sampled["begin"] + sampled["ns"])
                assert accepted["begin"] <= item["visible"], "visible result preceded correlated frontend sample"
                item["landmarks"] = {
                    "pty_write_begin": written["begin"],
                    "pty_write_end": written["begin"] + written["ns"],
                    "output_read": drained["begin"],
                    "output_applied": drained["begin"] + drained["ns"],
                    "snapshot_begin": snapshot["begin"],
                    "snapshot_end": snapshot["begin"] + snapshot["ns"],
                    "refresh_end": refreshed["begin"] + refreshed["ns"],
                    "frontend_sample": sampled["begin"],
                    "frontend_accept": accepted["begin"],
                }
        report["cleanup"] = (
            "terminal restored; retained shell reaped; exact master preserved during samples, then explicitly closed"
        )
    except BaseException as error:
        report["failures"].append(
            {"type": type(error).__name__, "completed_samples": sum("visible" in s for s in report["samples"])}
        )
        report["timeouts"] += int(
            isinstance(error, (TimeoutError, subprocess.TimeoutExpired))
            or "timed out" in str(error)
            or "stalled" in str(error)
        )
        raise
    finally:
        report["load_end"] = os.getloadavg()
        report["summary_ms"] = {}
        for case in ("tui-key", "cli-input-tui-observer"):
            values = [
                (s["visible"] - s["begin"]) / 1e6 for s in report["samples"] if s["case"] == case and "visible" in s
            ]
            if values:
                report["summary_ms"][case] = {
                    "n": len(values),
                    "median": statistics.median(values),
                    "p95": sorted(values)[math.ceil(0.95 * len(values)) - 1],
                }
        output = Path(os.environ.get("TEST_UNDECLARED_OUTPUTS_DIR", root)) / "shell-interaction.json"
        output.write_text(json.dumps(report, indent=2) + "\n")
        print(
            "SHELL_INTERACTION "
            + json.dumps(
                {"artifact": str(output), "summary_ms": report["summary_ms"], "failures": report["failures"]}
                if "TEST_UNDECLARED_OUTPUTS_DIR" in os.environ
                else report
            ),
            flush=True,
        )
