# Disposable script execution boundary proof

For [Can remote script runs satisfy Hovel execution and cleanup contracts?](https://github.com/Bochner/burrow/issues/26).

**Finding: the synchronous confirmed throw path does not satisfy retained script
execution after caller loss at the pinned Hovel revision.** This is a prerequisite
failure reproduction, not the full script implementation or an accepted workaround.
The owner accepted the retained-session sequence and explicit-collection limit
below. The invocation follow-up below extends that sequence; owner acceptance of its final limits is pending.

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

The original probe alone does not establish the retained workflow. The follow-ups
below separate the lifetime proof from the complete invocation matrix. No new
job framework, audit database or production SSH code is introduced.

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

## Invocation and cleanup follow-up

The same retained-session implementation now snapshots a bounded, non-secret
fixture request before preparation returns. It sends that JSON envelope through
SSH stdin before the cancellation control stream. Script contents and stdin do
not enter SSH argv or Hovel configuration; the harness writes the request under
its private scratch root. This file is fixture input, not a job ledger or a
production secret-delivery interface.

The added checks exercise these paths through confirmed preparation, launch and
collection, and verify the collected artifact survives session close:

- Operator-selected local script, explicit `/bin/sh`, separate arguments including
  empty strings, quotes, metacharacters and Unicode, and independent binary stdin.
- Staged execution with remove/keep; streamed execution through an inherited pipe
  with no named remote script; existing remote script without deleting its source.
- Existing `/bin/cat` command and an alternate explicitly selected Python
  interpreter. Script input and the program's stdin remain separate.
- A real cleanup failure: unexpected content prevents removal of the staging
  directory. The report says `failed`; unrelated content survives. Only the
  independent harness removes that leftover fixture data.
- Two daemon-owned masters to the same test server, distinguished by their actual
  SSH connection tuples. A local Python driver receives the selected config and
  socket as non-secret argv. After one master closes, selecting its old socket
  returns SSH 255 without authenticating again; its sibling still works.

The local driver runs under the same public Hovel retained-session/confirmed
operation mechanism on the operator host. Its outer artifact contains `localExit`
(the fixture deliberately exits 23), while its inert JSON stdout separately
reports the SSH child's status. Burrow must not interpret arbitrary tool stdout
as a trusted remote result. Hovel confirms and records the outer operation, not
each command an unrestricted local script sends through the socket. Raw external
socket use remains outside Hovel confirmation and artifact collection. Local
cancellation can terminate the local process group; it does not establish remote
termination or cleanup. Use the supervised remote-run path when those guarantees
are required. No legacy environment-variable API or plugin framework is added.

### Proposed initial limits for owner review

- Retain a 65,536-byte prefix of **each** stream with exact discarded-byte counts;
  show truncation explicitly. Results are available after completion. Collection
  persists them in Hovel artifacts; uncollected output can be lost, as already
  accepted. Larger/full output and live output streaming are subsequent work.
- The supervised remote path requires Linux with Python 3 and the selected
  interpreter. The fixture uses pinned Python 3.12; this does not prove arbitrary
  targets have it. Implementation must check prerequisites and refuse clearly,
  without silently downgrading cancellation or installing remote software.
- Process-group cancellation covers ordinary descendants, not escaped/daemonized
  children. Transport loss keeps completion and cleanup unknown. The local-tool
  path makes no remote termination/cleanup promise.
- The 64 KiB request and 4 KiB streamed-script bounds are disposable harness
  limits. They are **not** product script-size limits. Production must transport
  operator inputs incrementally and preserve independent stdin/cancellation.

No credentials were used in operator inputs: the gate uses generated fixture
keys and inert data only. General secret-bearing script inputs/output require
the existing credential/retention contract, not generic saved configuration.
This proof does not validate arbitrary script safety, hostile-target result
integrity, production portability, or full LazySSH feature completion.

The comparison was traced against LazySSH's pinned
[plugin execution source](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/plugin_manager.py)
and [execution tests](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_plugin_manager.py):
interpreter/argv construction, selected connection context, separate output
streams and failure status inform this fixture. Legacy discovery/environment
compatibility is already excluded. Unlike LazySSH's streaming entrypoint, this
bounded retained proof exposes final output only; that difference requires the
owner's explicit initial-scope decision, not an assertion of full parity.
