# Retained manager proof — issue #71

Disposable Linux proof of one Hovel-retained manager per workspace, using the
single base `burrow@0.1.0`. The owner accepted the measured 38% warm dispatch
improvement as sufficient for now. Production integration remains #73 after #72.
Run only in the temporary workspaces created by the Aspect tasks:

```
aspect burrow-prototype manager-check
aspect burrow-prototype manager-latency
aspect burrow-prototype consumer-check
```

Requires Docker and the host OpenSSH client tools. Measurement is an explicit
experiment, not a CI latency SLA. `--trace-counts` additionally runs separate
process-count traces and requires strace; that optional pass has encountered
password timeouts under ptrace. The full `aspect burrow-check` includes the
behavior check; CI builds both proof binaries without Docker.

## Boundary and behavior

Activation and each connect use a real `throw --now --allow-dangerous --json`,
isolated per-request operations/chains, and the verified public daemon transport.
The adapter binds workspace, session, generation, creation ID, exact non-secret
request digest and adapter run ID. A dispatch result is distinct from observed
authentication. Atomic reservations serialize first attachment; uncertain owners
are refused, never automatically adopted or replaced.

Session commands **do not certify throw approval**. The supported frontend →
confirmed adapter → selected owner ordering follows
[ADR 0001](../../docs/adr/0001-manual-connection-approval.md); a supplied run ID is
not a signed credential. Same-user callers can use Hovel's privileged APIs.
No private Hovel packages, second public module, connection registry, daemon,
authorization service or audit database are added.

The lab checks two independent frontends, concurrent immutable connects,
cross-workspace names, changed/replayed requests, launch-key refusal, explicit
keys, config-selected keys, the current frontend's synthetic SSH agent, private
passwords and cancellation. `prepare` reuses `connection.Parse` to freeze the
frontend agent socket before request review. Passwords use the existing private
askpass socket/pipe and never become argv, environment or Hovel metadata.
The fixture uses controlled SSH config and synthetic keys, not operator keys.

Known activation generations and creation IDs allow `reconcile` to inspect a
lost acknowledgement without submitting again. The check dismisses activation
and connect before dispatch and closes only the selected stalled attempt after
dispatch. List and sibling close must finish within one second during that prompt.

`quit-review` observes all opened fixture workspaces. `quit` supports keep,
cancel, and digest-bound verified close. Each owner rechecks its entire reviewed
inventory under its admission lock. Changed inventory requires another review;
partial cleanup never promises rollback. Saved settings and evidence survive.
This is the #76 backend contract, not a second terminal dialog. Production UI,
frontend-local shells, transfers and tunnels remain separate implementation work.

One aggregate manager budget allows 200 ordinary logs plus one suppression
warning. Controls remain usable after 265 inventory requests. Manager death kills
its masters; daemon EOF ends the owner and masters. Unknown reservations and
runtime contents remain for manual investigation and explicit reconnect.

## Measurements

`instrument.py` declares generated baseline sources from the actual existing
per-connection implementation, adding monotonic dispatch/connected timestamps
and the same accepted LazySSH host-trust flags. It changes no production source.
A slim wrapper calls existing `Execute` / `ExecutePrompt`; this compares control
paths, not the full shipping TUI or ordinary SSH.

There are 20 cold and 20 warm untraced samples per variant, split equally between
key and password authentication. Cold starts after a clean daemon restart with
packages installed and no manager; submission includes activation. Connected
means verified master publication. Dispatch-to-prompt includes network/server
time; the synthetic reply delay is 20 ms. Human review/typing time is excluded.
Separate traces count successful executable starts, excluding threads and failed
PATH searches; traced durations never enter the phase percentiles.

[Evidence and limitations](../../docs/research/retained-manager-proof.md) and
[retained samples](../../docs/research/retained-manager-measurements.json) include
the incomplete tracing/provenance results. The sampled binary predates the final
prepare/reconcile/quit additions; no final-build speed claim is inferred from it.
Further speed work is deferred per the owner, rather than rerunning benchmarks.

## Fresh consumers — issue #72

`consumer-check` extends this same candidate owner with a disposable, fixed local
forward and the existing HTTP nonce fixture. A new confirmed adapter selects the
workspace, owner session/generation, connection creation, tunnel creation, exact
endpoints, action and unique flow. The owner performs the bounded exchange; the
adapter records a normal result and JSON artifact with those non-secret identities.
The verifier checks exact persisted plans, confirmations and artifacts after the
daemon exits; it does not open the private database while Hovel is operating.
The fixture's reverse loopback relay makes its host HTTP server reachable inside
the pinned SSH container. Both forwarding hops use the existing master.

Run the focused check above or the full `aspect burrow-check`. It checks changed
approved inputs at the actual adapter, launch-key refusal, replay, stale selection,
same-port recreation, concurrent sibling traffic, frontend disconnect, explicit
flow cancellation, and owner loss. Close acknowledgement means cancellation was
requested; `flow-status` separately observes completion. Frontend disconnect does
not promise cancellation: the operation may finish and has an eight-second I/O
deadline. Operations serialize per connection; siblings on another connection
remain usable. Used flow IDs are retained up to a hard limit of 128 in this proof.
Unknown forwarding outcomes reserve their identity/bind and require inspection
or full connection teardown; they never become usable automatically.

The single generic TCP exploration deliberately returns the retained owner's
session ID from a fresh `OpenMeshStream` provider. Hovel rejects adoption with
`already tracked`. This is a **negative routing result**, not a byte-stream or
close-routing success. No bridge endpoint or bearer capability is created; no
execution-capable Mesh task is implemented. No public module identity is added.

See [the #72 evidence and proposed decision](../../docs/research/retained-consumer-proof.md).
Owner acceptance remains pending; production L/R/SOCKS consumers stay in #62 and
manager integration stays in #73. This proof does not migrate production.
