# Disposable script execution boundary proof

For [Can remote script runs satisfy Hovel execution and cleanup contracts?](https://github.com/Bochner/burrow/issues/26).

**Finding: the synchronous confirmed throw path does not satisfy retained script
execution after caller loss at the pinned Hovel revision.** This is a prerequisite
failure reproduction, not the full script implementation or an accepted workaround.
Owner review is pending. The full ticket requirements remain in force.

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
and cleanup; a returned session reference alone is insufficient. An upstream
retained-run contract is the other route. Neither is implemented or accepted here.

Do not claim the remaining ticket matrix has passed: operator-selected local
tools driving two connections, remote existing commands/scripts, separate stdin
files, actual staged-file cleanup and keep behavior, per-run cancellation with a
child, timeout, connection loss during execution and cleanup, and long-running
retained status/output still need the selected execution path. No AI integration,
new job framework, audit database or production SSH code is introduced.
