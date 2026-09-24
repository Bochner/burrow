"""Collect and publish Burrow's commit-bound test evidence through Aspect."""
import argparse
from datetime import datetime, timezone
import hashlib
import html
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import unicodedata
from urllib.parse import unquote, urlsplit

SUITES = ("portable", "lifecycle", "files", "reverse", "shell", "chains", "reports", "automation", "follow", "runs")
PRODUCTION = re.compile(r"^core/(cmd/burrow|connection|launch|reports|agent|terminal)/[^/]+\.go$")
START, END = "<!-- burrow-report:start -->", "<!-- burrow-report:end -->"


def digest(data):
    return hashlib.sha256(data).hexdigest()


def write_json(path, data):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2, sort_keys=True) + "\n")


def snapshot(root):
    def git(*args):
        return subprocess.check_output(["git", "-C", str(root), *args])
    names = sorted(set(git("ls-files", "-c", "-o", "--exclude-standard", "-z").decode().split("\0")) - {""})
    files = {}
    for name in names:
        path = root / name
        if path.is_symlink():
            data = os.readlink(path).encode()
        elif path.is_file():
            data = path.read_bytes()
        else:
            data = b"<deleted>"
        files[name] = digest(data)
    return {"commit": git("rev-parse", "HEAD").decode().strip(),
            "dirty": bool(git("status", "--porcelain", "--untracked-files=normal")),
            "treeSHA256": digest(json.dumps(files, sort_keys=True).encode()), "files": files,
            "pins": {name: {"sha256": files[name], "content": (root / name).read_text()}
                     for name in (".bazelversion", ".aspect/version.axl", "MODULE.bazel", "docs/site/.nvmrc", "docs/site/package.json", "core/connection/lab-image.txt") if name in files}}


def suite_directory(root, suite):
    directory = root / ".report-input" / suite
    if directory.is_symlink() or directory.parent.is_symlink():
        raise ValueError("refusing symlink evidence directory")
    return directory


def begin(root, suite):
    directory = suite_directory(root, suite)
    if directory.exists():
        shutil.rmtree(directory)
    directory.mkdir(parents=True)
    write_json(directory / "start.json", {"source": snapshot(root),
        "started": datetime.now(timezone.utc).isoformat(),
        "environment": {"os": platform.platform(), "python": platform.python_version(),
                        "runID": os.environ.get("GITHUB_RUN_ID", ""), "runAttempt": os.environ.get("GITHUB_RUN_ATTEMPT", "")}})


def collect(root, suite, exit_code):
    directory = suite_directory(root, suite)
    metadata = json.loads((directory / "start.json").read_text())
    if metadata["source"] != snapshot(root):
        raise ValueError("source changed during evidence collection; rerun this partition")
    targets, selected, finished = {}, set(), False
    bep = directory / "bep.json"
    for line in bep.read_text().splitlines() if bep.exists() else []:
        event = json.loads(line)
        identity = event.get("id", {})
        if "expanded" in event:
            selected.update(child["targetConfigured"]["label"] for child in event.get("children", []) if "targetConfigured" in child)
        target = None
        for kind in ("targetConfigured", "testResult", "testSummary"):
            if kind in identity and (kind != "targetConfigured" or event.get("configured", {}).get("testSize")):
                label = identity[kind]["label"]
                target = targets.setdefault(label, {"label": label, "status": "MISSING", "attempts": []})
        if "testResult" in event:
            result = event["testResult"]
            attempt = {"identity": identity["testResult"], "status": result.get("status", "MISSING"), "files": []}
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
                name = "evidence/" + digest(json.dumps(attempt["identity"], sort_keys=True).encode())[:20] + "-" + item["name"] + ".txt"
                destination = directory / name
                destination.parent.mkdir(exist_ok=True)
                destination.write_bytes(data)
                attempt["files"].append({"path": name, "sha256": digest(data), "kind": item["name"]})
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
    status = "PASSED" if exit_code == 0 and finished and selected == targets.keys() and selected and all(t["status"] == "PASSED" for t in targets.values()) else "FAILED"
    result = metadata | {"schemaVersion": 1, "suite": suite, "status": status, "exitCode": exit_code, "finished": finished,
                         "selectedTargets": sorted(selected),
                         "targets": sorted(targets.values(), key=lambda target: target["label"])}
    coverage = directory / "coverage.lcov"
    if coverage.exists():
        result["coverage"] = {"path": "coverage.lcov", "sha256": digest(coverage.read_bytes())}
    write_json(directory / "suite.json", result)


