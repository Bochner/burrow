# Existing Burrow tunnels as Hovel chain inputs

Research date: 2026-09-11. Source baseline: Hovel commit
`c461ba282a8aecc7aa3a079a4613bf5e2640c388`, inspected from the preserved
upstream archive used by the SDK proof. This is planning evidence, not an
implemented integration or an approved API design.

For the initial workflow, the owner requires tunnels to be explicitly
established in Burrow first. Hovel chains select and consume those live
tunnels; this consumption path must not create, reconnect, or recreate them.
The investigation therefore concerns exposing existing resources. Later
optional chain automation may explicitly establish connections and tunnels;
its credential delivery and provisioning contracts remain separate proof
questions. This initial boundary does not rule out that later workflow or
authorize storing secrets in chain definitions.

## Public contracts and their limits

| Need | Pinned Hovel contract | Consequence for Burrow |
| --- | --- | --- |
| Discover live endpoints | `MeshListenerProvider.ListMeshListeners` reports IDs, state, addresses, protocols and attributes; topology can report nodes and routes. | A candidate inventory surface exists. A listener is defined as a listening post, so mapping SSH forwards into that vocabulary needs a small proof. No dedicated SSH tunnel registry is established by this research. |
| Bind a chain input | `StepProvider` declares typed requirements and outputs; `TransportEndpoint`, `MeshNode`, `MeshRoute` and `MeshDestination` are public capability types. | Public types exist, but their existence does not prove automatic conversion of discovered listeners into selectable chain inputs. Verify that binding explicitly. |
| Consume one flow | `MeshStreamProvider.OpenMeshStream` returns a session registered through `MeshContext.OpenSession`. | A provider may connect through an already-live tunnel. Its implementation must reject missing or inactive tunnel IDs rather than provisioning a replacement. |
| Start/stop listeners | Separate optional lifecycle interface; stable caller-selected IDs and idempotent operations. | Discovery does not require implementing creation. Chain consumption need not expose listener start at all. |

Sources: [public Mesh interfaces and data shapes](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/mesh.go),
[public chain-step types](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/step.go),
[Mesh terminology and daemon operations](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/mesh.html).

## Consumption is explicit

