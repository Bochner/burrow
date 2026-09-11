# Hovel diagnostics, progress, confirmations, and audit

Research baseline: 2026-09-11. Inspected Hovel commit
`c461ba282a8aecc7aa3a079a4613bf5e2640c388`; GitHub's `commits/main`
endpoint returned the same SHA during this investigation. Source inspection,
not a live Hovel/Burrow integration test. Resolves the factual investigation in
[How does Hovel handle diagnostics, progress, confirmations, and audit?](https://github.com/Bochner/burrow/issues/4).
Recommendations below do not settle the dependent architecture decisions.

## Answer

Reuse the public SDK logger for bounded, non-secret operator diagnostics and
milestones. Hovel correlates ordinary run logs, stores events, and publishes a
frontend log view. That does **not** make every message a confirmation record,
a durable Mesh operation ledger, or a typed transfer-progress update. Burrow
must preserve these separate contracts and verify the selected execution path.
[SDK logger][logger], [run event conversion][runner], [publishing sink][runtime],
[Mesh bookkeeping][meshbook].

## Routes and ownership

| Information | Public entry point | Downstream behavior and limit |
| --- | --- | --- |
| Ordinary run diagnostics | `ctx.Log.Debug/Info/Warn/Error(message, key, value, ...)` | `module/log` JSON-RPC notification; daemon creates `hovel.module.log`, persists through its event sink, appends a chain log, and publishes it. |
| Live operator log view | Daemon `PollLogs`, with sequence cursor and optional operation/chain filtering | Unary polling over a bounded in-memory broker; separate from durable event storage. `ActiveLogs` exposes the active operator-session view. |
| Progress milestones | Ordinary SDK log messages/fields | Visible logging, without a dedicated generic byte-count/percentage/update-in-place protocol in the inspected logger. |
| Interactive output | SDK session read/write and daemon session methods | Separate byte stream with bounded broker history; does not become a structured log or command audit record merely by being displayed. |
| Final facts/evidence | `Result.Outputs`, `Findings`, `Artifacts`, `Sessions` | Explicit run result categories; use the category matching the information rather than encoding all evidence in logs. |
| Mesh task evidence | `MeshTaskResult.Events` containing `MeshEvent` | Provider-authored result evidence; not proof of an independently durable audit ledger or a live progress subscription. |

Sources: [SDK logger][logger], [SDK server][server], [runner][runner],
[daemon publishing sink][runtime], [public daemon contract][rpcdoc],
[session history][history], [result types][results], [Mesh types][meshsdk].

The daemon's default event sink is its SQLite store. `publishingEventSink.Append`
first calls that sink and returns on failure; only afterward does it create the
operator log entry, publish it, and persist the configured session snapshot.
SQLite's `RecordEvent` stores event identifiers, timestamps, type, message,
references and JSON fields. These writes are separate steps, so a failed later
publication/snapshot write is not evidence that the earlier event insert was
rolled back. [Default wiring][wiring], [sink ordering][runtime], [SQLite events][sqlite].

For ordinary run logs, the runner attaches operation, chain, run ID, module ID
and target from the host's run request, and assigns its own event ID/time. It
stringifies extra field values with `fmt.Sprint`; nested JSON structure is not
preserved as a typed progress schema. It does not add a throw ID or SSH
connection/transfer ID in this conversion. The operator projection adds elapsed
time and the `operation/<operation>/chain/<chain>/logs` topic. Burrow can add
non-secret connection/transfer identifiers as diagnostic fields, but that would
be a Burrow convention, not a Hovel progress contract. Avoid field names
`message`, `level`, `logger`, and `exception`: extra fields can overwrite these
runner field-map entries. [Conversion][runner], [operator projection][runtime].

Two concrete ceilings matter for a long-running SSH module:

- The RPC client retains at most **256 module log notifications** and returns an
  error for the next one. A live callback does not drain this retained slice.
  This is per RPC-client lifetime, not a per-second allowance. High-frequency
  transfer updates can therefore fail the module protocol path; merely lowering
  their frequency does not make an indefinitely retained process safe.
- The daemon live log broker retains **4096 entries** by default and overwrites
  the oldest. Its sequence cursor is process-local; the poll response returns
  the current sequence and retained tail, not a durable replay guarantee.

Sources: [notification handling and callback][runner],
[resource-limit regression tests][limitstests], [daemon broker][rpcsource].

A feasibility test must exercise logs after `Run` returns while an SSH session
keeps the module process alive; the existing source route is not sufficient
proof that an unlimited connection lifetime can produce unlimited diagnostics.
For initial progress, bounded milestones plus a UI-local spinner are a plausible
option; sustained byte updates need a proven route or upstream contract change.

## Redaction and terminal boundaries

The SDK recursively redacts specific credential wrapper types in structured
field values, including `CredentialBytes`, protected paths, material values,
and credential references. It does **not** treat arbitrary strings as secrets,
and the log message is emitted unchanged. Converting a credential to raw
strings/bytes before logging defeats the typed protection. The runner then
copies messages and stringified fields into durable events; no general
secret-value scrubber appears in that ordinary log route. [SDK sanitation][logger],
[runner event conversion][runner].

Credential-provider calls and operation credential delivery explicitly mark
the process as credential-bearing. This clears buffered logs/callbacks,
suppresses subsequent log/session notifications, and sanitizes diagnostic
errors; process stderr receives related suppression. This special path must
not be generalized to ordinary SSH-password inputs: a field being called
`password` does not demonstrate that this process mode is active. It also
means diagnostic visibility after using credential delivery must be tested,
not assumed. [Credential-bearing transitions and notification handling][runner].

Stdout is exclusively framed JSON-RPC; stderr is for crash diagnostics and can
be included in ordinary module failure errors. Never send credentials, child
process output, or ANSI rendering there. Hovel's terminal log renderer styles
and wraps messages and fields; inspection found no explicit control-sequence
sanitizer in that renderer. Plain/no-color output is not proof that
remote-controlled escape sequences are harmless. Burrow should sanitize remote
text before putting it in diagnostic labels/messages, while keeping intentional
interactive terminal bytes on their separate session channel. [Module rules][moddoc],
[stderr failure composition][runner], [terminal renderer][renderer].

## Confirmation and audit obligations

Hovel's module guide requires potentially dangerous modules—including command
execution, disk writes and listener creation—to declare `dangerous` and the
necessary confirmation requirements. Frontends must share planning, validation,
scope guardrails and audit behavior. A custom Burrow UI or local script must
reuse the relevant public daemon execution surface rather than invent an
independent yes/no prompt and call it equivalent. [Module obligations][moddoc],
[frontend contract][frontends].

The inspected throw command records a plan and a `hovel.throw.planned` event,
looks up confirmation by the current plan hash, and records a confirmation plus
`hovel.throw.confirmed` when needed. Interactive acceptance is `typed_yes`;
`--now` is recorded as `now_bypass`. Preconfirmation is reused only for the
matching hash. The path also calls the launch-key readiness gate. Plan and
confirmation records are stored separately from free-form logs. A module
message saying “confirmed” is therefore not equivalent. [Throw path][commands],
[confirmation storage][sqlite], [documented review semantics][frontends].

Do not extend those throw guarantees automatically to every Mesh/session RPC.
The SDK describes `MeshEvent` as provider-authored audit or progress evidence,
but the daemon's `MeshBook` explicitly documents a bounded, process-local
recent-operation view that does not survive restart. Its task/stream/listener
handlers update that book; those records alone cannot satisfy durable audit
requirements. Session output history is likewise bounded memory (10 MiB default)
and not an immutable transcript of each command the operator intended.
[Mesh types][meshsdk], [Mesh bookkeeping][meshbook], [Mesh handlers][rpcsource],
[session history][history].

## Decisions this evidence enables

1. Reuse SDK logging; decide a bounded progress policy and prove retained-session
   diagnostics before specifying continuous transfer updates.
2. In each of the three scripting decisions, identify the exact daemon
   operation, confirmation/launch-key enforcement, result/output retention, and
   durable audit evidence. Mesh routing does not settle these automatically.
3. Keep Hovel-owned event/confirmation storage in Hovel. Decide which Burrow
   connection/transfer correlation fields and user-visible errors are needed;
   do not duplicate Hovel's event database.
4. Require a focused integration check for non-secret correlated logs, injected
   remote control characters, a credential-bearing operation, stale-plan
   confirmation rejection, and log limits once the selected implementation
   exists. No SSH runtime or new logger is implemented by this research.

## Validation

Read the owning source paths and existing resource-limit tests. Verified the
upstream main SHA through GitHub. No runtime behavior test was executed: this
artifact is research only. The repository's `//:research` Aspect gate validates
metadata/build-graph inputs, not these prose claims or a functioning SSH module.
The metadata gate passed using the existing pinned Aspect binary outside PATH:

```sh
rtk proxy env PATH=/tmp/burrow-research.cW39N9:/usr/local/bin:/usr/bin:/bin \
  /tmp/burrow-research.cW39N9/aspect-cli-x86_64-unknown-linux-musl build //:research
```

The temporary tool directory contains the existing Bazel launcher; Aspect owns
its invocation. No new dependency was installed for this investigation.

[logger]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/context.go
[server]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/server.go
[runner]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/moduleruntime/pythonrpc/runner.go
[runtime]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/infra/daemonruntime/runtime.go
[wiring]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonlocal/daemonlocal.go
[sqlite]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/storage/sqlite/store.go
[rpcdoc]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/daemon-rpc.html
[rpcsource]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonrpc/daemonrpc.go
[results]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/result.go
[meshsdk]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/mesh.go
[meshbook]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonrpc/mesh_book.go
[history]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/moduleruntime/pythonrpc/session_history.go
[limitstests]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/moduleruntime/pythonrpc/resource_limits_test.go
[renderer]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/terminallog/renderer.go
[moddoc]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/module-development.html
[frontends]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/front-ends.html
[commands]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/app/commands/catalog.go