def target_status(target, suite):
    attempts = target["attempts"]
    identities = {json.dumps(attempt["identity"], sort_keys=True) for attempt in attempts}
    if not attempts or len(identities) != len(attempts) or len(attempts) != target.get("expectedRuns", 0):
        return "MISSING"
    if suite in ("all", "portable") and target["label"] in ("//core/cmd/burrow:setup_test", "//core/cmd/burrow:terminal_test"):
        if len(attempts) != 3 or {attempt["identity"].get("run") for attempt in attempts} != {1, 2, 3}:
            return "MISSING"
    if any(attempt["status"] != "PASSED" for attempt in attempts):
        return "FAILED"
    if not all(any(item["kind"] == "test.log" for item in attempt["files"]) for attempt in attempts):
        return "MISSING"
    return target.get("summaryStatus", "MISSING")


def validate_parity(inventory, parity):
    operations = {op["id"]: op for op in inventory["operations"]}
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
    return (all(shaped(child, results, seen) for child in shape.get("properties", {}).values())
            and ("items" not in shape or shaped(shape["items"], results, seen))
            and (not isinstance(shape.get("additionalProperties"), dict) or shaped(shape["additionalProperties"], results, seen)))


def verified_file(directory, item):
    path = directory / item["path"]
    if Path(item["path"]).is_absolute() or not path.resolve().is_relative_to(directory.resolve()):
        raise ValueError("unsafe evidence path")
    data = path.read_bytes()
    if digest(data) != item["sha256"]:
        raise ValueError("inconsistent evidence hash: " + item["path"])
    return data


def coverage_summary(data):
    files, current = {}, None
    for line in data.decode().splitlines():
        if line.startswith("SF:"):
            name = line[3:]
            current = None
            if PRODUCTION.fullmatch(name) and not name.endswith("_test.go") and Path(name).name not in ("legacy_module.go", "screen_check.go"):
                current = files.setdefault(name, {})
        elif line.startswith("DA:") and current is not None:
            number, count, *_ = line[3:].split(",")
            number, count = int(number), int(count)
            if number <= 0 or count < 0:
                raise ValueError("invalid coverage observation")
            current[number] = max(current.get(number, 0), count)
    rows = [{"source": name, "covered": sum(count > 0 for count in lines.values()), "total": len(lines)}
            for name, lines in sorted(files.items()) if lines]
    return {"status": "MEASURED" if rows else "MISSING", "files": rows,
            "covered": sum(row["covered"] for row in rows), "total": sum(row["total"] for row in rows)}


