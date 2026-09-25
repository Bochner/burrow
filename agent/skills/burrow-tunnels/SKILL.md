---
name: burrow-tunnels
description: Create, verify and remove Burrow local or reverse tunnels and SOCKS, or consume existing endpoints through Hovel chains.
compatibility: Requires the Burrow Linux CLI and an explicitly selected live SSH connection.
metadata:
  burrow-skill-version: "0.2.0"
  burrow-cli-contract: "1"
---

# Forwarding

Load `burrow` and its operating rules. Discover `tunnel.create`, `tunnel.list`
and `tunnel.check`. Resolve workspace, connection, direction, listening endpoint
and destination host/port before creating anything. A reverse listening port
does not identify its destination. “Open a reverse tunnel on 9000” needs that
missing destination clarified, with zero tunnel creation. Reuse explicit intent
already supplied; a request for an authorized tunnel does not need invented
extra approval after its exact recap is checked.

| Direction | Listener | Destination reached from |
| --- | --- | --- |
| `forward` | Operator/daemon host | Remote SSH server |
| `reverse` | Remote SSH server | Operator/daemon host |
| SOCKS | Operator/daemon host | Remote SSH server, selected by each client |

Routine LISTEN ports bind loopback. An explicit broad IP bind changes exposure
and belongs in the review. If the listen port is unspecified, resolve an explicit
port or a random-port request. Reverse LISTEN `0` actually allocates a high port
in 49152–65535; review that request and report the returned actual `listen`.
Do not invent an allocated port from a suggestion, dry-run or unchecked local
availability probe. Forward/SOCKS have no random-zero contract; clarify their
port selection instead of passing unsupported zero.

```sh
burrow --workspace PATH tunnel create NAME reverse 0 127.0.0.1 8080
burrow --workspace PATH tunnel create NAME reverse 0 127.0.0.1 8080 --review HASH --yes
burrow --workspace PATH tunnel list
burrow --workspace PATH tunnel check QUALIFIED_ID
```

This example assumes the user selected the operator's local service on 8080.
Use the actual requested destination; never infer it from the listening port.
For a fixed port or forward, substitute the resolved direction and endpoint.
Verify the returned connection generation, qualified `id`, actual listener,
destination and state. An occupied explicit port fails; report the failure and
preserve existing resources instead of choosing another port without intent.
A rejected review/confirmation authorizes no mutation. An uncertain creation
needs `tunnel list`/`inspect` before retry; missing acknowledgement is not rollback.

`tunnel check` is a passive greeting probe. A silent HTTP/service listener can
be listening but needs a protocol client to prove traffic. Discover `chain.select`
and `chain.http` for the built-in reviewed HTTP GET through an existing tunnel;
its bounded HTTP-only request reports status, bytes and SHA256, not response body.
Use an authorized protocol-specific probe when needed; label allocation, listener
verification and successful destination traffic separately. Report only endpoints
and traffic actually observed.

Discover `tunnel.proxy-create`, `tunnel.proxy-inspect` and `tunnel.proxy-remove`
for `proxy create NAME LISTEN`, `proxy inspect NAME` and `proxy remove NAME`.
Create/remove need review, and accept the digest with `--review HASH --yes`.
SOCKS is unauthenticated, one per connection, separate from L/R IDs and counts.

For requested L/R removal, review `tunnel remove QUALIFIED_ID`, then repeat with
`--yes`; this route does not accept `--review`. Use the exact immutable qualified
ID from inventory, never one synthesized from a port. Removing a listener stops
new connections while accepted streams may finish; siblings and the master stay.
Connection close ends its forwarding. Frontend detach/Keep running retains it.

For saved Hovel automation, use `chain select NAME`, then `chain export NAME
QUALIFIED_ID URL`. The export emits a chain, not execution approval. Run through
the existing Hovel integration with normal planning/confirmation/launch policy.
It consumes the selected live endpoint and refuses stale ownership; it does not
provision a tunnel, create an SSH server or silently reconnect.
