"""Bounded, opt-in attachment scaling through the existing real SSH/TUI lab."""

import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import time

from core.cmd.burrow.shell_lab import shell_checks


def measure_attachments(binary, root, env, workspace, burrow, options, wait, decoder, count, image, wheel, daemons):
    source = Path(os.environ["BUILD_WORKSPACE_DIRECTORY"])
    traced = "BURROW_ATTACHMENT_TRACE" in env
    interval = 5
    ticks = os.sysconf("SC_CLK_TCK")
    other = root / "empty"
    # Register this disposable daemon with the existing lab cleanup before the
    # TUI opens it; even a failed measurement must not leave a daemon behind.
    daemons.append(burrow(other, "--offline", "status")["pid"])
    report = {
        "provenance": {
            "source_revision": subprocess.check_output(["git", "-C", source, "rev-parse", "HEAD"], text=True).strip(),
            "tracked_diff_sha256": hashlib.sha256(
                subprocess.check_output(["git", "-C", source, "diff", "HEAD", "--", "core", ".aspect"])
            ).hexdigest(),
            "lab_sources_sha256": {
                name: hashlib.sha256((source / "core/cmd/burrow" / name).read_bytes()).hexdigest()
                for name in ("attachment_cost.py", "connection_lab.py", "shell_lab.py")
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
            "traced": traced,
            "interval_seconds": interval,
            "repeats": count,
            "cpu_clock_ticks_per_second": ticks,
            "cpu": "process utime+stime from /proc/PID/stat; frontend and sum of retained shell owners separately, excludes Hovel daemons, connection manager, SSH children, remote workload and VT decoder; percent of one logical CPU",
            "counts": "completed rpcBody attempts by public method (including GetDaemonInfo identity checks); ownerCommand returns by verb; frontend PID only, bounded by monotonic completion timestamps; no counts inferred when tracing disabled",
            "bytes": "UTF-8 byte length of ownerCommand snapshot Stdout JSON payload; excludes outer RPC JSON escaping/envelope, HTTP headers and Unix transport; not wire bytes",
            "workload": "one local approved OpenSSH master; 1,2,4 retained shells, each with one TUI controller attachment; idle then controlled 10-line/s ASCII output per shell; visible last shell, all tabs hidden in same-workspace management, then all tabs in background workspace while empty workspace management is focused",
            "geometry": "160x40 outer TUI; all retained shells 98x35",
            "collection": "drain and decode real PTY throughout each interval; read process ticks at boundaries; no CLI probes inside observation windows; trace read/aggregation after each window; 1s settle per placement; enabled vs disabled runs bound overhead, not a causal subtraction",
            "limits": "fixed ascending attachment counts and placement order, warmed local fixture, at most 5 repeats, 1800s outer lab timeout; CPU tick quantization and boundary skew; no product tab-count/CPU guarantee",
        },
        "samples": [],
        "failures": [],
        "timeouts": 0,
    }

    def cpu(pid):
        fields = Path(f"/proc/{pid}/stat").read_text().rsplit(") ", 1)[1].split()
        return {"ticks": int(fields[11]) + int(fields[12]), "starttime": fields[19]}

    def exercise(send, view, visible_wait, command, frontend):
        def click(row):
            send(f"\x1b[<0;5;{row}M\x1b[<0;5;{row}m")

        def management():
            click(4)
            visible_wait("ACTIVE SSH CONNECTIONS")

        def select(number):
            # Sidebar focus selects the retained tab without release/takeover.
            row = next(i + 1 for i, line in enumerate(view().splitlines()) if f"#{number} gateway" in line[:25])
            click(row)
            visible_wait(f"SSH: gateway #{number} · CONTROL")
            visible_wait("snapshot-current")

        def shells():
            result = burrow(workspace, "session", "list", "gateway")
            assert all(
                s["state"] == "running"
                and s["controller"] == f"tui-{frontend}"
                and (s["columns"], s["rows"]) == (98, 35)
                for s in result
            )
            return result

        management()
        send("\x1bn")
        visible_wait("Workspace name")
        send(other.name + "\t" + str(other.parent) + "\r")
        visible_wait("No connections")
        management()
        for attachments in (1, 2, 4):
            while len(shells()) < attachments:
                command("shell gateway", "CONTROL")
                management()
            before = shells()
            owners = sorted({s["ownerPID"] for s in before})
            generations = {s["id"]: s["controlGeneration"] for s in before}
            for workload in ("idle", "output"):
                if workload == "output":
                    for number in range(1, attachments + 1):
                        select(number)
                        # Bracketed paste prevents command-line echo from being
                        # mistaken for output; each counter is checked on return.
                        setup = "i=0; while [ $i -lt 1800 ]; do printf 'LOAD_%04d\\n' $i; i=$((i+1)); sleep 0.1; done"
                        send("\x1b[200~" + setup + "\x1b[201~\r")
                        visible_wait("LOAD_0001")
                for placement in ("visible", "hidden", "background-workspace"):
                    if placement == "visible":
                        select(attachments)
                    elif placement == "hidden":
                        management()
                    else:
                        click(5)
                        visible_wait("No connections")
                    settle = time.monotonic() + 1
                    while time.monotonic() < settle:
                        view()
                    for repeat in range(count):
                        trace = root / "attachments.jsonl"
                        offset = trace.stat().st_size if traced else 0
                        processes = {"frontend": [frontend], "owners": owners}
                        start_cpu = {role: {pid: cpu(pid) for pid in pids} for role, pids in processes.items()}
                        begin = time.monotonic_ns()
                        end = begin + interval * 1_000_000_000
                        while time.monotonic_ns() < end:
                            view()
                        end = time.monotonic_ns()
                        end_cpu = {role: {pid: cpu(pid) for pid in pids} for role, pids in processes.items()}
                        item = {
                            "attachments": attachments,
                            "placement": placement,
                            "workload": workload,
                            "repeat": repeat,
                            "frontend_pid": frontend,
                            "owner_pids": owners,
                            "begin": begin,
                            "end": end,
                            "cpu": {},
                        }
                        for role in start_cpu:
                            assert all(
                                start_cpu[role][pid]["starttime"] == end_cpu[role][pid]["starttime"]
                                for pid in processes[role]
                            ), "process replaced during sample"
                            delta = sum(
                                end_cpu[role][pid]["ticks"] - start_cpu[role][pid]["ticks"] for pid in processes[role]
                            )
                            item["cpu"][role] = {
                                "start": start_cpu[role],
                                "end": end_cpu[role],
                                "seconds": delta / ticks,
                                "percent": delta / ticks / ((end - begin) / 1e9) * 100,
                            }
                        if traced:
                            with trace.open("rb") as stream:
                                stream.seek(offset)
                                records = [json.loads(line) for line in stream.read().splitlines()]
                            records = [r for r in records if r["pid"] == frontend and begin <= r["at"] <= end]
                            assert all(set(r) == {"pid", "operation", "at", "bytes"} for r in records)
                            item["operations"] = {}
                            for record in records:
                                item["operations"][record["operation"]] = (
                                    item["operations"].get(record["operation"], 0) + 1
                                )
                            item["snapshot_payload_bytes"] = sum(
                                r["bytes"] for r in records if r["operation"] == "command:snapshot"
                            )
                            assert item["snapshot_payload_bytes"] > 0
                            assert {
                                "rpc:GetDaemonInfo",
                                "rpc:ListSessions",
                                "rpc:RunSessionCommand",
                                "command:inspect",
                                "command:snapshot",
                            } <= item["operations"].keys(), item["operations"]
                        report["samples"].append(item)
                        print("ATTACHMENT_SAMPLE " + json.dumps(item), flush=True)
                after = shells()
                assert {s["id"]: s["controlGeneration"] for s in after} == generations
                # Background output must still advance and remain navigable.
                for number in range(1, attachments + 1):
                    select(number)
                    if workload == "output":
                        visible_wait("LOAD_")
                        send("\x03")
                        visible_wait(":~$")
                        marker = f"RETURN_{attachments}_{number}"
                        send(f"printf '{marker}_%s\\n' OK\r")
                        visible_wait(marker + "_OK")
                management()
        select(1)

    try:
        burrow(workspace, "connect", "gateway", "127.0.0.1", "tester", *options)
        first = wait(lambda: s if (s := burrow(workspace, "inspect", "gateway"))["state"] == "connected" else None)
        shell_checks(binary, workspace, env, decoder, burrow, first, options, attachments=exercise)
        burrow(workspace, "close", "gateway", "--yes")
        assert burrow(workspace, "connections") == []
        report["cleanup"] = (
            "terminal restored; Keep running released claims, retained shells then explicitly closed, original master preserved until final close"
        )
    except BaseException as error:
        report["failures"].append({"type": type(error).__name__, "completed_samples": len(report["samples"])})
        report["timeouts"] += int(isinstance(error, (TimeoutError, subprocess.TimeoutExpired)))
        raise
    finally:
        report["load_end"] = os.getloadavg()
        output = Path(os.environ.get("TEST_UNDECLARED_OUTPUTS_DIR", root)) / "shell-attachment-cost.json"
        output.write_text(json.dumps(report, indent=2) + "\n")
        print(
            "ATTACHMENT_COST "
            + json.dumps(
                {"artifact": str(output), "samples": len(report["samples"]), "failures": report["failures"]}
                if "TEST_UNDECLARED_OUTPUTS_DIR" in os.environ
                else report
            ),
            flush=True,
        )
