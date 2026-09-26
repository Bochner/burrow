"""Bounded file latency evidence using the production CLI, PTY and pinned fixture."""

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
import statistics
import struct
import subprocess
import termios
import time

from core.cmd.burrow.file_history_lab import file_rpc


def measure_files(
    binary, root, env, workspace, burrow, options, wait, decoder, count, image, wheel, container, command
):
    source = Path(os.environ["BUILD_WORKSPACE_DIRECTORY"])
    report = {
        "provenance": {
            "source": subprocess.check_output(["git", "-C", source, "rev-parse", "HEAD"], text=True).strip(),
            "diff_sha256": hashlib.sha256(subprocess.check_output(["git", "-C", source, "diff", "HEAD"])).hexdigest(),
            "lab_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
            "binary_sha256": hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
            "wheel_sha256": hashlib.sha256(Path(wheel).read_bytes()).hexdigest(),
            "image": image,
            "build": "Aspect fastbuild; phase tracing enabled in both variants",
            "pins": {
                p: (source / p).read_text()
                for p in (
                    "MODULE.bazel",
                    ".bazelversion",
                    ".aspect/version.axl",
                    ".bazelrc",
                    "core/prototype_sdk/go.mod",
                )
            },
            "machine": platform.platform(),
            "cpus": os.cpu_count(),
            "load_start": os.getloadavg(),
            "cpu_model": next(
                line.split(":", 1)[1].strip()
                for line in Path("/proc/cpuinfo").read_text().splitlines()
                if line.startswith("model name")
            ),
            "memory": Path("/proc/meminfo").read_text().splitlines()[:3],
            "windows_path_entries": sum(bool(re.match(r"/mnt/[a-z]/", p)) for p in env["PATH"].split(os.pathsep)),
            "workload": "loopback Docker, same already-approved master per frontend, five ordinary entries; 32 links plus target; five files with unique unknown UID/GID per account sample",
            "cold": "cold-history: absent files chain before each sample; initial sample also has a cold account cache; burrow operation already exists for the connection; OS page caches not flushed",
            "warm": "established history and owner account cache; every explicit ls still opens a new SFTP subsystem",
            "observation": "CLOCK_MONOTONIC ns; CLI includes process startup; TUI starts before PTY Enter write and ends at decoded listing cells (up to 10ms polling plus decoder); TUI startup and entry browse excluded",
            "spans": "raw phases nest and overlap; phase sums/medians are not additive; ready-to-accept measures result delivery; accept-end-to-visible includes rendering, PTY and lab observation",
            "process_counts": "local frontend, Hovel setup and master-check launches from process phases; SFTP/account SSH starts cross-checked by remote wrapper; TUI windows may include background owner checks; remote shell/server descendants excluded",
            "p95": "nearest rank sorted[ceil(0.95*n)-1]; small n is descriptive",
            "timeouts": "CLI 60s; TUI 15s; account lookup 2s; SFTP owner 30s; lab 1800s",
        },
        "samples": [],
        "failures": [],
        "timeouts": 0,
    }
    phases = root / "phases.jsonl"
    config_path = "/config/sshd/sshd_config"
    original = command("docker", "exec", container, "cat", config_path)
    frontend = None
    outer = slave = None
    base = "/tmp/burrow-latency"
    try:
        # Fixture-only controls affect fresh account lookups without changing the owner.
        wrapper = root / "file-observe"
        wrapper.write_text("""#!/bin/sh
case "$SSH_ORIGINAL_COMMAND" in
  *sftp*)
    echo sftp >> /tmp/burrow-latency-sessions
    exec /usr/lib/ssh/sftp-server -e -l DEBUG3 2>> /tmp/burrow-latency-requests ;;
  *getent*)
    echo account >> /tmp/burrow-latency-sessions
    [ ! -e /tmp/burrow-latency-missing ] || exit 127
    [ ! -e /tmp/burrow-latency-slow ] || sleep 3 ;;
esac
exec /bin/sh -c "$SSH_ORIGINAL_COMMAND"
""")
        wrapper.chmod(0o755)
        command("docker", "cp", wrapper, container + ":/tmp/burrow-latency-observe")
        config = root / "file-sshd-config"
        config.write_text(original + "\nForceCommand /tmp/burrow-latency-observe\n")
        command("docker", "cp", config, container + ":" + config_path)
        command("docker", "exec", container, "sh", "-c", "kill -HUP $(cat /config/sshd.pid)")
        burrow(workspace, "connect", "file-latency", "127.0.0.1", "tester", *options)
        state = wait(lambda: s if (s := burrow(workspace, "inspect", "file-latency"))["state"] == "connected" else None)
        command(
            "docker",
            "exec",
            container,
            "sh",
            "-c",
            "mkdir -p " + " ".join(base + "/" + d for d in ("ordinary-a", "ordinary-b", "links", "accounts")) + "; "
            "for d in ordinary-a ordinary-b accounts; do for i in 1 2 3 4 5; do touch "
            + base
            + "/$d/file-$i; done; done; "
            "touch "
            + base
            + "/links/target; i=0; while [ $i -lt 32 ]; do ln -s target "
            + base
            + "/links/link-$i; i=$((i+1)); done",
        )

        def counts():
            sessions = command(
                "docker", "exec", container, "sh", "-c", "cat /tmp/burrow-latency-sessions 2>/dev/null || true"
            ).splitlines()
            requests = command(
                "docker", "exec", container, "sh", "-c", "cat /tmp/burrow-latency-requests 2>/dev/null || true"
            )
            return {
                "sftp": sessions.count("sftp"),
                "account": sessions.count("account"),
                "opendir": requests.count("opendir "),
                "readlink": requests.count("readlink "),
                "stat": requests.count("stat name "),
            }

        def record(front, case, path, index, action):
            before = counts()
            item = {"frontend": front, "case": case, "sample": index, "begin": time.monotonic_ns()}
            report["samples"].append(item)
            action(path, item)
            item["end"] = time.monotonic_ns()
            after = counts()
            item["remote_counts"] = {k: after[k] - before[k] for k in before}
            item["phases"] = [
                p
                for line in phases.read_text().splitlines()
                if item["begin"] <= (p := json.loads(line))["begin"] <= item["end"]
            ]
            item["totals"] = {}
            for p in item["phases"]:
                total = item["totals"].setdefault(p["phase"], {"ns": 0, "count": 0})
                total["ns"] += p["ns"]
                total["count"] += 1
            item["process_counts"] = {
                "frontend": int(front == "cli"),
                "hovel_setup": sum(
                    v["count"] for k, v in item["totals"].items() if k in ("hovel-process:op", "hovel-process:chain")
                ),
                "sftp_ssh": item["remote_counts"]["sftp"],
                "account_ssh": item["remote_counts"]["account"],
                "master_check_ssh": item["totals"].get("ssh-master-check", {}).get("count", 0),
            }
            for phase in (
                "files-owner",
                "files-sftp-setup",
                "files-readdir",
                "files-accounts",
                "files-sftp-teardown",
                "files-history-write",
            ):
                assert item["totals"][phase]["count"] == 1, (phase, item["totals"])
            if front == "tui":
                ready = next(p for p in item["phases"] if p["phase"] == "files-tui-ready")
                accepted = next(p for p in item["phases"] if p["phase"] == "files-tui-accept")
                item["delivery_ns"] = accepted["begin"] - ready["begin"]
                item["presentation_ns"] = item["end"] - accepted["begin"] - accepted["ns"]
            assert item["remote_counts"]["sftp"] == 1, item["remote_counts"]
            print(
                "FILE_SAMPLE "
                + json.dumps({k: item[k] for k in ("frontend", "case", "sample", "begin", "end", "process_counts")}),
                flush=True,
            )

        def cli(path, item):
            listing = burrow(workspace, "scp", "file-latency", "ls", path)
            item["entries"] = len(listing["entries"])
            item["notice"] = listing.get("notice", "")
            assert item["entries"] == (33 if path.endswith("links") else 5)
            if item["case"] in ("slow-accounts", "missing-accounts"):
                assert "numeric" in item["notice"] and listing["entries"][0]["owner"].isdigit()

        snapshot = file_rpc(workspace, "Snapshot", {})["State"]
        assert not any(
            c["name"] == "files"
            for op in snapshot.get("operations", [])
            if op["name"] == "burrow"
            for c in op["chains"]
        )
        record("cli", "initial-absent-history-accounts", base + "/ordinary-a", 0, cli)
        for index in range(count):
            for case in ("cold-history", "warm"):
                if case == "cold-history":
                    file_rpc(workspace, "DeleteChain", {"Operation": "burrow", "Chain": "files"})
                record("cli", case, base + "/ordinary-a", index, cli)
        for case in ("links", "slow-accounts", "missing-accounts"):
            flag = "/tmp/burrow-latency-" + case.split("-")[0]
            if case != "links":
                command("docker", "exec", container, "touch", flag)
            for index in range(count):
                if case != "links":
                    ident = str(20000 + index + (100 if case == "missing-accounts" else 0))
                    command("docker", "exec", container, "chown", "-R", ident + ":" + ident, base + "/accounts")
                record("cli", case, base + ("/links" if case == "links" else "/accounts"), index, cli)
            if case != "links":
                command("docker", "exec", container, "rm", flag)

        outer, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 160, 0, 0))

        def controlling():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)

        frontend = subprocess.Popen(
            [binary, "--workspace", str(workspace), "tui"],
            env=env,
            stdin=slave,
            stdout=slave,
            stderr=slave,
            preexec_fn=controlling,
        )
        output = bytearray()

        def visible(needle):
            deadline = time.monotonic() + 15
            while time.monotonic() < deadline:
                if select.select([outer], [], [], 0.01)[0]:
                    output.extend(os.read(outer, 65536))
                    screen = subprocess.run(
                        [decoder, "160", "40"], input=output, capture_output=True, check=True
                    ).stdout.decode()
                    if all(text in screen for text in needle):
                        return
                assert frontend.poll() is None
            raise TimeoutError("file presentation timed out")

        visible(["file-latency"])
        os.write(outer, b"scp file-latency\r")
        visible(["LISTING /config"])

        def tui(path, item):
            os.write(outer, ("ls " + path + "\r").encode())
            visible(["LISTING " + path, "file-5"])

        for index in range(count):
            for case, suffix in (("cold-history", "ordinary-a"), ("warm", "ordinary-b")):
                if case == "cold-history":
                    file_rpc(workspace, "DeleteChain", {"Operation": "burrow", "Chain": "files"})
                record("tui", case, base + "/" + suffix, index, tui)
        os.write(outer, b"back\r")
        visible(["SAVED CONNECTIONS"])
        os.write(outer, b"quit\r")
        visible(["Keep running"])
        os.write(outer, b"\r")
        assert frontend.wait(timeout=10) == 0
        assert burrow(workspace, "inspect", "file-latency")["masterPID"] == state["masterPID"]
        # Raw SSH is a deliberately smaller-contract reference, outside the samples.
        report["raw_ssh_ns"] = []
        for _ in range(count):
            begin = time.monotonic_ns()
            command(
                "ssh",
                "-F",
                "/dev/null",
                "-S",
                state["socket"],
                "-o",
                "ControlMaster=no",
                "-o",
                "ProxyCommand=/usr/bin/false",
                "-o",
                "BatchMode=yes",
                "unused",
                "ls -la " + base + "/ordinary-a",
            )
            report["raw_ssh_ns"].append(time.monotonic_ns() - begin)
        burrow(workspace, "close", "file-latency", "--yes")
        report["cleanup"] = "TUI exited; same master during samples; connections closed explicitly"
    except BaseException as error:
        report["failures"].append({"type": type(error).__name__, "samples_started": len(report["samples"])})
        report["timeouts"] += int(isinstance(error, (TimeoutError, subprocess.TimeoutExpired)))
        raise
    finally:
        if frontend is not None and frontend.poll() is None:
            frontend.kill()
            frontend.wait()
        for fd in (outer, slave):
            if fd is not None:
                os.close(fd)
        report["load_end"] = os.getloadavg()
        groups = {}
        for item in report["samples"]:
            if "end" not in item:
                continue
            prefix = item["frontend"] + ":" + item["case"]
            for phase, ns in {
                "total": item["end"] - item["begin"],
                **{k: v["ns"] for k, v in item.get("totals", {}).items()},
                **{k: item[k] for k in ("delivery_ns", "presentation_ns") if k in item},
            }.items():
                groups.setdefault(prefix + ":" + phase, []).append(ns / 1e6)
        report["summary_ms"] = {
            k: {"n": len(v), "median": statistics.median(v), "p95": sorted(v)[math.ceil(0.95 * len(v)) - 1]}
            for k, v in groups.items()
        }
        artifact = Path(os.environ.get("TEST_UNDECLARED_OUTPUTS_DIR", root)) / "file-latency.json"
        artifact.write_text(json.dumps(report, indent=2) + "\n")
        print(
            "FILE_LATENCY "
            + json.dumps(
                {"artifact": str(artifact), "failures": report["failures"]}
                if "TEST_UNDECLARED_OUTPUTS_DIR" in os.environ
                else report
            ),
            flush=True,
        )
