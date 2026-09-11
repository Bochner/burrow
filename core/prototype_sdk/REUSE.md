# Disposable own-daemon reuse proof

Question: [Can Burrow safely reuse its own pinned daemon after relaunch?](https://github.com/Bochner/burrow/issues/20).
Owner verdict pending. This is a Linux runtime experiment, not production setup
code, a new daemon protocol, or general existing-daemon attachment.

## Repeat

```sh
aspect burrow-prototype reuse
aspect burrow-check
```

The declared test uses the existing verified Hovel v0.4.2 wheel and inert SDK
package. Source, package digest and toolchain provenance remain in [SETUP.md](SETUP.md)
and `MODULE.bazel`. Python 3.12 is declared through Aspect/Bazel. No new dependency
or Hovel modification was needed. The test requires Linux procfs, Unix peer
credentials, pidfds, and glibc's `pidfd_open`/`pidfd_send_signal` symbols: the
pinned standalone Python does not expose its usual pidfd wrappers, so this
throwaway probe calls libc through standard-library ctypes. This libc requirement
belongs to the Python fixture, not a proposed Go implementation requirement.

Measured 2026-09-11 on Linux x86-64, WSL2 kernel
`6.18.33.2-microsoft-standard-WSL2`, glibc 2.43. The supplied Ubuntu server was
not needed for this local lifecycle question. Other kernels/libcs and PID
namespaces have not been validated. The test is marked `no-sandbox` to inspect
real Linux process identities, and creates only temporary private workspaces.

## Proposed minimum evidence

Use Hovel's selected canonical workspace and its ordinary `hoveld.sock`.
After a controlled launch of verified pinned bytes, keep one owner-only local
launch receipt beside that workspace. The prototype calls it
`burrow-launch-prototype.json`; it is local provenance, not a discovery registry
or a copy of daemon-owned session/credential/operation state.

The receipt contains PID, Linux boot ID and process start ticks, SHA-256 of the
running executable, canonical workspace path and Hovel's reported start time.
The expected executable digest is independently derived from the SHA-256-pinned
wheel, never trusted merely because it appears in the receipt or installed path.

On a later frontend launch:

1. Read a bounded owner-only regular receipt, rejecting symlinks, malformed or
   missing content. Select the workspace explicitly; never take a fallback from
   the record.
2. Open a Linux pidfd for the recorded PID and confirm it has not exited.
3. Connect to the ordinary Unix endpoint and compare `SO_PEERCRED` PID/UID with
   the launch receipt and operator UID. Reachability alone proves nothing.
4. Compare boot/start identity and hash `/proc/<pid>/exe` against the independent
   pin. Read public `GetDaemonInfo` on that connection and match PID, workspace
   and Hovel start time. Recheck process identity/liveness before proceeding.
5. Perform the operation on that same verified socket. A closed connection
   requires fresh validation; the HTTP client must not silently reconnect.

No receipt, a stale receipt, an ended process, inaccessible identity evidence or
a mismatch means refusal. Leave processes, receipt and selected workspace alone.
Recovery/reconnect is explicit, consistent with the existing ownership decision.
The receipt is not a credential and does not attest against root or a hostile
process running as the same user. Hovel's workspace is not that security boundary.

## Observations

| Check | Observed result |
| --- | --- |
| Frontend exit and relaunch | First process launches the pinned daemon, installs the existing inert module, creates a session and exits. Separate frontend processes verify the same daemon and session; retained input/output still works. |
| Stale evidence | Altered PID, boot ID, start ticks, executable digest, workspace and discovery start time all refuse. |
| Record validation | Missing, malformed, oversized, non-private and symlink receipts refuse. Restoring the original receipt permits reuse. |
| Independent executable pin | A different expected digest refuses even with an otherwise valid receipt. Replacing the installed pathname while the original executable remains running does not invalidate the original process's verified bytes. |
| Endpoint replacement | Another real pinned Hovel daemon, serving a different workspace at the expected socket, refuses while both daemons remain alive and the receipt stays unchanged. Restoring the original socket allows reuse. |
| Ended/restarted process | After explicit shutdown, reuse refuses. Starting the same pinned binary in the same workspace does not make the old receipt valid. |
| Cleanup | Explicit close removes the retained module process; explicit shutdown ends the original daemon. The fixture stops its replacement daemon and removes temporary files. |

`aspect burrow-check` includes `reuse_test` plus inherited package/frontend
builds, SDK protocol behavior and generated documentation checks. Passing this
gate establishes the bounded observations above, not production SSH behavior.

## Implementation limits and handoff

This proves the feasibility of local reuse without a Hovel API change. It does
not approve a production installer or implement crash recovery, concurrent first
launches, durable receipt publication, upgrades, arbitrary daemon attachment,
TCP reuse or SSH recovery. The launcher here only creates a new scratch instance;
all negative cases exercise the reuse path, which never starts a daemon.

Production should preserve refusal on incomplete publication and serialize its
own first-launch/receipt creation. Hovel already locks its selected workspace;
do not duplicate daemon state or interpret that PID-based lock as provenance.
Its listener startup removes an existing Unix pathname, which is another reason
never to invoke startup as a response to failed reuse validation. Concurrent
launch, crash-publication and permission-failure cases belong in the first
implementation slice's behavior gate. PID recycling was represented by a stale
start identity; this test does not force the kernel to recycle a numeric PID.

The parent decision should accept or reject this evidence and its proposed
receipt/peer-check boundary. General attachment still needs the deferred public
compatibility contract. This prototype is preserved separately from `main`.

Upstream source inspected at `c461ba282a8aecc7aa3a079a4613bf5e2640c388`:
[public discovery](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonrpc/daemonrpc.go),
[workspace lock and listener startup](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/infra/daemonruntime/runtime.go).
These are inspected as evidence; Burrow imports none of those internal packages.