def report_model(inventory, parity, root=None):
    checks = validate_parity(inventory, parity)
    source = snapshot(root) if root else None
    suites = {name: {"status": "MISSING", "advisory": name == "hovel", "targets": []}
              for name in (*SUITES, "coverage", "hovel")}
    evidence = {}
    target_results = {}
    coverage = {"status": "MISSING", "files": [], "covered": 0, "total": 0}
    if root:
        for name in (*SUITES, "all", "coverage", "hovel"):
            directory = suite_directory(root, name)
            path = directory / "suite.json"
            if not path.exists():
                continue
            data = json.loads(path.read_text())
            if data["schemaVersion"] != 1 or data["suite"] != name or data["source"] != source:
                raise ValueError("stale or inconsistent suite evidence: " + name)
            if name != "hovel" and data["environment"]["runID"] != os.environ.get("GITHUB_RUN_ID", ""):
                raise ValueError("evidence from another CI run: " + name)
            if len({target["label"] for target in data["targets"]}) != len(data["targets"]):
                raise ValueError("duplicate target in suite: " + name)
            if set(data["selectedTargets"]) != {target["label"] for target in data["targets"]}:
                raise ValueError("evidence omits selected targets: " + name)
            passed = data["exitCode"] == 0 and data["finished"] and data["targets"] and all(target_status(t, name) == "PASSED" for t in data["targets"])
            if data["status"] != ("PASSED" if passed else "FAILED"):
                raise ValueError("inconsistent suite status: " + name)
            if name in SUITES[1:] and {target["label"] for target in data["targets"]} != {"//core/cmd/burrow:ssh_" + name + "_test"}:
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
                if name != "coverage":
                    if target["label"] in target_results:
                        raise ValueError("duplicate target evidence: " + target["label"])
                    target_results[target["label"]] = target
            evidence[name + "/suite.json"] = path.read_bytes()
            data["advisory"] = name == "hovel"
            if name == "all":
                # A local full preflight uses exactly the same required target set.
                for suite in SUITES:
                    selected = [target for target in data["targets"] if
                                (target["label"] == "//core/cmd/burrow:ssh_" + suite + "_test" if suite != "portable"
                                 else not target["label"].startswith("//core/cmd/burrow:ssh_"))]
                    suites[suite] = data | {"targets": selected, "status": data["status"] if selected else "MISSING", "evidence": "all/suite.json"}
            else:
                if suites[name]["status"] != "MISSING":
                    raise ValueError("duplicate suite evidence: " + name)
                suites[name] = data | {"evidence": name + "/suite.json"}
            if name == "coverage" and "coverage" in data:
                if data["coverage"]["path"] != "coverage.lcov":
                    raise ValueError("unexpected coverage path")
                content = verified_file(directory, data["coverage"])
                evidence["coverage/coverage.lcov"] = content
                coverage = coverage_summary(content) | {"evidence": "coverage/coverage.lcov", "suiteStatus": data["status"]}
    if source:
        measured = {row["source"] for row in coverage["files"]}
        coverage["unmeasuredFiles"] = sorted(name for name in source["files"] if PRODUCTION.fullmatch(name) and
            not name.endswith("_test.go") and Path(name).name not in ("legacy_module.go", "screen_check.go") and name not in measured)
    capabilities = []
    for op in inventory["operations"]:
        bindings = []
        for group in checks[op["id"]]:
            if source and group["source"] not in source["files"]:
                raise ValueError("missing parity source: " + group["source"])
            for label in group["targets"]:
                target = target_results.get(label, {})
                bindings.append({"source": group["source"], "target": label, "scope": group["scope"],
                                 "status": target.get("status", "MISSING"),
                                 "sha256": source["files"].get(group["source"]) if source else None})
        status = "PASSED" if bindings and all(item["status"] == "PASSED" for item in bindings) else "MISSING" if all(item["status"] == "MISSING" for item in bindings) else "INCOMPLETE"
        schema = shaped(inventory["results"][op["result"]], inventory["results"]) and all(shaped(inventory["inputs"][name], inventory["results"]) for name in op["inputs"])
        capabilities.append(op | {"checks": bindings, "semanticStatus": status, "schemaDocumented": schema})
    parity_result = {"capabilities": capabilities, "total": len(capabilities),
                     "reachable": sum(op["agent"]["status"] == "supported" for op in capabilities),
                     "schemas": sum(op["schemaDocumented"] for op in capabilities),
                     "demonstrated": sum(op["semanticStatus"] == "PASSED" and op["agent"]["status"] == "supported" for op in capabilities)}
    publishable = bool(source and not source["dirty"] and coverage["status"] == "MEASURED" and
                       all(suites[name]["status"] == "PASSED" for name in (*SUITES, "coverage")))
    return {"schemaVersion": 1, "source": source, "suites": suites, "coverage": coverage, "parity": parity_result,
            "provenance": inventory["provenance"], "publishable": publishable,
            "releaseReady": publishable and parity_result["demonstrated"] == parity_result["total"]}, evidence


