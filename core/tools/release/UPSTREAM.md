# Release tooling provenance

The wheel builder and exec launcher adapt Hovel's
[`core/tools/release/build_hovel_wheel.py`](https://github.com/vibepwners/hovel/blob/541e78ada0af48165aef932732145c418aa2d290/core/tools/release/build_hovel_wheel.py),
Copyright 2026 William Born, Apache-2.0. Burrow narrows it to Linux amd64,
declared inputs, a `burrow-ssh` distribution and `burrow` entry point; adds
compressed deterministic output and installed-wheel verification. The pinned
Hovel SDK license is included in the wheel. Agent skills and the module
manifest remain embedded in the existing Go executable.

The release workflow follows Hovel's separate verification/build/publish jobs,
pinned PyPA publishing action, `pypi` environment and job-local OIDC permission.
It requires manual dispatch and defaults to build-only while PyPI setup is
pending. No legacy LazySSH API token is copied or required.
