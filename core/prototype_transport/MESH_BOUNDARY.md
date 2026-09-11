# Existing-tunnel workaround on current Hovel

For [Can Hovel chains discover and consume existing Burrow tunnels?](https://github.com/Bochner/burrow/issues/27).
The owner requested a working Burrow workaround on current Hovel, with necessary
upstream gaps handed off as `hovel`-labeled Burrow issues rather than blocking on
the end state. This remains a disposable, controlled-fixture proof.

Run `aspect burrow-prototype transport` or `aspect burrow-check`. Pins and host
requirements are in [README.md](README.md). Hovel v0.4.2 and SDK/source
`c461ba282a8aecc7aa3a079a4613bf5e2640c388` were still the latest published release
and upstream main respectively when checked on 2026-09-11. No Hovel patch,
private import, additional dependency or second live-state registry is used.

## Working path

1. An explicit confirmed Hovel run starts the fixture's connection owner and
   operator-requested OpenSSH forwards. Consumers never create those resources.
2. A frontend selects an existing connection session using Hovel's session
   inventory and queries `RunSessionCommand(tunnel-list)`. The authoritative
   tunnel map stays inside that retained connection owner.
3. The compatible Burrow chain consumer takes `proof_session` and `proof_tunnel`
   IDs as its input binding. It calls `RunSessionCommand(tunnel-probe)` through
   the public Unix-socket API. The owner validates the selection and performs
   an inert HTTP nonce exchange over the existing forward.
4. The chain records the nonce and selected endpoint metadata in a normal Hovel
   artifact. Completing the consumer closes its HTTP/SSH client, retaining the
   shared connection and tunnel. Missing selections fail instead of reconnecting.

This consumer deliberately runs its bounded traffic operation in the owner via
a structured request. It does not export a general stream to an arbitrary module
or automatically produce a typed `TransportEndpoint` capability.

Each tunnel has an opaque creation ID, owning connection, direction, mode, bind
address and observed reverse bind. Local/reverse forwards have fixed
destinations. A SOCKS tunnel has `mode=socks5` and no fixed destination;
`probeDestination` is explicitly the test's HTTP target, not a tunnel limitation.
Recreating an endpoint on the same port produces a different identity.

## Checks passed

- Real confirmed chain HTTP exchanges twice through each local, reverse and
  SOCKS forward. Reverse requests originate in a Python process launched on the
  SSH target and connect to the remote listener; reverse is not an outbound proxy.
- Concurrent consumers, independent frontend requests and consumer completion
  leave the same owner and tunnel inventory alive.
- Explicit per-tunnel close removes its selection and leaves siblings usable.
  Connection close, missing IDs, old IDs after same-port recreation and master
  loss all refuse further consumption without reconnect or fallback.
- Empty bind host defaults to `127.0.0.1`; explicit `127.0.0.2` is exercised.
- Reverse `GatewayPorts no` overriding a wildcard request, and `yes` overriding
  a loopback request, are detected by inspecting the remote Linux kernel's TCP
  listener tables. The fixture refuses and closes its connection/resources.
  `clientspecified` honors the explicit address and carries real chain traffic.
- SOCKS HTTP to a `localhost` hostname succeeds. Go 1.26.5's standard HTTP
  transport handles `socks5h` and sends non-IP names as SOCKS FQDN requests
  (`src/net/http/socks_bundle.go`); resolution is delegated to the SSH side.
  This is source tracing plus a real hostname request, not a DNS packet capture.
- Nonce validation, bounded requests/output, dangerous-run confirmation and
  normal artifact materialization are retained. Proxy credentials and Mesh
  bearer tokens are neither needed nor generated. Ambient HTTP proxies are
  bypassed by the explicit Go transport and remote Python proxy configuration.

All tests use temporary local Linux SSH servers with explicit fixture-key trust.
The Ubuntu VM was not needed. The local server is the remote SSH endpoint in
these tests; this is not a separate-machine networking or platform-matrix claim.

## Why the workaround is needed

`ListMeshListeners` starts a fresh module subprocess. Merely adding that method
to a retained module cannot expose its in-memory inventory. The earlier probe
still reproduces this with different process IDs and a live original master.

Hovel's session-command broker does reach the retained session object. Its SDK
uses `PayloadCommandProvider`, and both that interface and the daemon RPC table
describe installed-payload use. The runtime accepts this ordinary connection
session without any fabricated installed payload. This is the narrow contract
gap to hand off: explicitly support this existing structured-command route for
non-payload connection sessions, preserving request isolation and lifecycle.
It does not require changing Hovel to run the current bounded workaround.

Source trace at the pinned revision:

- `core/internal/moduleruntime/pythonrpc/runner.go`: `ListMeshListeners` calls
  `callMeshProvider`/`callProvider`, which starts and disposes a process;
  `SessionBroker.RunSessionCommand` uses the retained session's process.
- `sdk/go/hovel/session.go`: `sessionManager.runCommand` asserts
  `PayloadCommandProvider` on the selected session object.
- `sdk/go/hovel/payload.go` and the session operations table in
  `docs/site/src/content/spec/daemon-rpc.html`: documented payload semantics.

These internal files are evidence, never Burrow imports.

## Deliberate limits

- No generic Mesh bridge, arbitrary third-party module routing, UDP, proxy
  authentication, IPv6 or production terminal UI. They are not necessary for
  this compatible-consumer proof and have not been silently implemented.
- Fixture tunnels are established as a bounded initial batch, maximum four,
  to controlled HTTP destinations. Production add/edit UX is not demonstrated.
- Per-tunnel close uses an explicit operator `WriteSession` command and emits
  the existing bounded SDK milestone. Hovel does not automatically run throw
  confirmation for every session command; this is not claimed as durable audit
  parity or permission to expose arbitrary mutating commands through this path.
- Closing a tunnel waits for its active, bounded probes. General streams will
  need explicit cancellation. A failed/ambiguous close is an error, never proof
  that cleanup did not execute.
- Reverse verification requires readable Linux `/proc/net/tcp*` and Python on
  the controlled target. Unverifiable/mismatched exposure fails closed. A server
  may briefly bind a wider address before the mismatch is observed and cleanup
  completes; prevent that in server policy where temporary exposure is forbidden.
- Direct external manipulation of the OpenSSH control socket is outside this
  owner's inventory contract. OpenSSH has no general forward-enumeration query;
  a same-user external actor replacing a listener at the same address is not
  covered. Workspace isolation is not a same-user security boundary.
- The existing retained-log limit and script caller-disconnect limitations remain
  unchanged. A green observation gate includes their expected reproductions.