def safe_text(value):
    text = str(value)
    return html.escape("".join(char if char in "\n\t" or unicodedata.category(char) not in ("Cc", "Cf")
                              else (f"\\x{ord(char):02x}" if ord(char) < 256 else f"\\u{ord(char):04x}") for char in text))


def table(headers, rows):
    return '<div class="report-table" tabindex="0" role="region" aria-label="' + safe_text(headers[0]) + '"><table><thead><tr>' + "".join('<th scope="col">' + safe_text(h) + '</th>' for h in headers) + '</tr></thead><tbody>' + "".join('<tr>' + "".join('<td>' + cell + '</td>' for cell in row) + '</tr>' for row in rows) + '</tbody></table></div>'


def link(path, label):
    return '<a href="' + safe_text(path) + '">' + safe_text(label) + '</a>'


def render_html(report, evidence):
    source, parity, coverage = report["source"], report["parity"], report["coverage"]
    sections = ("overview", "coverage", "parity", "suites", "targets", "provenance")
    parts = [START, '<article class="report-shell"><div class="report-hero"><p class="hero-tag">// quality and operator evidence</p><h1>Burrow Test Report</h1>',
             '<p>Source: ' + safe_text(source["commit"] + (" (uncommitted changes)" if source["dirty"] else "") if source else "No test evidence attached to this build") + '</p></div>',
             '<div class="report-app"><nav aria-label="Report sections">' + ''.join(link('#' + name, name.title()) for name in sections) + '</nav><div class="report-panels">',
             '<section id="overview"><h2>Overview</h2><p>' + ("Required verification passed; site evidence is eligible for promotion." if report["publishable"] else "Not eligible for publication: required results, coverage or a clean matching source are missing or failed.") + '</p>',
             '<p>Final agent parity: ' + ("all required capabilities have usable routes and selected passing behavior evidence; owner acceptance remains separate." if report["releaseReady"] else "incomplete. This report does not claim milestone completion.") + '</p>',
             '<div class="summary-grid">' + ''.join('<div class="metric"><span>' + safe_text(label) + '</span><strong>' + str(value) + ' / ' + str(parity["total"]) + '</strong></div>' for label, value in [("Agent reachability", parity["reachable"]), ("Documented schemas", parity["schemas"]), ("Selected agent semantics", parity["demonstrated"])]) + '</div>',
             '<p>' + link('report.json', 'Download report and provenance JSON') + ' · Reproduce: <code>aspect burrow-check preflight</code>, <code>aspect burrow-report coverage</code>, <code>aspect burrow-site stage</code>, <code>aspect burrow-report render</code>.</p></section>',
             '<section id="coverage"><h2>Production Go coverage</h2><p>Measured executable lines from six declared production Go test targets on Linux. Excludes prototypes, dependencies, test code and test-only binaries. The production SSH/CLI/PTY acceptance partitions run separately and do not contribute to these counters. This is not branch coverage or exhaustive behavioral coverage. No percentage threshold is invented.</p>']
    if coverage["status"] == "MEASURED":
        percent = 100 * coverage["covered"] / coverage["total"]
        parts += [f'<p><strong>{percent:.2f}%</strong> ({coverage["covered"]} / {coverage["total"]} measured lines). Coverage suite: ' + safe_text(coverage["suiteStatus"]) + '. ' + link(coverage["evidence"], 'Raw LCOV baseline') + '</p>',
                  table(["Production file", "Covered lines", "Measured lines"], [[safe_text(row["source"]), str(row["covered"]), str(row["total"])] for row in coverage["files"]])]
    else:
        parts.append('<p class="empty-state">MISSING: no measured coverage is attached. A missing measurement is not zero coverage.</p>')
    if coverage.get("unmeasuredFiles"):
        parts.append('<details><summary>Production files without a line measurement</summary><pre>' + safe_text('\n'.join(coverage["unmeasuredFiles"])) + '</pre></details>')
    parts += ['</section><section id="parity"><h2>Operator interface parity</h2><p>Reachability means a supported CLI route is advertised. Schema coverage means inputs and results have documented JSON shapes; descriptions alone do not count. Neither proves behavior. Selected semantic coverage requires the listed real checks to pass for this source and a usable agent route. It is not exhaustive outcome coverage. Optional MCP is not measured as typed MCP coverage. Delegated and terminal-only routes remain distinct.</p>',
              table(["Capability / human route", "Agent route", "Risk / review", "Contract status", "Selected behavior evidence"], [[
                  '<strong>' + link('../api/' + op["category"] + '.html#' + op["id"], op["id"]) + '</strong><br>' + safe_text(op["human"]),
                  safe_text(op["agent"]["status"]) + '<br><code>' + safe_text(op["agent"]["syntax"]) + '</code><p>' + safe_text(op["agent"].get("limitation", "")) + '</p>',
                  safe_text(op["effects"]) + '<p>' + safe_text(op["review"]) + '</p>',
                  ('Documented shape' if op["schemaDocumented"] else 'Partial / no structured shape') + '<br>Semantics: ' + safe_text(op["semanticStatus"]),
                  ''.join('<p>' + (link('#target-' + digest(item["target"].encode())[:16], item["target"]) if item["status"] != "MISSING" else safe_text(item["target"])) + '<br>' + safe_text(item["source"]) + '<br>' + safe_text(item["scope"]) + '<br>' + safe_text(item["status"]) + '</p>' for item in op["checks"])
              ] for op in parity["capabilities"]]), '</section><section id="suites"><h2>Required and advisory suites</h2><p>The three Hovel #86 diagnostics are advisory under the accepted release exception. Missing advisory evidence is shown explicitly; it never substitutes for a required suite.</p>',
              table(["Suite", "Requirement", "Status", "Targets / metadata"], [[safe_text(name), 'Advisory: ' + link('https://github.com/Bochner/burrow/issues/86', '#86') if suite["advisory"] else 'Required', safe_text(suite["status"]), str(len(suite["targets"])) + (' · ' + link(suite["evidence"], 'Evidence') if 'evidence' in suite else '')] for name, suite in report["suites"].items()]),
              '</section><section id="targets"><h2>Individual test evidence</h2><p>Every repetition is retained. Failed, missing and skipped results do not count as passed. Log previews escape HTML and control characters; downloads retain the original bytes. Long previews are limited to 16,000 characters.</p>']
    seen = set()
    for suite_name, suite in report["suites"].items():
        for target in suite["targets"]:
            key = target["label"]
            anchor = suite_name + ":" + key if key in seen else key
            seen.add(key)
            parts.append('<details id="target-' + digest(anchor.encode())[:16] + '"><summary>' + safe_text(key + ' (' + suite_name + ') — ' + target["status"]) + '</summary>')
            for attempt in target["attempts"]:
                parts.append('<p>' + safe_text(attempt["identity"]) + ': ' + safe_text(attempt["status"]) + '</p>')
                for item in attempt["files"]:
                    parts.append('<p>' + link(item["path"], item["kind"] + ' (original)') + ' · SHA256 <code>' + safe_text(item["sha256"]) + '</code></p>')
                    if item["kind"] == "test.log":
                        parts.append('<pre tabindex="0">' + safe_text(evidence[item["path"]].decode(errors="replace")[:16000]) + '</pre>')
            parts.append('</details>')
    parts += ['</section><section id="provenance"><h2>Source and tool provenance</h2><p>Source digests bind every tracked and nonignored input, including dependency locks, Aspect workflows, Go toolchain pins and the SSH lab image. Each suite JSON also records its collection time, OS, Python toolchain and CI run identity.</p>',
              '<pre>' + safe_text(json.dumps({"source": {key: value for key, value in (source or {}).items() if key != "files"}, "runtime": report["provenance"]}, indent=2)) + '</pre>',
              '<p>Report layout and evidence approach adapted from Hovel <code>a4cbfdf7769a9551695088c11061e3cabc368e07</code>, Copyright 2026 William Born, Apache-2.0. ' + link('../LICENSE-HOVEL', 'License') + ' · ' + link('https://github.com/Bochner/burrow/blob/main/docs/site/UPSTREAM.md', 'Adaptation notes') + '. All measurements are Burrow data.</p></section></div></div></article>', END]
    return ''.join(parts)


