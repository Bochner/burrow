"""Exercise the built book in an explicitly supplied local Chromium browser."""

import functools
import http.server
import os
import re
from pathlib import Path
import subprocess
import sys
import tempfile
import threading

root = Path(sys.argv[1]).resolve()
browser = os.environ["BURROW_DOCS_BROWSER"]
print(subprocess.check_output([browser, "--version"], text=True).strip(), flush=True)
probe = """
<script>
addEventListener('load', async () => {
  const wait = () => new Promise(resolve => setTimeout(resolve, 500));
  const errors = [];
  await wait();
  for (const details of document.querySelectorAll('details')) details.open = true;
  if (document.documentElement.classList.contains('home-page')) {
    if (scrollY > 1) errors.push('initial scroll: ' + scrollY);
    scrollTo({top: 160, behavior: 'instant'});
    await wait();
    if (Math.abs(scrollY - 160) > 1) errors.push('forced scroll: ' + scrollY);
    scrollTo({top: document.body.scrollHeight, behavior: 'instant'});
    await wait();
    scrollTo({top: 0, behavior: 'instant'});
    await wait();
    if (scrollY > 1) errors.push('cannot return to top: ' + scrollY);
  }
  for (const diagram of document.querySelectorAll('.mermaid')) {
    for (let attempt = 0; attempt < 8 && !diagram.classList.contains('rendered'); attempt++) await wait();
    if (!diagram.querySelector('svg[role~="graphics-document"]')) errors.push('diagram not rendered: ' + diagram.innerHTML.slice(0, 200));
  }
  if (document.documentElement.scrollWidth > innerWidth + 1) errors.push('horizontal overflow');
  const result = document.createElement('output');
  result.id = 'browser-check';
  result.textContent = errors.length ? errors.join('; ') : 'PASS';
  document.body.append(result);
});
</script>
"""


class Handler(http.server.SimpleHTTPRequestHandler):
    def translate_path(self, path):
        return super().translate_path(path.removeprefix("/burrow"))

    def log_message(self, *args):
        pass

    def do_GET(self):
        path = Path(self.translate_path(self.path))
        if path.is_dir():
            path /= "index.html"
        if path.suffix == ".html" and path.is_file():
            body = path.read_text().replace("</body>", probe + "</body>").encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        else:
            super().do_GET()


server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), functools.partial(Handler, directory=str(root)))
threading.Thread(target=server.serve_forever, daemon=True).start()
try:
    pages = [root / "index.html"]
    pages += sorted((root / "spec").glob("*.html"))
    pages += sorted((root / "api").glob("*.html"))
    for width, height in [(1280, 900), (390, 844)]:
        for page in pages:
            with tempfile.TemporaryDirectory() as profile:
                result = subprocess.run(
                    [
                        browser,
                        "--headless",
                        "--no-sandbox",
                        "--disable-gpu",
                        "--disable-background-networking",
                        f"--user-data-dir={profile}",
                        f"--window-size={width},{height}",
                        "--virtual-time-budget=6000",
                        "--dump-dom",
                        f"http://127.0.0.1:{server.server_port}/burrow/{page.relative_to(root)}",
                    ],
                    capture_output=True,
                    text=True,
                    timeout=30,
                    check=True,
                )
                verdict = re.search(r'<output id="browser-check">(.*?)</output>', result.stdout)
                if not verdict or verdict[1] != "PASS":
                    outputs = Path(os.environ.get("TEST_UNDECLARED_OUTPUTS_DIR", profile))
                    outputs.mkdir(parents=True, exist_ok=True)
                    (outputs / f"{page.stem}-{width}.html").write_text(result.stdout)
                    (outputs / f"{page.stem}-{width}.stderr").write_text(result.stderr)
                assert verdict and verdict[1] == "PASS", (
                    page.name,
                    width,
                    verdict[1] if verdict else result.stderr[-1000:],
                )
                print(f"PASS {width}x{height} {page.relative_to(root)}", flush=True)
finally:
    server.shutdown()
