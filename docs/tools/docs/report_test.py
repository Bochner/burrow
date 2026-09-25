"""Exercise the evidence CLI, including failures and publication refusals."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

tool = str(Path(sys.argv[1]).resolve())

def run(*args, ok=True):
    result = subprocess.run([tool, *map(str, args)], capture_output=True, text=True, timeout=20)
    assert (result.returncode == 0) == ok, (args, result.stdout, result.stderr)
    return result

with tempfile.TemporaryDirectory() as scratch:
    root = Path(scratch)
    def git(*args):
        return subprocess.check_output(["git", "-C", str(root), *args], stderr=subprocess.STDOUT)
    git("init", "-q")
    (root / ".gitignore").write_text("/.report-input/\n/_site/\n")
    (root / "core/launch").mkdir(parents=True)
    (root / "core/launch/example.go").write_text("package example\n")
    inventory = {"schemaVersion": 1, "inputs": {"options": {"type": "object", "description": "Options described only in prose"}}, "results": {"Result": {"type": "object", "properties": {}}},
                 "provenance": {}, "operations": [{"id": "example.inspect", "category": "example", "summary": "Inspect",
                 "human": "Inspect button", "agent": {"status": "supported", "syntax": "burrow inspect"},
                 "inputs": ["options"], "result": "Result", "effects": "Reads", "review": "None", "evidence": []}]}
    inventory["operations"] += [
        dict(inventory["operations"][0], id="example.tab", agent={"status": "equivalent", "syntax": "inspect tab", "equivalents": ["example.inspect"]}),
        dict(inventory["operations"][0], id="example.focus", presentationOnly="Selects the existing tab without changing its resource.", agent={"status": "terminal-only", "syntax": "focus tab"}),
    ]
    parity = {"schemaVersion": 1, "groups": [{"source": "core/launch/example.go", "targets": ["//core/example:check"],
              "scope": "Selected inspection outcome", "capabilities": ["example.inspect", "example.tab", "example.focus"]}]}
    (root / "parity.json").write_text(json.dumps(parity))
    git("add", ".")
    git("-c", "user.name=Report fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "fixture")
    run("begin", "--root", root, "--suite", "portable")
    directory = root / ".report-input/portable"
    log = directory / "raw.log"
    log.write_text('<script>alert("remote")</script>\x1b[31mFAIL\n')
    events = [
        {"id": {"targetConfigured": {"label": "//core/example:check"}}, "configured": {"testSize": "SMALL"}},
        {"id": {"testResult": {"label": "//core/example:check", "run": 1, "shard": 0, "attempt": 1}},
         "testResult": {"status": "FAILED", "testActionOutput": [{"name": "test.log", "uri": log.as_uri()}]}},
        {"id": {"testSummary": {"label": "//core/example:check"}}, "testSummary": {"overallStatus": "FAILED", "totalRunCount": 1}},
        {"id": {"buildFinished": {}}, "finished": {"exitCode": {"code": 3}}},
        {"id": {"pattern": {"pattern": ["//..."]}}, "expanded": {},
         "children": [{"targetConfigured": {"label": "//core/example:check"}}]},
    ]
    (directory / "bep.json").write_text("".join(json.dumps(event) + "\n" for event in events))
    run("collect", "--root", root, "--suite", "portable", "--exit-code", 3)
    report = json.loads((directory / "suite.json").read_text())
    assert report["status"] == "FAILED" and report["source"]["dirty"] is False
    assert report["targets"][0]["status"] == "FAILED"
    evidence = report["targets"][0]["attempts"][0]["files"][0]
    assert (directory / evidence["path"]).read_text() == log.read_text()
    # A passing summary cannot hide a failed repetition.
    events[2]["testSummary"]["overallStatus"] = "PASSED"
    events[3]["finished"]["exitCode"]["code"] = 0
    (directory / "bep.json").write_text("".join(json.dumps(event) + "\n" for event in events))
    run("collect", "--root", root, "--suite", "portable", "--exit-code", 0)
    assert json.loads((directory / "suite.json").read_text())["status"] == "FAILED"
    site = root / "_site"
    (site / "api").mkdir(parents=True)
    (site / "reports").mkdir()
    (site / "api/inventory.json").write_text(json.dumps(inventory))
    (site / "reports/index.html").write_text('<html><main><!-- burrow-report:start -->old<!-- burrow-report:end --></main></html>')
    (site / "search-index.json").write_text(json.dumps([{"href": "reports/", "text": "old"}]))
    run("render", "--root", root, "--site", site, "--parity", root / "parity.json")
    report = json.loads((site / "reports/report.json").read_text())
    assert not report["publishable"] and not report["releaseReady"]
    assert report["suites"]["portable"]["status"] == "FAILED"
    assert report["suites"]["files"]["status"] == "MISSING"
    assert report["suites"]["hovel"]["advisory"] is True
    assert report["coverage"]["status"] == "MISSING"
    assert report["parity"]["total"] == 2 and report["parity"]["presentationOnly"] == 1
    assert report["parity"]["reachable"] == 2 and report["parity"]["demonstrated"] == 0
    assert report["parity"]["schemas"] == 0, "prose-only object options counted as a documented shape"
    html = (site / "reports/index.html").read_text()
    assert '&lt;script&gt;alert' in html and '<script>' not in html
    assert '\\x1b' in html and '\x1b' not in html
    assert '<nav aria-label="Report sections">' in html
    before = (site / "reports/index.html").read_bytes()
    run("render", "--root", root, "--site", site, "--parity", root / "parity.json", "--require-publishable", ok=False)
    assert (site / "reports/index.html").read_bytes() == before, "failed publication replaced staged site"
    # An unregistered capability fails drift checks instead of disappearing.
    inventory["operations"].append(dict(inventory["operations"][0], id="example.new"))
    (site / "api/inventory.json").write_text(json.dumps(inventory))
    result = run("render", "--root", root, "--site", site, "--parity", root / "parity.json", ok=False)
    assert "parity inventory drift" in result.stderr
    inventory["operations"].pop()
    # Equivalence cannot point at missing, presentation-only, self or chained routes.
    for target in ("example.missing", "example.focus", "example.tab"):
        inventory["operations"][1]["agent"]["equivalents"] = [target]
        (site / "api/inventory.json").write_text(json.dumps(inventory))
        result = run("render", "--root", root, "--site", site, "--parity", root / "parity.json", ok=False)
        assert "invalid equivalent capability" in result.stderr
    inventory["operations"][1]["agent"]["equivalents"] = ["example.inspect"]
    inventory["operations"][0]["agent"] = {"status": "equivalent", "equivalents": ["example.tab"]}
    (site / "api/inventory.json").write_text(json.dumps(inventory))
    result = run("render", "--root", root, "--site", site, "--parity", root / "parity.json", ok=False)
    assert "invalid equivalent capability" in result.stderr
    inventory["operations"][0]["agent"] = {"status": "supported", "syntax": "burrow inspect"}
    inventory["inputs"]["options"]["properties"] = {"name": {"type": "string"}}
    (site / "api/inventory.json").write_text(json.dumps(inventory))
    events[1]["testResult"]["status"] = "PASSED"
    events[3]["finished"] = {"exitCode": {"name": "SUCCESS"}, "overallSuccess": True}
    for suite in ("portable", "lifecycle", "files", "reverse", "shell", "chains", "reports", "automation", "follow", "runs", "coverage"):
        run("begin", "--root", root, "--suite", suite)
        if suite == "portable":
            archived = list((root / ".report-input/archive").glob("portable-*/suite.json"))
            assert len(archived) == 1 and json.loads(archived[0].read_text())["status"] == "FAILED"
            assert (archived[0].parent / evidence["path"]).read_text() == '<script>alert("remote")</script>\x1b[31mFAIL\n'
        destination = root / ".report-input" / suite
        log = destination / "raw.log"
        log.write_text("PASS selected real behavior\n")
        events[1]["testResult"]["testActionOutput"][0]["uri"] = log.as_uri()
        for event, key in zip(events, ("targetConfigured", "testResult", "testSummary")):
            event["id"][key]["label"] = "//core/example:check" if suite in ("portable", "coverage") else "//core/cmd/burrow:ssh_" + suite + "_test"
        events[4]["children"][0]["targetConfigured"]["label"] = events[0]["id"]["targetConfigured"]["label"]
        events[0]["configured"]["tag"] = [] if suite in ("portable", "coverage") else ["acceptance", "acceptance-" + suite]
        partition_events = json.loads(json.dumps(events))
        if suite == "shell":
            # The real shell partition also selects its retained-session proof.
            extra = json.loads(json.dumps(events[:3]))
            for event, key in zip(extra, ("targetConfigured", "testResult", "testSummary")):
                event["id"][key]["label"] = "//core/prototype_sessions:check"
            partition_events += extra
            partition_events[4]["children"].append({"targetConfigured": {"label": "//core/prototype_sessions:check"}})
        (destination / "bep.json").write_text("".join(json.dumps(event) + "\n" for event in partition_events))
        if suite == "coverage":
            (destination / "coverage.lcov").write_text("SF:core/launch/example.go\nDA:1,2\nDA:2,0\nend_of_record\nSF:core/prototype_example/fake.go\nDA:1,1\nend_of_record\nSF:core/launch/example_test.go\nDA:1,1\nend_of_record\n")
        run("collect", "--root", root, "--suite", suite, "--exit-code", 0)
        assert json.loads((destination / "suite.json").read_text())["status"] == "PASSED"
    (site / ".source.json").write_text(json.dumps(json.loads((directory / "start.json").read_text())["source"]))
    run("render", "--root", root, "--site", site, "--parity", root / "parity.json", "--require-publishable", "--require-parity")
    report = json.loads((site / "reports/report.json").read_text())
    assert report["publishable"] and report["releaseReady"]
    assert report["coverage"]["covered"] == 1 and report["coverage"]["total"] == 2
    assert report["parity"]["demonstrated"] == 2
    assert report["parity"]["schemas"] == 2
    # An optional run for another merge tree cannot block or certify this one.
    advisory = root / ".report-input/hovel/suite.json"
    advisory.parent.mkdir()
    stale = json.loads((directory / "suite.json").read_text())
    stale["suite"] = "hovel"
    stale["source"]["commit"] = "0" * 40
    advisory.write_text(json.dumps(stale))
    run("render", "--root", root, "--site", site, "--parity", root / "parity.json", "--require-publishable", "--require-parity")
    with_stale = json.loads((site / "reports/report.json").read_text())
    assert with_stale["releaseReady"] and with_stale["suites"]["hovel"]["status"] == "MISSING"
    advisory.unlink()
    advisory.parent.rmdir()
    # A proof alone cannot substitute for its partition's production target.
    shell_evidence = root / ".report-input/shell/suite.json"
    original_shell = shell_evidence.read_text()
    proof_only = json.loads(original_shell)
    proof_only["targets"] = [t for t in proof_only["targets"] if t["label"] == "//core/prototype_sessions:check"]
    proof_only["selectedTargets"] = [t["label"] for t in proof_only["targets"]]
    shell_evidence.write_text(json.dumps(proof_only))
    refused = run("render", "--root", root, "--site", site, "--parity", root / "parity.json", "--require-parity", ok=False)
    assert "wrong targets for partition: shell" in refused.stderr
    shell_evidence.write_text(original_shell)
    for tags in (None, ["acceptance", "acceptance-files"], ["acceptance"], ["acceptance", "acceptance-shell", "acceptance-files"]):
        invalid = json.loads(original_shell)
        invalid["targets"][1]["tags"] = tags
        shell_evidence.write_text(json.dumps(invalid))
        refused = run("render", "--root", root, "--site", site, "--parity", root / "parity.json", ok=False)
        assert any(text in refused.stderr for text in ("target tags", "wrong targets", "ambiguous or unclassified"))
    shell_evidence.write_text(original_shell)
    # Local all-suite evidence must classify the same graph tags as hosted jobs.
    run("begin", "--root", root, "--suite", "all")
    combined, labels, partitions = [], [], []
    for partition in (root / ".report-input").iterdir():
        if partition.name in ("coverage", "all", "archive"):
            continue
        partitions.append(partition)
        for line in (partition / "bep.json").read_text().splitlines():
            event = json.loads(line)
            if "targetConfigured" in event.get("id", {}):
                labels.append(event["id"]["targetConfigured"]["label"])
            if "expanded" not in event and "finished" not in event:
                combined.append(event)
    combined += [{"expanded": {}, "children": [{"targetConfigured": {"label": label}} for label in labels]},
                 {"finished": {"exitCode": {"code": 0}}}]
    (root / ".report-input/all/bep.json").write_text("".join(json.dumps(event) + "\n" for event in combined))
    run("collect", "--root", root, "--suite", "all", "--exit-code", 0)
    for partition in partitions:
        partition.rename(partition.with_name("saved-" + partition.name))
    run("render", "--root", root, "--site", site, "--parity", root / "parity.json", "--require-publishable", "--require-parity")
    combined_report = json.loads((site / "reports/report.json").read_text())
    assert {t["label"] for t in combined_report["suites"]["shell"]["targets"]} == {"//core/cmd/burrow:ssh_shell_test", "//core/prototype_sessions:check"}
    assert [t["label"] for t in combined_report["suites"]["portable"]["targets"]] == ["//core/example:check"]
    (root / ".report-input/all").rename(root / ".report-input/saved-all")
    for partition in partitions:
        partition.with_name("saved-" + partition.name).rename(partition)
    # A passing adapter cannot hide a missing check on its headless equivalent.
    alternate = root / ".report-input/partial-parity.json"
    alternate.write_text(json.dumps({"schemaVersion": 1, "groups": [
        dict(parity["groups"][0], capabilities=["example.inspect"], targets=["//core/example:missing"]),
        dict(parity["groups"][0], capabilities=["example.tab", "example.focus"]),
    ]}))
    run("render", "--root", root, "--site", site, "--parity", alternate)
    partial = json.loads((site / "reports/report.json").read_text())
    tab = next(op for op in partial["parity"]["capabilities"] if op["id"] == "example.tab")
    assert tab["semanticStatus"] == "INCOMPLETE" and not partial["releaseReady"]
    run("render", "--root", root, "--site", site, "--parity", root / "parity.json", "--require-parity")
    assert (site / "reports/index.html").read_text().count('<details id="target-') == 12, "coverage repetitions lost their individual evidence"
    commit = git("rev-parse", "HEAD").decode().strip()
    run("verify", "--site", site, "--commit", commit)
    run("verify", "--site", site, "--commit", "0" * 40, ok=False)
    (site / "reports/index.html").write_text("unverified replacement")
    run("verify", "--site", site, "--commit", commit, ok=False)
    (site / "reports/index.html").write_text('<main><!-- burrow-report:start --><!-- burrow-report:end --></main>')
    # Required CI success never manufactures agent parity for missing routes.
    inventory["operations"][0]["agent"]["status"] = "unsupported"
    (site / "api/inventory.json").write_text(json.dumps(inventory))
    run("render", "--root", root, "--site", site, "--parity", root / "parity.json", "--require-publishable")
    result = run("render", "--root", root, "--site", site, "--parity", root / "parity.json", "--require-parity", ok=False)
    assert "usable agent routes" in result.stderr
    # Selection comes from the declared graph, independently of result records.
    original = (directory / "suite.json").read_text()
    omitted = json.loads(original)
    omitted["targets"] = []
    omitted["status"] = "FAILED"
    (directory / "suite.json").write_text(json.dumps(omitted))
    result = run("render", "--root", root, "--site", site, "--parity", root / "parity.json", ok=False)
    assert "selected targets" in result.stderr
    (directory / "suite.json").write_text(original)
    for event, key in zip(events, ("targetConfigured", "testResult", "testSummary")):
        event["id"][key]["label"] = "//core/cmd/burrow:setup_test"
    events[4]["children"][0]["targetConfigured"]["label"] = "//core/cmd/burrow:setup_test"
    # One passing run, or three retries of it, cannot satisfy three repetitions.
    for attempts in ([dict(events[1])], [json.loads(json.dumps(events[1])) for _ in range(3)]):
        for index, attempt in enumerate(attempts, 1):
            attempt["id"]["testResult"]["attempt"] = index
        events[2]["testSummary"]["totalRunCount"] = len(attempts)
        (directory / "bep.json").write_text("".join(json.dumps(event) + "\n" for event in [events[0], *attempts, *events[2:]]))
        run("collect", "--root", root, "--suite", "portable", "--exit-code", 0)
        incomplete = json.loads((directory / "suite.json").read_text())
        assert incomplete["status"] == "FAILED" and incomplete["targets"][0]["status"] == "MISSING"
    (directory / "suite.json").write_text(original)
    evidence = json.loads((directory / "suite.json").read_text())["targets"][0]["attempts"][0]["files"][0]
    (directory / evidence["path"]).write_text("tampered")
    result = run("render", "--root", root, "--site", site, "--parity", root / "parity.json", ok=False)
    assert "inconsistent evidence hash" in result.stderr
    (root / "core/launch/example.go").write_text("package changed\n")
    result = run("collect", "--root", root, "--suite", "portable", "--exit-code", 0, ok=False)
    assert "source changed" in result.stderr, result.stderr
print("PASS commit-bound collection, failed attempts and retained evidence")