def render(root, site, parity, require_publishable=False, require_parity=False):
    site_hashes(site)  # Refuse preexisting symlinks before writing attached artifacts.
    inventory = json.loads((site / "api/inventory.json").read_text())
    model, evidence = report_model(inventory, parity, root)
    stamp = site / ".source.json"
    if stamp.exists() and json.loads(stamp.read_text()) != model["source"]:
        raise ValueError("stale site source; restage before attaching evidence")
    if require_publishable and (not model["publishable"] or not stamp.exists()):
        raise ValueError("publication requires clean matching site, every required suite and measured production coverage")
    if require_parity and not model["releaseReady"]:
        raise ValueError("final release requires usable agent routes and passing behavior evidence for every capability")
    page = site / "reports/index.html"
    body = page.read_text()
    if body.count(START) != 1 or body.count(END) != 1:
        raise ValueError("site is missing its report insertion boundary")
    content = render_html(model, evidence)
    before, rest = body.split(START)
    _, after = rest.split(END)
    for name, data in evidence.items():
        path = site / "reports" / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    write_json(site / "reports/report.json", model)
    page.write_text(before + content + after)
    index_path = site / "search-index.json"
    index = json.loads(index_path.read_text())
    for entry in index:
        if entry["href"] == "reports/":
            entry["text"] = html.unescape(re.sub(r"<[^>]*>", " ", content))
    write_json(index_path, index)
    publication = site / ".publication.json"
    if publication.exists():
        publication.unlink()
    if model["publishable"]:
        write_json(publication, {"commit": model["source"]["commit"], "files": site_hashes(site)})


