# Release tooling provenance

The wheel builder and exec launcher adapt Hovel's
[`core/tools/release/build_hovel_wheel.py`](https://github.com/vibepwners/hovel/blob/541e78ada0af48165aef932732145c418aa2d290/core/tools/release/build_hovel_wheel.py),
Copyright 2026 William Born, Apache-2.0. Burrow narrows it to Linux amd64,
declared inputs, a `burrow-ssh` distribution and `burrow` entry point; adds
compressed deterministic output and installed-wheel verification. The pinned
Hovel SDK license is included in the wheel. Agent skills and the module
manifest remain embedded in the existing Go executable.

The release workflow retains Hovel's pinned PyPA action, `pypi` environment
and job-local OIDC permission. Burrow's owner-approved promotion path builds and
smoke-tests archive/wheel files on verified main CI, then Release reuses that exact bundle. A version increase on main automatically
creates the matching tag and GitHub release after CI succeeds; root CHANGELOG.md
supplies notes. GitHub publication is independent of optional manual PyPI publishing; manual
dispatch defaults to rehearsal. Tag/VERSION/main checks, successful source-run verification and
artifact checksums guard publication. No legacy LazySSH API token is copied.