Hovel documents endpoint substitution: a frontend selects a provider and
route, obtains a bridge, and a cooperating consumer authenticates that local
connection before giving it to its protocol client. This does not establish
transparent interception of arbitrary chain modules' network calls. A raw
third-party tool needs a compatible local adapter if it cannot send the
bridge authentication preface. A tool can also use an ordinary Burrow
forward or SOCKS endpoint directly when its own connection configuration
supports that endpoint; proving Hovel passes that selection is still required.
[Consumer workflow](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/mesh-development.html#L543-L646).

`OpenMeshBridge` creates a new daemon loopback socket and calls the provider
stream opener. It is one authenticated TCP connection or UDP peer association,
not a reusable general-purpose forward port. The Go SDK's `DialMeshBridge`
sends the bearer preface; the token must remain in memory. Using a bridge
therefore creates an ephemeral consumption adapter even if the selected
Burrow tunnel already exists. It must not be confused with creating that
tunnel. Ordinary local forwards and SOCKS listeners should not be described
as daemon Mesh bridges merely because they expose a local port.
[Daemon bridge implementation](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonrpc/mesh_bridge.go#L286-L443),
[SDK authentication helper](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/mesh_bridge.go),
[one-flow bridge contract](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/mesh-development.html#L624-L630).

## Direction, protocols and lifetime

Proposed inventory mapping: preserve the tunnel's direction, bind location,
fixed destination or SOCKS mode, owner connection and live identity. A local
forward offers its fixed destination; it is not an arbitrary destination
route. A reverse forward exposes a listener on the remote side and must not
be advertised as a generic local outbound route. A SOCKS consumer must support
the actual proxy mode, including its DNS behavior. These are mapping
requirements, not capabilities supplied automatically by Mesh. The Mesh
request separates the selected path from destination and protocol, while its
listener shape permits provider-defined kind/protocol metadata.
[Mesh request and listener contracts](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/mesh.go).

Hovel supports TCP and UDP bridge adapters, but UDP specifically requires a
provider session advertising datagram semantics. That is not evidence that
Burrow's selected SSH forwarding transport supports UDP or SOCKS UDP. Advertise
only capabilities the transport proof demonstrates.
[Bridge protocol rules](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/mesh.html#L230-L267).

Hovel explicitly does not guarantee persistence of the subprocess serving a
listener lifecycle RPC. Long-lived listener resources must survive outside
that individual call. Burrow's already-selected surviving OpenSSH master is
the starting point for the ownership proof; introducing a second persistent
registry is not justified by the public types alone. A consumed stream can
outlive its opening RPC through a registered session. Bridge close tears down
that session, so the adapter must avoid treating per-consumer session close as
permission to close the shared SSH connection or operator-created tunnel.
[Listener lifetime ADR](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/adr/0001-use-mesh-for-node-operations.md),
[stream session registration](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/mesh.go#L409-L416),
[bridge close implementation](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonrpc/mesh_bridge.go#L567-L610).

## Focused next proof

Can pinned Hovel discover and let a chain select an already-live Burrow local,
reverse or SOCKS tunnel through its public contracts, consume the appropriate
endpoint, and refuse a stale selection without creating or reconnecting any
tunnel?

The smallest useful proof should establish:

1. Explicit Burrow creation precedes discovery; inventory includes only live
   selectable resources and accurately identifies direction and destination.
2. A real compatible Hovel chain consumer uses the chosen existing endpoint.
   Identify the public binding from inventory to `TransportEndpoint` or Mesh
   input; if absent, record the exact API gap instead of importing internals.
3. Multiple consumer connections use the same existing tunnel. Consumer
   cleanup leaves that tunnel intact; operator tunnel/connection close makes
   its selection unusable. Normal frontend detach retains it.
4. Failure after selection is surfaced and does not recreate resources or
   silently choose another route. Confirm DNS/proxy behavior and validate
   reverse traffic from the remote side separately from outbound routing.
5. Confirm Hovel bookkeeping and approval behavior, and keep credentials and
   any bridge bearer capabilities out of durable chain outputs and logs.

The offered Ubuntu server at `192.168.10.50` can support the later forwarding
proof. No live SSH tests were performed for this source investigation.

## LazySSH bind-address compatibility check

At LazySSH `9eb84452c31cb527bf8e938e23ffc92974fb91cb`, `tunc` accepts
connection, direction, listen port, destination host and destination port.
It converts both ports to integers and calls `SSHManager.create_tunnel`,
which emits `-L port:host:port` or `-R port:host:port`. The host argument is
the **destination**, not the listening bind address. The tunnel wizard is
the only other application caller and passes the same fields; the model and
cancel operation likewise have no separate bind-address field. The reverse
wizard's local/remote prompts are misleading, but its actual `-R` argument
still puts the remote listening port first and the locally reached destination
last. This is not evidence of supported user-selected listen addresses.
[Command and wizard](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/command_mode.py#L564-L603),
[wizard caller](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/command_mode.py#L1757-L1800),
[creation and cancellation](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/ssh.py#L187-L276),
[tunnel model](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/models.py#L8-L26).

SOCKS is configured separately as an integer `dynamic_port`, emitted as
`-D port` when the SSH master is created. No explicit listen address is
emitted there either. Upstream tests mock subprocesses or tunnel creation;
they exercise forward/reverse success and numeric SOCKS options, not actual
socket binding or bind-address choice. No upstream tests were run here.
[SOCKS command construction](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/ssh.py#L50-L70),
[command tests](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_command_mode.py#L866-L929),
[transport tests](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_ssh.py#L230-L255),
[forward/reverse tests](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_ssh.py#L528-L581).

LazySSH consequently delegates binding defaults to OpenSSH configuration.
Local and dynamic forwarding follow client `GatewayPorts` (default `no`,
loopback); remote forwarding follows server policy. Thus the default is
normally loopback, but LazySSH does not enforce that independently of the
environment. OpenSSH supports explicit bind addresses for `-L`, `-R` and
`-D`. Server `GatewayPorts no` forces reverse loopback, `yes` forces wildcard,
and `clientspecified` permits the client's chosen address. An accepted reverse
request alone therefore cannot prove the requested exposure was honored.
[SSH forwarding syntax](https://man.openbsd.org/ssh.1),
[client GatewayPorts](https://man.openbsd.org/ssh_config.5#GatewayPorts),
[server GatewayPorts](https://man.openbsd.org/sshd_config.5#GatewayPorts).

The owner's Burrow decision is explicit loopback defaults with a separate
user-selected listen bind address, subject to actual server support for
reverse forwarding. That is a deliberate improvement over the inspected
LazySSH interface, not a feature-parity claim. Keep bind address and destination
distinct in the specification and prove effective remote exposure.