def site_hashes(site):
    files = {}
    for path in sorted(site.rglob("*")):
        if path.is_symlink():
            raise ValueError("published site contains a symlink")
        if path.is_file() and path != site / ".publication.json":
            files[str(path.relative_to(site))] = digest(path.read_bytes())
    return files


def verify_publication(site, commit):
    publication = json.loads((site / ".publication.json").read_text())
    report = json.loads((site / "reports/report.json").read_text())
    stamp = json.loads((site / ".source.json").read_text())
    if (not re.fullmatch(r"[a-f0-9]{40}", commit) or publication["commit"] != commit
            or report["source"]["commit"] != commit or report["source"] != stamp or stamp["dirty"]
            or not report["publishable"] or publication["files"] != site_hashes(site)):
        raise ValueError("publication evidence does not match the eligible commit and site artifact")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("begin", "collect", "default", "render", "verify"))
    parser.add_argument("--root", type=Path, default=Path(os.environ.get("BUILD_WORKSPACE_DIRECTORY", ".")))
    parser.add_argument("--suite", choices=(*SUITES, "all", "hovel", "coverage"))
    parser.add_argument("--exit-code", type=int, default=0)
    parser.add_argument("--inventory", type=Path)
    parser.add_argument("--parity", type=Path)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--site", type=Path)
    parser.add_argument("--commit")
    parser.add_argument("--require-publishable", action="store_true")
    parser.add_argument("--require-parity", action="store_true")
    args = parser.parse_args()
    if args.mode == "begin":
        begin(args.root, args.suite)
    elif args.mode == "collect":
        collect(args.root, args.suite, args.exit_code)
    elif args.mode == "default":
        model, evidence = report_model(json.loads(args.inventory.read_text()), json.loads(args.parity.read_text()))
        write_json(args.output, {"report": model, "html": render_html(model, evidence)})
    elif args.mode == "render":
        render(args.root, args.site, json.loads(args.parity.read_text()), args.require_publishable, args.require_parity)
    else:
        verify_publication(args.site, args.commit)

if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, KeyError) as error:
        raise SystemExit(str(error))
