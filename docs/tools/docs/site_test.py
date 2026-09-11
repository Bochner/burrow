"""Check the emitted Pages book, navigation, assets, and search under /burrow/."""
import json
import os
import re
import sys
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
        if url.scheme or url.netloc or not url.path:
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
print(f"Validated {len(pages)} pages, internal links/assets, and search entries")
