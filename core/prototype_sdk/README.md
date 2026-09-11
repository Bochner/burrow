# Disposable external SDK and lifecycle proof

For [Can the external SDK package and session lifecycle be demonstrated?](https://github.com/Bochner/burrow/issues/6).
Prototype only; keep this branch out of main. Owner review is pending.

## Observed on Linux amd64, 2026-09-11

The pinned public SDK builds in Burrow with unchanged-source BUILD overlay.
The full upstream Bzlmod dependency failed before compilation at MODULE.bazel:24:
`nogo.includes = ["@//:__subpackages__"]` references a repository unavailable
when Hovel is an external module. No upstream source was patched to work around it.

The fallback downloads the full archive, replaces only its SDK BUILD boundary,
and compiles precisely upstream HOVEL_SRCS (excluding tests). It retains the original
import path and Apache-2.0 license, including that license in the package.
This is a consumer-maintained overlay, not a published standalone Go SDK release.

Pins: Hovel c461ba282a8aecc7aa3a079a4613bf5e2640c388;
archive SHA-256 c2233e71fa79473555863b5d0b8381ccf5902877fb6d341825f60a6e12a1e722;
Go 1.26.5; rules_go 0.61.1; Gazelle 0.51.3; x/sys v0.47.0;
rules_pkg 1.2.0; Aspect 2026.33.3; Bazel 9.1.1.
The manifest follows the pinned Hovel module-development contract; the inert
module reuses its public LineShellSession rather than executing shell commands.

| Check | Observed result |
| --- | --- |
| Independent SDK build and framed protocol | Pass: handshake/schema, structured module/log, returned session I/O, explicit close, shutdown exit 0. |
| Linked package and .tgz install | Both pass manifest, Linux launcher, RPC discovery, schema, and installed-module discovery. |
| Real daemon execution | Both pass inert survey execution through Hovel's normal chain planning and `throw --now`; persisted plans have confirmation IDs. Identical throws reuse the plan hash. |
| Attach/detach/reattach | Both pass two real `hovel session connect` invocations, detaching on input EOF; session remains usable between them. |
| Explicit close | Both pass: the module PID disappears after closing the session. |
| Daemon shutdown | Both pass: SIGTERM exits the daemon with code 0 and removes the active module process. |

The test uses disposable workspaces and XDG configuration/data directories.
The SDK build needs no local Hovel checkout. Integration accepts a real Hovel
executable built from the pin as its host service boundary; it imports no internals.
The package build and protocol test are declared, cacheable Bazel targets.

## Repeat

From this branch in a clean Burrow checkout:

```sh
aspect burrow-prototype check
aspect burrow-prototype package
```

In a separate Hovel checkout at the exact pin, use its documented workflow:

```sh
aspect build @hovel_core//cmd/hovel
```

Then from Burrow, substitute that checkout's absolute executable path:

```sh
aspect burrow-prototype integration -- /absolute/hovel/bazel-bin/external/+local_repository+hovel_core/cmd/hovel/hovel_/hovel
aspect burrow-check
```

The integration command prints each package check, attachment transcript, and
PASS line. It removes its scratch state and processes on completion.
Hovel `run -- session connect` is not an attachment entry point; the proof uses
its direct `session connect` command. The direct command runs without a controlling
terminal to exercise input-EOF detach reproducibly. The PID is reported in the
inert result summary because Hovel's throw JSON does not expose SDK outputs.

## Limits for owner review

This proves the smallest inert package/session boundary, not useful SSH parity,
terminal geometry/raw mode/Ctrl-] behavior, multiple sessions sharing one process,
remote EOF cleanup, failed-close recovery, stuck-operation cancellation, pipe loss,
or restart recovery. Those observations from the lifecycle research remain open
acceptance work for the relevant UI/ownership/transport decisions.

No SSH connections, credentials, listeners, real commands, or persistent user
state are created. Process disappearance proves OS process cleanup, not that
arbitrary future SSH cleanup callbacks will execute.

Proposed conclusion: accept this bounded integration proof and carry the overlay
as a viable packaging candidate into the next decisions. Keep upstream distribution
and terminal/lifecycle limits explicit; do not merge this prototype into production.
