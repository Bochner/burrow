# Disposable script execution boundary proof

For [Can remote script runs satisfy Hovel execution and cleanup contracts?](https://github.com/Bochner/burrow/issues/26).

**Finding: the synchronous confirmed throw path does not satisfy retained script
execution after caller loss at the pinned Hovel revision.** This is a prerequisite
failure reproduction, not the full script implementation or an accepted workaround.
The owner accepted the retained-session sequence and explicit-collection limit
below. The remaining invocation/cleanup matrix is still open.

Run `aspect burrow-prototype transport` or `aspect burrow-check`. The latter also
builds the SDK package/frontend and checks the SDK protocol and documentation.
Use the [transport pins and local prerequisites](README.md) and the accepted
[connection ownership mechanism](OWNERSHIP.md). Hovel is v0.4.2, source
`c461ba282a8aecc7aa3a079a4613bf5e2640c388`; this does not characterize newer versions.

## Observations

The packaged Go module runs a fixed inert script through ordinary OpenSSH over
the daemon-owned master. `/bin/sh -s --` receives a streamed script and separately
quoted arguments containing spaces, a quote, shell metacharacters and Unicode.
The loopback server trusts only fixture-generated keys. No operator credentials
or configuration are used. The offered Ubuntu server was not needed for these
local Hovel lifecycle and persistence failures.

| Case | Checked observation |
| --- | --- |
| Confirmed synchronous throw | The script exits 7; Hovel records a failed result and a workspace artifact with separate stdout/stderr and the observed SSH exit code. |
| Binary output over limit | A 70,000-byte NUL burst plus the argument output is fully drained; the artifact retains the first 65,536 bytes and the exact discarded count. Base64 preserves bytes through JSON. This is a fixture limit pending product policy. |
| Caller disappears | Kill the Hovel CLI after the remote start marker. The fixed remote script later writes its completion marker, but Hovel has only a run-start event, no terminal event, completed throw record or output artifact for this execution. |
| Direct daemon API comparison | Privileged `ExecuteModule` runs the dangerous-tagged fixture and returns a result without adding a throw confirmation or materializing an artifact. It is not a replacement for the confirmed throw flow. |
| Missing selected master | The script operation fails with SSH exit 255 and no stdout. `ProxyCommand=/bin/false` prevents fallback authentication. The result says transport/completion unknown rather than inventing a remote exit result. |
| Shared connection | A sibling command still runs after caller loss; the inherited connection-wide teardown checks continue to pass. |

The remote marker files are independent test observations in the fixture's
temporary directory. They are not a production ledger, a staged script or a
way to reconstruct lost Hovel evidence. The test reads Hovel's SQLite records
only to verify persistence; the module does not write Hovel's database.

## Why this happens

The pinned [daemon ExecuteModule handler](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonrpc/daemonrpc.go)
passes the HTTP request context into execution. The
[module runner](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/moduleruntime/pythonrpc/runner.go)
applies its execution deadline (60 seconds by default) and kills a non-retained
module on call failure. The
[run service](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/app/services/run.go)
returns on request cancellation without appending a terminal run event. The
[throw command](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/app/commands/catalog.go)
materializes result artifacts after the execution call returns to its caller.
The direct execution endpoint does not carry that surrounding throw workflow.

The [Go SDK dispatcher](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/server.go)
ignores cancel notifications. Killing a local caller or module therefore does not
prove remote termination. The observed completion marker is a concrete example;
this proof does not establish cancellation of a remote process tree.

## Decision boundary and remaining work

Keep the accepted detach, confirmation and evidence requirements. A candidate
next proof is a run represented by a retained Hovel session, with confirmed start
and explicit status/cancel/result collection through supported operations. This
must prove final artifact persistence, confirmation coverage, remote cancellation
and cleanup; a returned session reference alone is insufficient. The follow-up
below proves the now-accepted lifetime/collection sequence; the remaining
workflow matrix is not yet a complete production contract.
An upstream retained-run contract remains the other route.

Do not claim the remaining ticket matrix has passed: operator-selected local
tools driving two connections, remote existing commands/scripts, separate stdin
files, actual staged-file cleanup and keep behavior, per-run cancellation with a
child, timeout, connection loss during execution and cleanup, and long-running
retained status/output still need the selected execution path. No AI integration,
new job framework, audit database or production SSH code is introduced.

## Retained-session follow-up

Owner acceptance: initial runs use explicit collection; uncollected output may
be lost on daemon/module failure. [The script decision](https://github.com/Bochner/burrow/issues/26)
records the acceptance. [The nonblocking Hovel handoff](https://github.com/Bochner/burrow/issues/39)
requests durable completion without a foreground collector.

The owner authorized testing retained Hovel sessions and accepted this bounded
sequence after reviewing its collection tradeoff. The
candidate reuses the daemon-owned connection above and adds one result-bearing
SDK session per inert run. It introduces no job registry or audit database.
The same Aspect transport gate runs both the original failure reproduction and
this follow-up. The loopback remote supervisor uses the declared Python 3.12
toolchain executable inherited from the harness; Python availability on arbitrary
remote machines is **not** established by this fixture.

### Candidate sequence

1. A confirmed `script-prepare` throw creates an idle session. No remote code runs.
2. A confirmed `script-launch` throw addresses that already-retained session.
   A mutex and permanent started flag ensure retries refer to the same execution.
3. The remote supervisor owns its inert child's process group, optional timeout,
   and its exact staged script path. The retained module drains a bounded result
   envelope; it never places script output on the module's protocol stdout or in
   SDK diagnostic logs.
4. Status queries expose the retained result. A confirmed `script-cancel` throw
   requests cancellation and collects its result; ordinary frontend exit does
   neither. Killing a local SSH process is never treated as confirmed cleanup.
5. A confirmed `script-collect` throw materializes the result in Hovel's workspace
   artifact store. Collection does not erase the session result and can be retried.
   Explicit session close follows successful collection. Its persisted artifact
   remains readable after close.

The collection throw's success means **collection succeeded**. Remote exit code,
cancellation, timeout, and uncertainty remain separate fields in that artifact.
The proof retains a 65,536-byte prefix of each output stream, reports exact
discard counts, and base64-encodes arbitrary bytes. Its 256 KiB JSON reply limit
covers both encoded streams. These are fixture bounds, not an approved product
output-retention policy. Output is available in the result at completion; this
does not demonstrate live streaming of arbitrary script output.

### Behavior checks

| Case | Assertion |
| --- | --- |
| Success/nonzero exit | Frontend exits while the run remains active; status and a later confirmed artifact preserve separate stdout/stderr, binary bytes, truncation count and remote exit 0/7. |
| Cancel/timeout | Remote supervisor signals only its process group; independent harness observations verify the leader and its ordinary child are no longer live. Sibling use of the SSH connection remains available. |
| Staging | Default completion/cancel/timeout removes the exact inert staged script and its empty private directory; explicit keep preserves them for harness cleanup. |
| Failed/repeated collection | A missing session fails; the original result remains collectible. Repeated cancel/collection returns the existing terminal result. Artifact hash and correlated Hovel confirmation are checked. |
| One-step start interrupted | Deliberately delay the initial module result, then kill its caller after remote launch. No broker session or final artifact becomes discoverable. SDK registration alone does not establish retention. |
| Prepared-session launch interrupted | Prepare first, launch through a second confirmed throw, and kill that caller before its result. The existing run remains queryable; retrying launch does not change the independently observed remote PID or rerun the script. Later collection persists its result. |
| Master loss | The session preserves transport/completion uncertainty, no invented remote exit and unconfirmed cleanup. Confirmed collection records that uncertainty. Missing-master reuse refuses authentication fallback. |
| Daemon death before collection | After a completed result is visible through status, kill and restart Hovel without collecting it. The session is unavailable and no final output artifact has appeared. |

The one-step gap follows the pinned runner's adoption boundary: it keeps the
module only after receiving the result and adopting its sessions. The two-step
candidate starts remote work only after that boundary. Cancelling a later
session-command request abandons the request's response; it does not kill the
already-retained owner. Sources: pinned
[runner and session broker](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/moduleruntime/pythonrpc/runner.go),
[SDK dispatch](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/server.go),
and [throw confirmation/artifact collection](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/app/commands/catalog.go).

### Accepted collection limit and remaining proof

This candidate makes **prepare, launch, inspect/cancel, collect, close** explicit.
Until collection succeeds, final output lives in the retained module's memory,
not in Hovel's durable artifact store. Module/daemon death before collection can
lose it. Preparation or launch response loss is not permission to create and
execute a replacement run: inspect the existing session first. Session commands
remain privileged low-level interfaces; their destructive/read-only metadata
does not enforce throw confirmation. Operator-facing mutation must use the
confirmed throw path. This is the same Hovel authority boundary observed above,
not an added credential or confirmation system.

The remote supervisor is a Linux/Python inert fixture. It does not prove portable
remote process-tree containment, daemonized/escaped descendants, hostile target
result integrity, production signal escalation, or cleanup under every failure.
Transport loss is reported as unknown even when the independent loopback harness
can observe termination or remove leftover fixture files.

The full ticket still requires local tools selecting between two connections,
operator-selected scripts/interpreters, existing remote commands/scripts,
independent stdin files, streaming through this retained path, injected cleanup
failure, and a final output/retention policy. The original streamed synchronous
probe is separate evidence, not proof of those features in this candidate.
Do not close the ticket or unblock final slice planning merely because this
bounded gate passes. The lifetime/collection tradeoff is accepted; the remaining workflow matrix
must still be demonstrated before resolving the complete scripting ticket.
