"""Adapt Burrow's declared test evidence to Hovel's report model and application."""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
from dataclasses import asdict
from urllib.parse import unquote, urlsplit

import hovel_testreport as upstream

SUITES = (
    "portable",
    "lifecycle",
    "files",
    "reverse",
    "shell",
    "chains",
    "reports",
    "automation",
    "follow",
    "runs",
    "hovel",
)
HOVEL_TARGETS = {
    "//core/launch:hovel_wal_test",
    "//core/prototype_manager:check",
    "//core/prototype_manager:consumer_check",
}
PRODUCTION = re.compile(r"^core/(cmd/burrow|connection|launch|reports|agent|terminal)/[^/]+\.go$")
DOCUMENTATION_TARGETS = {"//docs/tools/docs:site_test", "//docs/tools/docs:stage_site_test"}


def documentation_path(name):
    path = Path(name)
    return name == "README.md" or (
        name.startswith("docs/research/")
        and path.suffix == ".md"
        or name.startswith("docs/site/src/content/")
        and path.suffix == ".html"
    )


def reusable_source(tested, current, documentation=False):
    if tested["dirty"] or current["dirty"]:
        return False
    return (
        tested["applicationTree"] == current["applicationTree"]
        if documentation
        else tested["gitTree"] == current["gitTree"]
    )


def digest(data):
    return hashlib.sha256(data).hexdigest()


def write_json(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2, sort_keys=True) + "\n")


def snapshot(root):
    def git(*args):
        return subprocess.check_output(["git", "-C", str(root), *args])

    files = {
        entry.split("\t", 1)[1]: entry.split("\t", 1)[0]
        for entry in git("ls-tree", "-r", "-z", "HEAD").decode().split("\0")
        if entry
    }
    return {
        "commit": git("rev-parse", "HEAD").decode().strip(),
        "gitTree": git("rev-parse", "HEAD^{tree}").decode().strip(),
        "applicationTree": digest(
            json.dumps(
                {name: value for name, value in files.items() if not documentation_path(name)}, sort_keys=True
            ).encode()
        ),
        "dirty": bool(git("status", "--porcelain", "--untracked-files=normal")),
        "files": files,
    }


def suite_directory(root, suite):
    directory = root / ".report-input" / suite
    if directory.is_symlink() or directory.parent.is_symlink():
        raise ValueError("refusing symlink evidence directory")
    return directory


