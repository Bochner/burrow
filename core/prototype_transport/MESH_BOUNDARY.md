# Disposable existing-tunnel discovery prerequisite

For [Can Hovel chains discover and consume existing Burrow tunnels?](https://github.com/Bochner/burrow/issues/27).
Run `aspect burrow-prototype transport`. Uses the existing pinned local SSH and
Hovel fixtures described in [README.md](README.md); no remote lab access needed.
This is a dispatch/selection proof, **not completed tunnel-chain integration**.

## Question and bounded result

Can a chain reach the already-retained connection owner through public Hovel
contracts, without keeping another registry or recreating the connection?

The Mesh listener RPC starts a fresh module subprocess. Its `ownership.connection`
is nil even while the original owner and SSH master remain alive. The check
compares process identities across two discovery calls and verifies the existing
master remains usable. A Mesh interface on the retained module alone therefore
does not make that owner's inventory discoverable.

Hovel also has public `ListSessionCommands` and `RunSessionCommand` operations.
The SDK dispatches these to `PayloadCommandProvider` on the selected **Session**.
That route reaches the retained owner; it does not require registering an
installed payload. The prototype exposes only `connection-status`, verifies the
master with bounded `ssh -O check`, rejects reconnect requests, and reports
non-secret owner identity.

A real confirmed Hovel chain selects that session by configuration and calls
`RunSessionCommand` through the fixture's Unix daemon socket. Two runs reach the
same owner. Missing selections fail; direct queries after owner close fail too.
Consumer completion leaves the owner and master alive. No Mesh bearer capability
or credentials are created or included in chain outputs.

## Contract caveat for owner discussion

The SDK names and documents `PayloadCommandProvider` as commands against an
installed payload. The daemon RPC reference also describes both session-command
methods as operating through an installed payload session, although runtime
dispatch does not enforce that restriction.
Using it for connection/tunnel inventory is mechanically supported at this pin,
but its intended long-term use for non-payload connections is not established.
An SSH connection remains a connection, never a fabricated installed payload.

Candidate direction: select the existing connection session first, query its
owner for compatible live tunnels through structured session commands, then pass
the selected tunnel identity to a compatible consumer. Keep the authoritative
inventory in that owner. Do not infer automatic Mesh discovery from this result.

## Still required before resolving the parent decision

- Actual owner-maintained tunnel inventory and consumer binding, including
  connection/tunnel identity, direction, effective bind and destination.
- Chain data traffic through local, reverse and SOCKS forwards; two consumers,
  per-tunnel removal and transport-loss races. Existing transport tests alone
  do not establish these chain properties.
- SOCKS DNS behavior, reverse `GatewayPorts` policy and effective exposure.
- Confirmation/audit treatment of any future mutating session commands. This
  probe exposes read-only status only; direct session commands are not presumed
  to run the confirmed throw workflow.
- Owner feedback on this proposed public-contract usage before adoption.

## Source trace

Hovel source baseline `c461ba282a8aecc7aa3a079a4613bf5e2640c388`:

- `core/internal/moduleruntime/pythonrpc/runner.go`: `ListMeshListeners` →
  `callMeshProvider` → `callProvider` starts and disposes a process per call;
  `SessionBroker.RunSessionCommand` uses the retained session's process.
- `sdk/go/hovel/session.go`: `sessionManager.runCommand` asserts
  `PayloadCommandProvider` on the session object.
- `sdk/go/hovel/payload.go`: public command request/result shape and documented
  installed-payload semantics.
- `core/internal/adapters/daemonrpc/daemonrpc.go`: public session command RPCs.
- `docs/site/src/content/spec/daemon-rpc.html`, session operations table:
  documented installed-payload restriction.

These internal files were inspected as evidence only; Burrow imports the public
SDK and calls public daemon RPCs.
