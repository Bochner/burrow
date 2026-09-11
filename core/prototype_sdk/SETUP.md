# Disposable Hovel setup and attachment proof

For [Can Hovel setup, daemon attachment, and persistent quit behavior be demonstrated?](https://github.com/Bochner/burrow/issues/18).
This is an inert runtime feasibility probe, not an installer or SSH implementation.
The existing setup policy remains unchanged. Operator verdict is pending.

## Repeat

On Linux amd64:

```sh
aspect burrow-prototype setup
aspect burrow-check
```

Aspect obtains the declared wheel through a SHA-256-pinned Bazel `http_file`.
The pinned Python 3.12 runtime extracts only `hovel/bin/hovel` into a temporary
per-user-style tree. No pip, Python package installation, global Hovel change,
or source build is necessary. All daemons bind owner Unix sockets and loopback
TCP only. Workspaces, configuration, home, package and processes are disposable.

The setup command includes the earlier linked/archive SDK session integration.
It leaves explicit close/shutdown cleanup assertions and uses two successive
terminal clients against the same retained session. It does not claim simultaneous
input arbitration, SSH connection recovery, or a production UI.

## Provenance

Inspected and executed on 2026-09-11:

- [Hovel release v0.4.2](https://github.com/vibepwners/hovel/releases/tag/v0.4.2),
  published 2026-09-04. The GitHub tag API resolves it to
  `c461ba282a8aecc7aa3a079a4613bf5e2640c388`, the existing source/SDK pin.
- [Linux amd64 wheel](https://github.com/vibepwners/hovel/releases/download/v0.4.2/hovel-0.4.2-py3-none-manylinux_2_28_x86_64.whl):
  SHA-256 `7edccdabe04e30098e417d27ab48064c11a8612149124b7df0797251e88a0933`,
  pinned from GitHub's release-asset digest and checked against downloaded bytes.
  The extracted executable reports `version 0.4.2`.
- [Upstream wheel builder](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/tools/release/build_hovel_wheel.py)
  packages the Go executable at that fixed member path and supplies a Python
  exec launcher. This proof invokes the executable directly.
- SDK source archive and Go/build toolchain pins are inherited from
  [the terminal proof](README.md); the SDK sources remain unmodified.

This establishes published artifact availability, pinned-byte integrity and
release/tag association. It is not an independent reproducible-build attestation
or proof that a remote running process came from those bytes. The wheel declares
manylinux 2.28 x86-64; other Linux architectures/libc variants are untested here.

## Runtime observations

| Probe | Result |
| --- | --- |
| Acquisition | Declared wheel downloaded and verified; altered bytes rejected before installation. |
| Default workspace | Normal Hovel workspace at a fixed path inside the scratch XDG data tree; explicit restart retains an inert saved operation. Exact production path remains a routine implementation choice. |
| Bounded discovery | Only default and explicitly selected workspace status/socket endpoints queried; both daemon identities checked. No scan or second discovery registry. |
| Endpoint precedence | Actual Hovel CLI selects flag over environment over `daemon.client` YAML. An unused local workspace argument does not create/switch the remote workspace. |
| Local/remote attachment | Owner Unix sockets, bare host:port and tcp://host:port work; daemon reports its actual workspace. Remote transport is exercised over loopback, not across machines. |
| Access | Read-only TCP refuses CreateOperation with HTTP 403 even with client acknowledgement. Insecure-full requires both server enablement and client acknowledgement; then creates only an inert operation. Without acknowledgement, GetDaemonInfo reports effective read-only access. |
| Endpoint errors | HTTPS and missing Unix socket fail through the public CLI; bounded connect/process timeouts, no fallback workspace or daemon restart. |
| Quit/handoff | Existing real-terminal fixture detaches, reattaches with fresh input, and retains the same inert module process; Ctrl-C interruption also retains it. Explicit close and daemon shutdown remove module processes. |
| Compatibility | Real GetDaemonInfo contains workspace/PID/start/health/access/listeners, but no version or capability identity. The proof emits an actionable refusal and verifies daemon identity is unchanged. |

The rejection is a proposed frontend refusal, not a feature already implemented
by Hovel. It deliberately has no invented successful-handshake branch. Fixture
operations still run because the harness itself acquired and launched the exact
pinned package into isolated workspaces; that controlled experiment does not
validate arbitrary daemon attachment. Download outage, hostile endpoint bodies,
cross-machine networking, interactive endpoint selection, and Linux arm64 are
not exercised. Terminal geometry limitations remain documented in the older proof.

## Smallest upstream requirement for owner review

Expose a documented compatibility identifier in the public daemon discovery
response: a protocol revision or capability set with defined client acceptance
rules. Include the running daemon's build/release identity for useful diagnostics.
The response must come from the connected daemon and retain workspace identity
and effective-access information. An installed executable's `version` output,
PID, or successfully calling one method is insufficient for this contract.

Burrow should refuse absent/unsupported compatibility information before
operational mutation and report the selected endpoint/workspace with a supported
upgrade/reconnect action. No silent replacement, restart, fallback workspace,
private protocol, or version guess. This handshake addresses compatibility only;
it does not add authentication or TLS to Hovel's existing TCP transport.

Owner review can accept the feasible setup/lifecycle evidence while requiring
this public contract before general existing-daemon attachment is implemented.
No upstream issue or core change has been made by this proof.

Source: [public daemon contract](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/daemon-rpc.html),
[OpenAPI DaemonInfo](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/spec/reference/daemon-rpc.openapi.json),
[client precedence](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonlocal/daemonlocal.go).
