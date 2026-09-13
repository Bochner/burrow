# Fresh consumers of the retained SSH manager — issue #72

Bounded Linux proof on the local `mvp1` branch, starting at `6c709bf`.
The owner accepted the proof and requested #72 closeout after reviewing the
results. Retain the #71 candidate with the bounded consumer; generic Mesh streams
remain deferred. Production integration is separate work in #73.

## Question and accepted decision

Can a newly launched confirmed Hovel consumer use the existing workspace manager
and SSH connection without another owner or SSH login?

The bounded owner-mediated HTTP consumer works through public session commands.
Retain that candidate for the proposed #73 integration and keep generic Mesh
routing deferred. The one generic TCP investigation fails at foreign-session
adoption; it does not justify replacing the working manager or implementing a
new Hovel service. Production L/R/SOCKS consumption remains #62, with SMB/WinRM
and arbitrary third-party stream routing outside this proof.

## Runnable evidence

```
aspect burrow-prototype consumer-check
aspect burrow-check
```

The focused target is `//core/prototype_manager:consumer_check`; its Python
fixture extends the #71 setup and uses the same base `burrow@0.1.0` module.
It runs against Hovel v0.4.2 / SDK commit
`c461ba282a8aecc7aa3a079a4613bf5e2640c388`, with the declared
`linuxserver/openssh-server@sha256:47f82202bcbffe214cea77dc7f5ed24b875e81ce6ebf0e0368e2954727bf5116`
image and synthetic key. No new dependency or production adapter is added.

Final validation passed: `aspect burrow-check` built the packages/metadata,
passed all 18 portable checks, and passed the production SSH/terminal fixture
(252.1 seconds), #71 manager fixture (60.4 seconds), and #72 consumer fixture
(50.7 seconds). Standards and Spec reviews, including the verifier correction,
reported no outstanding findings. These are behavior-check durations, not
connection latency benchmarks.

The controlled HTTP nonce fixture comes from `prototype_transport/check.py` and
the fixed-forward/HTTP approach from `prototype_transport/tunnel_boundary.go`.
For the Docker network boundary, a fixture-only reverse loopback relay makes the
host HTTP server reachable at container loopback. The manager's local forward
connects to that relay; both hops use the same authenticated master. HTTP
requests are observed by the fixture, not inferred from a returned ID. This is
one local-forward consumer proof, not a new claim of full L/R/SOCKS parity.

The check covers:

- A newly launched adapter performs a real confirmed throw, forwards the exact
  selected request, and carries a unique nonce through the retained manager.
  Adapter and owner PIDs differ; the connection/master PID is unchanged.
- Workspace, owner session/generation, connection creation, tunnel creation,
  bind, destination, action, flow and nonce are frozen in the reviewed digest.
  The actual adapter refuses altered request/action/owner inputs. Launch-key
  refusal prevents traffic. Completed flow IDs cannot be replayed.
- Actual Hovel throw plans reference confirmations and the successful run.
  Ordinary artifacts contain the returned nonce and complete non-secret
  selection, owner/master/adapter correlation, run ID and reviewed digest.
  They are collected evidence; live status is not treated as an audit record.
  The verifier checks every captured successful consumer after daemon shutdown,
  including the actual confirmation row and exact request in its plan. The
  frontend also checks the summary's run ID against the outer Hovel throw result.
- Stale generations/session/connection/tunnel selections, wrong workspace,
  bind or destination fail without traffic. Duplicate binds are refused.
  Closing and recreating a forward on the same port produces a new tunnel ID;
  the old one stays invalid and sibling traffic still works.
- A stalled consumer leaves another connection's consumer usable. Killing the
  calling frontend does not imply cancellation: the bounded owner operation may
  finish. Explicit flow close requests cancellation of that flow, and a separate
  status observation confirms completion. Both preserve the master and sibling
  traffic; neither closes the manager session.
- Killing the manager during traffic fails the active consumer, terminates both
  masters, and refuses later consumption/automatic owner replacement. There is
  no consumer login or reconnect path. Reconnect remains explicit.

