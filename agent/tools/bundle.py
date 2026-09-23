"""Deterministic release skill bundle; the production installer validates it."""
import hashlib
import json
from pathlib import Path
import sys
import zipfile

output, *inputs = sys.argv[1:]
files = {str(Path(p).relative_to("agent")): Path(p).read_bytes() for p in inputs}
manifest = json.loads(files.pop("burrow-agent.json"))
manifest["files"] = {name: hashlib.sha256(data).hexdigest() for name, data in sorted(files.items())}
files["burrow-agent.json"] = (json.dumps(manifest, sort_keys=True, indent=2) + "\n").encode()
with zipfile.ZipFile(output, "w") as archive:
    for name, data in sorted(files.items()):
        entry = zipfile.ZipInfo(name, (1980, 1, 1, 0, 0, 0))
        entry.external_attr = 0o100644 << 16
        archive.writestr(entry, data)
