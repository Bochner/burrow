# Disposable connection ownership proof

For [Can connection ownership and complete teardown be proven through Hovel?](https://github.com/Bochner/burrow/issues/25).
Run `aspect burrow-prototype transport` or `aspect burrow-check`.
Pins and host requirements are inherited from [the transport proof](README.md).

## Observed ownership and logging boundary

The packaged module starts its own foreground OpenSSH master and returns an SDK
session of kind `connection`. This exposes a small inert status control channel;
it is not an interactive SSH shell. Hovel retains the module after Run returns.
Two ordinary SSH clients and local, reverse and SOCKS listeners reuse its master.

- Explicit Hovel CloseSession ends the master, both clients, all three listeners,
  and master socket. The check tests processes and ports as well as the pathname.
- Killing the module initially left an orphan master. The preserved version uses
  Linux `Pdeathsig=SIGTERM` and holds the creating Go OS thread until child exit.
  SIGKILL of the module now ends the master, clients and listeners.
- Killing the daemon yields SDK input EOF. An explicit ServeIO-return cleanup
  wrapper closes the master; plain SDK EOF does not call session close itself.
- External master exit refuses subsequent reuse with `ProxyCommand=/bin/false`.
  No automatic authentication or tunnel recreation occurs.
- Atomic creation of an owner-only fixture directory rejects a same-name
  collision. Empty stale reservations are deliberately not automatically adopted
  or removed by a new invocation. Cleanup removes only the owned empty directory.
- Dangerous execution without explicit allowance refuses before master creation.
  Hovel stores confirmation IDs and timestamped diagnostics after Run returns.
  Artifact paths are workspace-relative, content-addressed, and hash checked;
  evidence remains intact across close/loss.

## Reproduced upstream failure and bounded workaround

Send 257 additional raw SDK log notifications after two initial milestones.
Exactly 256 total module logs are persisted. The next notification ends Hovel's
RPC reader with `module log notification count exceeds maximum 256`; the broker
reports the session closed even though the SSH master and listeners remain live.
CloseSession returns that protocol error. Its request can still reach the module,
so cleanup may execute despite the missing successful acknowledgment.

The production recommendation is to fix the upstream lifetime buffer without
making it unbounded. The disposable workaround allows 200 diagnostic milestones
per module lifetime, then emits one warning and suppresses subsequent milestones.
The gate sends 600 attempts, checks the warning, and verifies explicit close still
cleans up all resources. This sacrifices later diagnostics and is not a durable
audit guarantee. The raw `log-ceiling` command deliberately bypasses the budget to
preserve the upstream failure reproduction; it is a fixture-only command.

## Coverage still required before full ownership acceptance

This checkpoint establishes the lifecycle/logging boundary, not all acceptance
criteria in the owner ticket. Burrow-local terminal handoff, independent live
geometry, foreground/background execution and redraw, raw input/restoration,
frontend exit, comprehensive workspace/name validation, stale/surviving socket
identity and explicit reconnection UX still require their focused checks.
The fixture uses a fixed `gateway` name and a harness-provided private root;
it does not select the final production runtime layout or authentication UI.
Its two `ssh -tt` clients do not prove a frontend PTY handoff.

These cases remain on the open owner ticket; neither this passing observation
gate nor the documented workarounds resolve that ticket or certify full parity.
The offered Ubuntu host was not required for this local daemon/logging failure.
