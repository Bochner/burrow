"""Check the emitted Pages book, navigation, assets, and search under /burrow/."""
import json
import os
import re
import sys
import subprocess
from pathlib import Path
from urllib.parse import unquote, urlsplit

root = Path(sys.argv[1]).resolve()
pages = sorted(root.rglob("*.html"))
assert len(pages) >= 6, "expected a home page and documentation chapters"
for page in pages:
    html = page.read_text()
    assert len(re.findall(r"<h1(?:\s|>)", html)) == 1, page
    assert 'id="main-content"' in html and 'class="skip-link"' in html, page
    for raw in re.findall(r'\b(?:href|src)="([^"]+)"', html):
        url = urlsplit(raw)
        if url.scheme or url.netloc:
            continue
        if not url.path:
            if url.fragment:
                assert f'id="{unquote(url.fragment)}"' in html, (page, raw)
            continue
        path = unquote(url.path)
        if path.startswith("/"):
            assert path.startswith("/burrow/"), (page, raw)
            target = root / path.removeprefix("/burrow/")
        else:
            target = page.parent / path
        # Bazel runfiles are symlinks to declared assets outside this directory.
        target = Path(os.path.abspath(target))
        assert target.is_relative_to(root), (page, raw)
        if target.is_dir():
            target /= "index.html"
        assert target.is_file(), (page, raw)
        if url.fragment and target.suffix == ".html":
            assert f'id="{unquote(url.fragment)}"' in target.read_text(), (page, raw)
index = json.loads((root / "search-index.json").read_text())
assert len(index) == len(pages), "search must cover every page"
for entry in index:
    target = root / entry["href"]
    assert (target / "index.html" if target.is_dir() else target).is_file(), entry
    assert entry["title"] and entry["text"], entry
assert "Authorized red-team emulation only." in (root / "index.html").read_text()
assert (root / "LICENSE-HOVEL").is_file()
assert (root / ".nojekyll").is_file()
contract = json.loads((root / "api/inventory.json").read_text())
binary_contract = json.loads(subprocess.check_output([sys.argv[2], "capabilities"], text=True))
# The site producer may be built in the execution configuration; compare the
# contract, while retaining its actual binary digest as separate provenance.
assert contract.pop("provenance")["binarySHA256"]
assert binary_contract.pop("provenance")["binarySHA256"]
assert contract == binary_contract, "site inventory differs from the real binary"
for operation in contract["operations"]:
    page = root / f'api/{operation["category"]}.html'
    html = page.read_text()
    assert f'id="{operation["id"]}"' in html, operation["id"]
    assert operation["agent"]["status"] in html, operation["id"]
    assert any(operation["id"] in entry["text"] for entry in index), operation["id"]
for page in pages:
    assert re.search(r'href="[^"]*api/"[^>]*>API</a>', page.read_text()), page
assert 'aria-current="page">API</a>' in (root / "api/index.html").read_text()
report = json.loads((root / "reports/report.json").read_text())
report_html = (root / "reports/index.html").read_text()
assert {op["id"] for op in report["parity"]["capabilities"]} == {op["id"] for op in contract["operations"]}
assert not report["publishable"] and report["coverage"]["status"] == "MISSING"
assert 'aria-current="page">Reports</a>' in report_html
assert 'Optional MCP is not measured as typed MCP coverage' in report_html
assert 'aria-label="Report sections"' in report_html and 'tabindex="0"' in report_html
for page in pages:
    assert re.search(r'href="[^"]*reports/"[^>]*>Reports</a>', page.read_text()), page
print(f"Validated {len(pages)} pages, internal links/assets, and search entries")