Forward allocation and consumer execution use real confirmed throws. Explicit
tunnel close and flow cancellation are bounded session controls, not separately
persisted throw approvals. As in #71, a passed run ID is correlation, not a signed
authorization credential; same-user privileged API access is outside the workspace
isolation boundary. The implementation preserves the accepted ordering in
[ADR 0001](../adr/0001-manual-connection-approval.md).

## Exactly one generic TCP investigation

The proof calls the real daemon `OpenMeshStream` RPC with the selected resource,
TCP protocol and fixture destination. Its fresh provider verifies the retained
owner through public identity control, observes a different PID, and returns
that existing owner's session ID. The daemon rejects it as `already tracked`.
The original owner and sibling traffic remain usable after this refusal.

This deliberately negative experiment does **not** demonstrate generic byte
transport or successful stream adoption/close routing. It rejects the shortcut
of returning an ID owned by another process. It does not prove that every
possible explicit adapter is impossible. The working HTTP path above executes
inside the owner and is the accepted bounded consumer, not a generic stream.

Pinned source explains the exact boundary:

- [`Runner.callMeshStream`](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/moduleruntime/pythonrpc/runner.go)
  starts a fresh credential-operation process and calls its stream method.
  It passes that process and the returned session to `SessionBroker.adopt`.
  The broker refuses an already tracked ID. For a new ID it attaches the fresh
  process; returning an untracked ID allocated elsewhere would not route reads,
  writes or close to the retained owner either.
- [`MeshContext.OpenSession`](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/mesh.go)
  registers a session in the current provider's registry, not in a different
  manager's process. The public method is not a retained-session rebind API.
- [`The public Mesh contract`](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/mesh.html)
  and [daemon RPC contract](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/daemon-rpc.html)
  describe provider-owned flows and daemon-owned bridge endpoints. Context7's
  current Hovel documentation was checked on 2026-09-12; pinned source governs
  this runtime proof. Internal source is evidence only, never an import.

Smallest proposed upstream handoff: explicitly support an owner-bound session
flow reference from a retained non-payload session, preserving its existing
broker process for byte reads/writes and closing only that flow. It must retain
workspace/resource generation checks and planned execution constraints. The
earlier structured non-payload command handoff (#34) remains useful independently.
An explicit adapter-owned proxy with a new bounded flow protocol is another
possible investigation; this ticket does not build or approve it.

Direct `command`, `execute`, `upload_execute` and `load` Mesh tasks are rejected.
No `OpenMeshBridge` call is made, so no capability token is generated, persisted
or exported. Generic bridges remain deferred; creating a private bridge service
or copying session IDs into configuration would not fix the demonstrated boundary.

## Deliberate proof limits

One fixed IPv4 loopback forward and HTTP nonce exchange; no production tunnel UX,
arbitrary TCP proxy, transport endpoint discovery, remote-host matrix, secrets,
new daemon, second public module, or independent live-state registry. The owner
keeps its tunnel/flow state in memory. The I/O deadline is eight seconds;
operations serialize per master and there are at most 128 retained flow IDs.
Unknown allocation/close acknowledgement keeps the bind reserved and unusable;
full connection teardown remains available. Same-user external socket/listener
replacement is outside the existing ownership model, as in the #27 proof.

Verification finding: opening the private workspace SQLite database repeatedly
from Python while Hovel was running produced missing completion records, and
explicitly closing those live readers also reproduced a malformed-database
error. Moving all SQL inspection after daemon exit preserved the same strict
checks and passed three concurrent fixture repetitions (49.3–50.7 seconds).
The precise cross-runtime SQLite locking cause is not established. This proof
uses public RPC during operation and inspects durable SQL evidence only after
shutdown; it does not introduce live database reads into Burrow or claim that
arbitrary external SQLite inspection is safe.

Owner decision after the runnable proof and review: accept the bounded consumer
and retain the manager candidate, with this generic-stream boundary explicitly
deferred before #73 production migration. Proof commits are `676a2ef` and
`a8b9f72`; the owner authorized tracker closeout while keeping all commits local.