def begin(root, suite):
    directory = suite_directory(root, suite)
    if directory.exists():
        archive = directory.parent / "archive"
        if archive.is_symlink():
            raise ValueError("refusing symlink evidence archive")
        archive.mkdir(exist_ok=True)
        directory.rename(archive / (suite + "-" + datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")))
    directory.mkdir(parents=True)
    write_json(
        directory / "start.json",
        {
            "source": {
                "commit": subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
            },
            "started": datetime.now(timezone.utc).isoformat(),
            "environment": {
                "os": platform.platform(),
                "python": platform.python_version(),
                "runID": os.environ.get("GITHUB_RUN_ID", ""),
                "runAttempt": os.environ.get("GITHUB_RUN_ATTEMPT", ""),
            },
        },
    )


def collect(root, suite, exit_code):
    directory = suite_directory(root, suite)
    metadata = json.loads((directory / "start.json").read_text())
    targets, selected, finished = {}, set(), False
    bep = directory / "bep.json"
    for line in bep.read_text().splitlines() if bep.exists() else []:
        event = json.loads(line)
        identity = event.get("id", {})
        if "expanded" in event:
            selected.update(
                child["targetConfigured"]["label"] for child in event.get("children", []) if "targetConfigured" in child
            )
        target = None
        for kind in ("targetConfigured", "testResult", "testSummary"):
            if kind in identity and (kind != "targetConfigured" or event.get("configured", {}).get("testSize")):
                label = identity[kind]["label"]
                target = targets.setdefault(label, {"label": label, "status": "MISSING", "attempts": []})
                if kind == "targetConfigured":
                    target["tags"] = event["configured"].get("tag", [])
                    target["kind"] = event["configured"].get("targetKind", "")
        if "testResult" in event:
            result = event["testResult"]
            attempt = {
                "identity": identity["testResult"],
                "status": result.get("status", "MISSING"),
                "files": [],
                "cached": bool(result.get("cachedLocally") or result.get("executionInfo", {}).get("cachedRemotely")),
                "duration": upstream.parse_float(str(result.get("testAttemptDuration", "")).removesuffix("s"))
                or upstream.parse_float(result.get("testAttemptDurationMillis")) / 1000,
            }
            for item in result.get("testActionOutput", []):
                if item["name"] not in ("test.log", "test.xml"):
                    continue
                uri = urlsplit(item.get("uri", ""))
                if uri.scheme != "file" or uri.netloc:
                    continue  # Remote/missing artifacts remain visibly absent.
                source = Path(unquote(uri.path))
                if not source.is_file():
                    continue
                data = source.read_bytes()
                name = (
                    "evidence/"
                    + digest(json.dumps(attempt["identity"], sort_keys=True).encode())[:20]
                    + "-"
                    + item["name"]
                    + ".txt"
                )
                destination = directory / name
                destination.parent.mkdir(exist_ok=True)
                destination.write_bytes(data)
                attempt["files"].append({"path": name, "kind": item["name"]})
            target["attempts"].append(attempt)
        if "testSummary" in event:
            target["summaryStatus"] = event["testSummary"].get("overallStatus", "MISSING")
            target["expectedRuns"] = event["testSummary"].get("totalRunCount", 0)
        if "finished" in event:
            finished = "exitCode" in event["finished"] and event["finished"]["exitCode"].get("code", 0) == 0
    for label in selected:
        targets.setdefault(label, {"label": label, "status": "MISSING", "attempts": []})
    for target in targets.values():
        target["status"] = target_status(target, suite)
    status = (
        "PASSED"
        if exit_code == 0
        and finished
        and selected == targets.keys()
        and selected
        and all(t["status"] == "PASSED" for t in targets.values())
        else "FAILED"
    )
    result = metadata | {
        "schemaVersion": 1,
        "suite": suite,
        "status": status,
        "exitCode": exit_code,
        "finished": finished,
        "selectedTargets": sorted(selected),
        "targets": sorted(targets.values(), key=lambda target: target["label"]),
    }
    coverage = directory / "coverage.lcov"
    if coverage.exists():
        result["coverage"] = {"path": "coverage.lcov"}
    write_json(directory / "suite.json", result)


def target_status(target, suite):
    attempts = target["attempts"]
    identities = {json.dumps(attempt["identity"], sort_keys=True) for attempt in attempts}
    if not attempts or len(identities) != len(attempts) or len(attempts) != target.get("expectedRuns", 0):
        return "MISSING"
    if suite in ("all", "portable") and target["label"] in (
        "//core/cmd/burrow:setup_test",
        "//core/cmd/burrow:terminal_test",
    ):
        if len(attempts) != 3 or {attempt["identity"].get("run") for attempt in attempts} != {1, 2, 3}:
            return "MISSING"
    if any(attempt["status"] != "PASSED" for attempt in attempts):
        return "FAILED"
    if not all(any(item["kind"] == "test.log" for item in attempt["files"]) for attempt in attempts):
        return "MISSING"
    return target.get("summaryStatus", "MISSING")


def validate_parity(inventory, parity):
    operations = {op["id"]: op for op in inventory["operations"]}
    for op in operations.values():
        equivalents = op["agent"].get("equivalents", [])
        if op.get("presentationOnly") and (equivalents or op["agent"]["status"] not in ("terminal-only", "delegated")):
            raise ValueError("invalid presentation-only capability: " + op["id"])
        if (op["agent"]["status"] == "equivalent") != bool(equivalents):
            raise ValueError("equivalent route requires explicit capabilities: " + op["id"])
        for name in equivalents:
            if (
                name not in operations
                or name == op["id"]
                or operations[name]["agent"].get("equivalents")
                or operations[name].get("presentationOnly")
            ):
                raise ValueError("invalid equivalent capability: " + name)
    checks = {name: [] for name in operations}
    names = set()
    for group in parity["groups"]:
        if not group["source"] or not group["scope"] or not group["targets"]:
            raise ValueError("empty parity evidence binding")
        for name in group["capabilities"]:
            names.add(name)
            if name in checks:
                checks[name].append(group)
    if names != set(operations) or len(operations) != len(inventory["operations"]):
        raise ValueError("parity inventory drift: " + str(sorted(names ^ set(operations))))
    return checks


def shaped(shape, results, seen=()):
    """Documented JSON shape, not validation coverage or typed MCP support."""
    if "$ref" in shape:
        name = shape["$ref"].removeprefix("#/results/")
        return name not in seen and name in results and shaped(results[name], results, (*seen, name))
    if "anyOf" in shape:
        return bool(shape["anyOf"]) and all(shaped(child, results, seen) for child in shape["anyOf"])
    if "const" in shape or "enum" in shape:
        return True
    if "type" not in shape:
        return False
    types = [shape["type"]] if isinstance(shape["type"], str) else shape["type"]
    if "object" in types and "properties" not in shape and not isinstance(shape.get("additionalProperties"), dict):
        return False
    if "array" in types and "items" not in shape:
        return False
    return (
        all(shaped(child, results, seen) for child in shape.get("properties", {}).values())
        and ("items" not in shape or shaped(shape["items"], results, seen))
        and (
            not isinstance(shape.get("additionalProperties"), dict)
            or shaped(shape["additionalProperties"], results, seen)
        )
    )


def verified_file(directory, item):
    path = directory / item["path"]
    if Path(item["path"]).is_absolute() or not path.resolve().is_relative_to(directory.resolve()):
        raise ValueError("unsafe evidence path")
    return path.read_bytes()


def coverage_summary(data):
    files, current = {}, None
    for line in data.decode().splitlines():
        if line.startswith("SF:"):
            name = line[3:]
            current = None
            if (
                PRODUCTION.fullmatch(name)
                and not name.endswith("_test.go")
                and Path(name).name not in ("legacy_module.go", "screen_check.go")
            ):
                current = files.setdefault(name, {})
        elif line.startswith("DA:") and current is not None:
            number, count, *_ = line[3:].split(",")
            number, count = int(number), int(count)
            if number <= 0 or count < 0:
                raise ValueError("invalid coverage observation")
            current[number] = max(current.get(number, 0), count)
    rows = [
        {"source": name, "covered": sum(count > 0 for count in lines.values()), "total": len(lines)}
        for name, lines in sorted(files.items())
        if lines
    ]
    return {
        "status": "MEASURED" if rows else "MISSING",
        "files": rows,
        "covered": sum(row["covered"] for row in rows),
        "total": sum(row["total"] for row in rows),
    }


def target_suite(target):
    tags = target.get("tags")
    if not isinstance(tags, list):
        raise ValueError("missing target tags: " + target["label"])
    if "hovel-followup" in tags:
        return "hovel"
    matches = [
        name
        for name in SUITES
        if name != "hovel" and ("acceptance-" + name in tags if name != "portable" else "acceptance" not in tags)
    ]
    if len(matches) != 1:
        raise ValueError("ambiguous or unclassified target: " + target["label"])
    return matches[0]


def required_targets(suite):
    if suite == "hovel":
        return HOVEL_TARGETS
    return {"//core/cmd/burrow:ssh_" + suite + "_test"} if suite != "portable" else set()


def report_model(inventory, parity, root=None):
    checks = validate_parity(inventory, parity)
    source = snapshot(root) if root else None
    suites = {name: {"status": "MISSING", "advisory": False, "targets": []} for name in (*SUITES, "coverage")}
    evidence = {}
    target_results = {}
    coverage = {"status": "MISSING", "files": [], "covered": 0, "total": 0}
    if root:
        for name in (*SUITES, "all", "coverage", "documentation"):
            directory = suite_directory(root, name)
            path = directory / "suite.json"
            if not path.exists():
                continue
            data = json.loads(path.read_text())
            if data["schemaVersion"] != 1 or data["suite"] != name:
                raise ValueError("unsupported or inconsistent suite evidence: " + name)
            if len({target["label"] for target in data["targets"]}) != len(data["targets"]):
                raise ValueError("duplicate target in suite: " + name)
            if set(data["selectedTargets"]) != {target["label"] for target in data["targets"]}:
                raise ValueError("evidence omits selected targets: " + name)
            passed = (
                data["exitCode"] == 0
                and data["finished"]
                and data["targets"]
                and all(target_status(t, name) == "PASSED" for t in data["targets"])
            )
            if data["status"] != ("PASSED" if passed else "FAILED"):
                raise ValueError("inconsistent suite status: " + name)
            if name == "documentation" and set(data["selectedTargets"]) != DOCUMENTATION_TARGETS:
                raise ValueError("wrong documentation targets")
            if name in SUITES:
                # The graph can select additional checks in a partition;
                # they never replace its mandatory production acceptance target.
                required = required_targets(name)
                if any(target_suite(target) != name for target in data["targets"]) or not required.issubset(
                    data["selectedTargets"]
                ):
                    raise ValueError("wrong targets for partition: " + name)
            for target in data["targets"]:
                if target["status"] != target_status(target, name):
                    raise ValueError("inconsistent target status: " + target["label"])
                for attempt in target["attempts"]:
                    for item in attempt["files"]:
                        content = verified_file(directory, item)
                        if not re.fullmatch(r"evidence/[a-f0-9]{20}-test\.(log|xml)\.txt", item["path"]):
                            raise ValueError("unexpected evidence artifact")
                        item["path"] = name + "/" + item["path"]
                        evidence[item["path"]] = content
                if name not in ("coverage", "documentation"):
                    if target["label"] in target_results:
                        raise ValueError("duplicate target evidence: " + target["label"])
                    target_results[target["label"]] = target
            evidence[name + "/suite.json"] = path.read_bytes()
            data["advisory"] = False
            if name == "all":
                # A local full preflight uses exactly the same required target set.
                for suite in SUITES:
                    selected = [target for target in data["targets"] if target_suite(target) == suite]
                    required = required_targets(suite)
                    if not required.issubset(target["label"] for target in selected):
                        raise ValueError("wrong targets for partition: " + suite)
                    suites[suite] = data | {
                        "targets": selected,
                        "advisory": False,
                        "status": data["status"] if selected else "MISSING",
                        "evidence": "all/suite.json",
                    }
            else:
                if name in suites and suites[name]["status"] != "MISSING":
                    raise ValueError("duplicate suite evidence: " + name)
                suites[name] = data | {"evidence": name + "/suite.json"}
            if name == "coverage" and "coverage" in data:
                if data["coverage"]["path"] != "coverage.lcov":
                    raise ValueError("unexpected coverage path")
                content = verified_file(directory, data["coverage"])
                evidence["coverage/coverage.lcov"] = content
                coverage = coverage_summary(content) | {
                    "evidence": "coverage/coverage.lcov",
                    "suiteStatus": data["status"],
                }
    if source:
        measured = {row["source"] for row in coverage["files"]}
        coverage["unmeasuredFiles"] = sorted(
            name
            for name in source["files"]
            if PRODUCTION.fullmatch(name)
            and not name.endswith("_test.go")
            and Path(name).name not in ("legacy_module.go", "screen_check.go")
            and name not in measured
        )
    capabilities = []
    for op in inventory["operations"]:
        bindings = []
        for group in checks[op["id"]]:
            if source and group["source"] not in source["files"]:
                raise ValueError("missing parity source: " + group["source"])
            for label in group["targets"]:
                target = target_results.get(label, {})
                bindings.append(
                    {
                        "source": group["source"],
                        "target": label,
                        "scope": group["scope"],
                        "status": target.get("status", "MISSING"),
                        "advisory": False,
                        "sha256": source["files"].get(group["source"]) if source else None,
                    }
                )
        status = (
            "PASSED"
            if bindings and all(item["status"] == "PASSED" for item in bindings)
            else "MISSING"
            if all(item["status"] == "MISSING" for item in bindings)
            else "INCOMPLETE"
        )
        schema = shaped(inventory["results"][op["result"]], inventory["results"]) and all(
            shaped(inventory["inputs"][name], inventory["results"]) for name in op["inputs"]
        )
        capabilities.append(
            op
            | {
                "checks": bindings,
                "semanticStatus": status,
                "requiredChecksPassed": bool(bindings) and all(item["status"] == "PASSED" for item in bindings),
                "schemaDocumented": schema,
            }
        )
    by_id = {op["id"]: op for op in capabilities}
    for op in capabilities:
        equivalents = [by_id[name] for name in op["agent"].get("equivalents", [])]
        op["agentUsable"] = (
            op["agent"]["status"] == "supported"
            or bool(equivalents)
            and all(item["agent"]["status"] == "supported" for item in equivalents)
        )
        if equivalents:
            op["schemaDocumented"] = all(item["schemaDocumented"] for item in equivalents)
            op["requiredChecksPassed"] = op["requiredChecksPassed"] and all(
                item["requiredChecksPassed"] for item in equivalents
            )
            if any(item["semanticStatus"] != "PASSED" for item in equivalents) and op["semanticStatus"] == "PASSED":
                op["semanticStatus"] = "INCOMPLETE"
    operational = [op for op in capabilities if not op.get("presentationOnly")]
    parity_result = {
        "capabilities": capabilities,
        "total": len(operational),
        "presentationOnly": len(capabilities) - len(operational),
        "reachable": sum(op["agentUsable"] for op in operational),
        "schemas": sum(op["schemaDocumented"] for op in operational),
        "demonstrated": sum(op["semanticStatus"] == "PASSED" and op["agentUsable"] for op in operational),
    }
    publishable = bool(
        source and coverage["status"] == "MEASURED" and all(suite["status"] == "PASSED" for suite in suites.values())
    )
    return {
        "schemaVersion": 1,
        "source": source,
        "suites": suites,
        "coverage": coverage,
        "parity": parity_result,
        "provenance": inventory["provenance"],
        "publishable": publishable,
        "releaseReady": publishable
        and parity_result["reachable"] == parity_result["total"]
        and all(op["requiredChecksPassed"] for op in capabilities),
    }, evidence


def report_html():
    # Shell and application follow Hovel 1789ce47; assets own the interactive UI.
    return """<section class="report-hero"><p class="hero-tag">// quality and test evidence</p>
<h1>Burrow Test Report</h1><p id="report-meta" class="report-meta">No generated test evidence is attached to this site build.</p></section>
<section id="report-app" class="report-app" aria-live="polite"><p class="empty-state">Run <code>aspect burrow-check preflight</code> and <code>aspect burrow-report coverage</code>, stage the site, then run <code>aspect burrow-report render</code>.</p></section>"""


def hovel_report(model, root=None):
    targets = []
    for suite, data in model["suites"].items():
        for item in data["targets"]:
            target = upstream.new_target(
                item["label"] + (" [" + suite + "]" if suite in ("coverage", "documentation") else "")
            )
            target.suite = suite
            target.language = {
                "py_test rule": "python",
                "go_test rule": "go",
                "sh_test rule": "shell",
                "js_test rule": "javascript",
            }.get(item.get("kind"), "unknown")
            target.status = item["status"]
            target.duration = sum(attempt.get("duration", 0) for attempt in item["attempts"])
            target.attempts = len(item["attempts"])
            for attempt in item["attempts"]:
                for file in attempt["files"]:
                    target.outputs.append(file["path"])
                    if file["kind"] == "test.log":
                        target.log_path = file["path"]
                    elif file["kind"] == "test.xml":
                        target.xml_path = file["path"]
                        if root:
                            upstream.enrich_from_xml(target, root / ".report-input" / file["path"])
            targets.append(target)
    targets.sort(key=lambda target: (upstream.STATUS_ORDER.get(target.status, 99), target.suite, target.label))
    coverage = model["coverage"]
    metrics = []
    if coverage["status"] == "MEASURED":
        metrics.append(
            upstream.CoverageMetric(
                name="Production Go",
                scope="Six declared production targets; excludes SSH acceptance and prototypes",
                metric_type="line",
                language="Go",
                platforms=["Linux amd64"],
                covered=coverage["covered"],
                total=coverage["total"],
                percentage=100 * coverage["covered"] / coverage["total"],
                minimum=0,
                status="MEASURED",
                source_path=coverage["evidence"],
            )
        )
    parity = model["parity"]
    total = parity["total"]
    operator = {
        "schemaVersion": "burrow.operator-parity/v1",
        "totals": {
            "capabilities": total,
            "reachabilityPercentage": 100 * parity["reachable"] / total if total else 0,
            "schemaPercentage": 100 * parity["schemas"] / total if total else 0,
            "contractPercentage": 100 * parity["demonstrated"] / total if total else 0,
        },
        "capabilities": [
            {
                "id": op["id"],
                "humanRoutes": [op["human"]],
                "agentRoutes": [{"tool": op["agent"].get("syntax", "")}],
                "risk": op["effects"],
                "status": op["semanticStatus"],
            }
            for op in parity["capabilities"]
        ],
    }
    jobs = [
        upstream.TestJob(
            name=name,
            category="verification",
            status=data["status"],
            duration=sum(attempt.get("duration", 0) for target in data["targets"] for attempt in target["attempts"]),
            description="Evidence collected at "
            + data.get("source", {}).get("commit", "unknown revision")
            + "; collecting GitHub run "
            + (data.get("environment", {}).get("runID") or "local")
            + ". Cached attempts: "
            + str(sum(attempt.get("cached", False) for target in data["targets"] for attempt in target["attempts"])),
        )
        for name, data in model["suites"].items()
    ]
    linters = []
    for label, name, scope in (
        ("//:python_lint_test", "Ruff", "Selected correctness rules on declared Python sources"),
        ("//:python_format_test", "Python formatting", "Declared Python sources"),
        ("//:go_format_test", "Go formatting", "Six production Go packages"),
        ("//:workflow_test", "actionlint", "GitHub Actions workflows"),
    ):
        target = next((target for target in targets if target.label == label), None)
        if target:
            linters.append(
                upstream.LintTool(
                    id=label,
                    name=name,
                    kind="static check",
                    scope=scope,
                    status=target.status,
                    duration=target.duration,
                    commands=["aspect test " + label],
                    ignore_statements=[],
                    log_path=target.log_path,
                )
            )
    result = upstream.TestReport(
        title="Burrow Test Report",
        generated_at=datetime.now(timezone.utc).isoformat(),
        workflow=os.environ.get("GITHUB_WORKFLOW", "local"),
        job=os.environ.get("GITHUB_JOB", "local"),
        commit=(model["source"] or {}).get("commit", ""),
        ref=os.environ.get("GITHUB_REF_NAME", ""),
        totals=upstream.summarize(targets),
        linters=linters,
        coverage=metrics,
        operator_parity=operator,
        jobs=jobs,
        targets=targets,
    )
    rendered = asdict(result)
    for linter in rendered["linters"]:
        # Burrow collects actual tool results, not an ignore-directive inventory.
        linter["ignores_measured"] = False
    return rendered


def render(root, site, parity, require_publishable=False, require_parity=False):
    # Preserve safe output paths independently of the removed hash manifest.
    if site.is_symlink() or any(path.is_symlink() for path in site.rglob("*")):
        raise ValueError("refusing symlink in report output")
    inventory = json.loads((site / "api/inventory.json").read_text())
    model, evidence = report_model(inventory, parity, root)
    if require_publishable and not model["publishable"]:
        raise ValueError("publication requires every required suite and measured production coverage")
    if require_parity and not model["releaseReady"]:
        raise ValueError(
            "final release requires usable agent routes and passing required behavior evidence for every capability"
        )
    # Hovel-style enforcement: structured evidence, complete referenced files,
    # parity and the successful workflow gate. No same-source/run/site hash gate.
    for name, data in evidence.items():
        path = site / "reports" / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    write_json(site / "reports/report.json", hovel_report(model, root))
    write_json(site / "reports/verification.json", model)
    for target in json.loads((site / "reports/report.json").read_text())["targets"]:
        for name in (target["log_path"], target["xml_path"], *target["outputs"]):
            if name and not (site / "reports" / name).is_file():
                raise ValueError("missing report evidence: " + name)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("begin", "collect", "default", "render"))
    parser.add_argument("--root", type=Path, default=Path(os.environ.get("BUILD_WORKSPACE_DIRECTORY", ".")))
    parser.add_argument("--suite", choices=(*SUITES, "all", "coverage", "documentation"))
    parser.add_argument("--exit-code", type=int, default=0)
    parser.add_argument("--inventory", type=Path)
    parser.add_argument("--parity", type=Path)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--site", type=Path)
    parser.add_argument("--require-publishable", action="store_true")
    parser.add_argument("--require-parity", action="store_true")
    args = parser.parse_args()
    if args.mode == "begin":
        begin(args.root, args.suite)
    elif args.mode == "collect":
        collect(args.root, args.suite, args.exit_code)
    elif args.mode == "default":
        model, evidence = report_model(json.loads(args.inventory.read_text()), json.loads(args.parity.read_text()))
        write_json(args.output, {"report": hovel_report(model), "verification": model, "html": report_html()})
    elif args.mode == "render":
        render(args.root, args.site, json.loads(args.parity.read_text()), args.require_publishable, args.require_parity)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, KeyError) as error:
        raise SystemExit(str(error))
