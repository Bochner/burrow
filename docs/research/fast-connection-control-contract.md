# Fast connection control contract

Research for [issue #70](https://github.com/Bochner/burrow/issues/70), 2026-09-12.
This is evidence and a recommendation, not an owner-approved architecture or
proof of production readiness. The accepted manual interaction is recorded in
[ADR 0001](../adr/0001-manual-connection-approval.md).

## Sources and scope

Inspected local Hovel source at
`c461ba282a8aecc7aa3a079a4613bf5e2640c388`. Context7 resolved
`/vibepwners/hovel` and returned its public daemon RPC and plan/review/confirm
documentation; because that documentation tracks main, the pinned source below
owns version-specific conclusions. No Hovel internal package is proposed as a
Burrow dependency.

## Public setup can avoid repeated CLI launches

The public daemon transport is HTTP POST with JSON bodies, normally through the
workspace Unix socket. It is separate from the framed JSON-RPC module protocol.
The documented operator methods include `CreateOperation`, `CreateChain`,
`AddModule`, `AddTarget`, `SetChainConfig`, and `Snapshot`.
[Public RPC contract](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/daemon-rpc.html).

These cover Burrow's existing six setup CLI calls: create operation and chain,
add the base module and target, and set workspace and connection configuration.
Supply explicit `Operation` and `Chain` fields rather than relying on another
frontend's selection. `ModuleRequest` additionally accepts `ModuleID` and
`StepID`; `ConfigRequest` accepts string `Key` and `Value`. The daemon owns
persistence: `withChainAccess` serializes the mutation, attaches the requested
operation/chain, and calls `persistLocked`. `AddModule` and `SetChainConfig`
also publish operator logs.
[Request shapes and handlers](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonrpc/daemonrpc.go#L1810),
[persistence boundary](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonrpc/daemonrpc.go#L3484).

**Recommendation:** first replace only these setup subprocesses through Burrow's
existing verified `launch.Call` transport, retaining module installation/catalog
checks and final CLI throw. This changes dispatch overhead without changing
connection ownership. Setup calls are individually persisted, not one atomic
transaction: stop on failure and do not execute a partially configured chain.
This is a source-supported implementation candidate, not a measured speedup.
Current boundaries are in [connection commands](../../core/connection/commands.go)
and [launch operations](../../core/launch/operations_linux.go).

## Preserve the actual throw coordinator

The CLI `throwHandler` resolves the modules and inputs, checks dangerous-module
allowance, constructs and records the plan, records confirmation and structured
events, checks launch-key readiness, and then executes the throw. `--now` records
confirmation method `now_bypass`; it does not skip the later launch-key check.
Thus a recap's affirmative action can retain Burrow's existing invocation of
`throw --now --allow-dangerous --json` without another interactive approval
screen or a new permission model.
[Pinned throw coordinator](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/app/commands/catalog.go#L3653).

`CreatePendingThrow`, `ConfirmPendingThrow`, `RequirePendingThrowReady`, and
`CancelPendingThrow` coordinate launch-key approval for a supplied plan hash and
policy flags. They do not compute the resolved throw plan or replace its durable
plan/confirmation/event recording. `ConfirmPendingThrow` binds entity ID, pending
ID, hash, and flags. Calling these methods followed by `ExecuteModule` is not
demonstrated equivalent to the coordinator above.
[Pending-throw handlers](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonrpc/daemonrpc.go#L2042).

## Retained-manager routing remains a separate proof

The public SDK supports `Context.OpenSession`: the session outlives `Run`, and
the daemon retains its module process. A session implementing
`PayloadCommandProvider` can expose command listing/execution through public
`ListSessionCommands` and `RunSessionCommand` RPCs.
[SDK Context](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/context.go#L246),
[session dispatch](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/session.go#L401).

A confirmed short-lived base `burrow` adapter could, in principle, forward its
resolved request to a retained base-module manager. This still launches the
adapter for every throw; it does not eliminate all subprocesses. SDK Context
exposes run/module/target/configuration but no daemon-control endpoint or signed
approval receipt. Burrow would need explicit workspace information and its
verified public RPC transport, not an import of Hovel internals.
[SDK fields](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/context.go#L246).

Critically, `RunSessionCommand` forwards directly to the session broker; it does
not invoke the throw coordinator or require a plan hash. An adapter calling it
after approval preserves that caller's ordering, but does not itself establish
an enforceable correspondence between the manager command and the approved
throw. A naked frontend `connect` session command must not be described as
preserving the existing throw contract. Whether a supported receipt/binding is
needed, and available without a new permission scheme, remains unproven.
[RPC forwarding](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonrpc/daemonrpc.go#L1767).

## Aggregate log ceiling

The pinned module RPC client retains at most **256 `module/log` notifications**.
The 257th returns an error; `readLoop` then calls `finish`, records the read error
and closes the client completion channel. `logsSnapshot` copies rather than
drains the buffer, and streaming callbacks do not remove entries. For an ordinary
retained manager, this is cumulative across its connections and commands, not a
per-command or per-connection allowance. It is not a rotating log tail. A
separate 256 KiB cap applies to notification parameter bytes.
[Limits and handling](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/moduleruntime/pythonrpc/runner.go#L2946),
[boundary tests](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/moduleruntime/pythonrpc/resource_limits_test.go#L44).

Do not promise automatic truncation, reset after each command, or survival of
all manager connections after overflow. The next proof must check exact cleanup
and observed connection states after the RPC client fails. Remaining acceptance
work includes changed-plan/launch-policy refusal, cancellation and sibling
isolation, manager crash scope, cold/warm timing, authentication latency measured
separately, and a fallback retaining current owners if manager routing fails.

## Current baseline observation

On 2026-09-12, `aspect burrow ssh-smoke` passed using Burrow `a725e7d` plus
fixture-only timing prints, the declared digest-pinned OpenSSH image, synthetic
Ed25519 credentials and a fresh temporary workspace/cache on this Linux amd64
host. Setup took 4.4 seconds. Approved CLI submission-to-return measured:

| Operation | Seconds | Interpretation |
| --- | ---: | --- |
| First connect, unknown host | 6.627 | First control submission after setup; subsequently refused by the existing host-trust policy, not authenticated success. |
| Explicit reconnect with independently supplied fixture trust | 7.132 | Later submission on the same daemon; successful key authentication verified by the fixture. |
| Connect another named connection | 6.660 | Warm daemon/module path; successful key authentication and retention subsequently verified. |

The command can return before authentication completes. These three observations
are not submission-to-dispatch, prompt latency, full-connection percentiles or a
manager benchmark. The smoke check also passed saved-profile reuse, selected
close and cleanup; it is not the full password/PTY/failure matrix. The fixture
prints these durations on future runs without recording credentials or adding
runtime instrumentation. #71 must instrument the separate phases in ADR 0001
and collect enough baseline/candidate samples to apply its acceptance criterion.

Source inspection counts **eight Hovel CLI launches on the already-installed
connect path**: one installed-module inventory query, six setup commands and one
throw. A mismatched installation adds the install invocation; initial workspace
setup, child module invocations and OpenSSH processes are additional work. This
is a source count, not a measured process-start trace. It differs from #73's
historical seven-invocation path, so comparisons must use a newly measured common
baseline rather than silently treating the historical process count as current.
