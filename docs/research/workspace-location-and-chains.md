# Workspace location and Hovel chains

Research date: 2026-09-13. Evidence, not an approved location decision or a
production chaining claim. Inspected Hovel revision
`c461ba282a8aecc7aa3a079a4613bf5e2640c388`, Burrow's pinned SDK source. A read-only
GitHub API check of `vibepwners/hovel` HEAD returned the **same revision** (commit
date 2026-09-04); no newer upstream behavior was substituted for the pin.
Burrow source baseline: local `mvp2` commit `2c977d2`. No live user workspaces,
daemons, SSH transports, permissions or configuration were changed for this
investigation. Existing proof source was inspected; its lab was not rerun.

## Answer

Burrow can put its ordinary Hovel workspaces under a Burrow-named directory
without obstructing Hovel chaining. The important choice is **which exact
workspace/daemon the chain uses**, not whether its parent folder is called
`burrow` or `hovel`. For the accepted native owner/session consumption path,
run the chain against the same workspace that owns the Burrow connection.
Putting two different workspaces under one parent does not share their live
resources. This conclusion follows from the workspace-bound daemon construction
and session broker, not from a filesystem naming convention. [Daemon setup](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/infra/daemonruntime/runtime.go#L337-L507),
[session lookup and routing](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/moduleruntime/pythonrpc/runner.go#L3877-L3965).

For example, `~/.local/share/burrow/workspaces/lab` can be the workspace used by
both the Burrow UI and Hovel chains. A separately launched Hovel using its
implicit `./.hovel` is another workspace, not automatically a frontend of
`lab`. Hovel's default is `.hovel`; its resolver accepts other paths and does
not prescribe a central workspaces directory. [Workspace resolver](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/domain/workspace/workspace.go#L11-L91).

## What is actually shared

| Scope | What it means | Effect on chaining |
| --- | --- | --- |
| Parent directory | A convenience such as `…/burrow/workspaces` containing several workspaces. | No automatic resource sharing or routing. Its spelling is irrelevant to Hovel's resolver. |
| Exact workspace and its running daemon | Persistent operator state, module runtime/session broker, run service and PKI are constructed for that workspace. | This is the native scope in which the chain can select the existing Burrow owner/session. |
| Hovel operation/chain | A subdivision of the workspace's operator state. | Multiple operations and chains can coexist without inventing a workspace per chain; operation context still matters for targets, configuration and history. |
| Named Burrow connection | An owned SSH master under the selected workspace. | Consumers reuse that connection; selecting a parent folder does not create another SSH login. |

Sources: [workspace-bound daemon state](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/infra/daemonruntime/runtime.go#L405-L505),
[shared-store and operation-segmentation tests](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/app/operatorsession/session_test.go#L367-L455),
[Burrow connection socket and explicit Hovel routing](../../core/launch/operations_linux.go),
[fresh retained-owner consumer proof](../../core/prototype_manager/consumer.go).

The embedded Hovel CLI already selects the Burrow workspace explicitly:
`hovelCommand` supplies `--workspace`, removes inherited `HOVEL_*` variables,
sets the endpoint to that workspace's `hoveld.sock`, and uses it as the working
directory. A name-only New form should keep using that existing launcher,
not create a Burrow-only state format. [Launch implementation](../../core/launch/operations_linux.go#L198-L232).

The production module also checks that its parent process is the verified
workspace daemon. Passing another workspace's path into a chain running under
a different daemon is therefore not a supported shortcut to cross-workspace
reuse. [Manager adapter check](../../core/connection/manager.go#L252-L263).

## Proven integration versus future work

- The accepted #72 bounded proof launches a fresh confirmed consumer, calls the
  existing manager through public Hovel session commands, and keeps the master
  PID unchanged. The actual fixture binds the throw to the selected workspace
  and endpoint. It also tests wrong-workspace refusal without traffic. These
  are **same-workspace** checks, not a cross-workspace federation proof.
  [Consumer adapter and owner checks](../../core/prototype_manager/consumer.go#L61-L116),
  [fixture](../../core/prototype_manager/consumer_check.py#L95-L140),
  [accepted proof and limits](retained-consumer-proof.md).
- Generic Mesh stream handoff is separately deferred: the negative experiment
  rejects adopting an already tracked session from another process. Sharing a
  workspace does not itself solve this protocol boundary. Production tunnel
  consumption and later callback/session handoff retain their existing scope;
  changing the default directory does not implement them.
  [Adoption implementation](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/moduleruntime/pythonrpc/runner.go#L3917-L3952),
  [proof](retained-consumer-proof.md),
  [accepted Wayfinder scope](https://github.com/Bochner/burrow/issues/1).
- A compatible external tool can explicitly use an existing local forward or
  SOCKS endpoint. That network possibility is not automatic shared Hovel
  discovery, owner verification, or shared audit history across workspaces.
  A future cross-workspace owner-routing feature needs explicit selection,
  authorization, lifecycle and evidence semantics; it is not necessary merely
  to place workspaces under a Burrow directory.
  [Endpoint consumption distinction](hovel-ssh-mesh-boundary.md#consumption-is-explicit),
  [workspace-local broker lookup](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/moduleruntime/pythonrpc/runner.go#L3955-L3965).

## Decision matrix

The first three rows choose storage layout. The last two choose different
runtime integration policies; they are not equivalent filesystem preferences.

| Choice | Hovel-chain compatibility | Advantages | Costs / cautions | Assessment |
| --- | --- | --- | --- | --- |
| Burrow default parent, e.g. `$XDG_DATA_HOME/burrow/workspaces/<name>` (fallback `~/.local/share/burrow/workspaces/<name>`) | Normal Hovel workspace; native reuse when chains select that exact workspace. | Stable name-only creation; no dependence on current directory; clear discoverable owner. | Explicit selection required from an independently started Hovel CLI; long home/XDG paths can limit socket names. | Recommended default layout, subject to owner approval and path validation. |
| Neutral shared parent, e.g. `~/hovel-workspaces/<name>` | Identical to the first row. | Product-neutral label; generally shorter path. | A new convention, **not** Hovel's existing central default; sibling workspaces still do not share owners. | Equally valid technically; naming preference only. |
| Current project/workspace parent | Identical when the exact intended workspace is selected. | Engagement files can stay together; convenient explicit override. | New location varies with navigation; less predictable name-only New behavior. | Useful override, weaker global default. |
| Force all Burrow and Hovel activity into one existing workspace | Common daemon/state if it is a supported owned instance. | Shared resources without cross-workspace routing. | Collapses desired workspace separation; arbitrary existing-daemon adoption is outside accepted initial support. | Not required; do not bypass ownership to achieve this. |
| Separate workspace/daemon per frontend, with automatic cross-workspace resource reuse | Not established by the accepted native consumer proof. | Could preserve separate stores while sharing selected transport later. | Needs explicit integration/proof; a common parent folder cannot supply it. | Defer; unnecessary for the requested New form. |

The recommended root is a **Burrow UI convention**, not a newly discovered
Hovel requirement. Socket length needs attention whichever root is chosen:
Burrow currently requires `<workspace>/burrow/<connection>/master` to fit in
90 bytes. The optional location field should remain available for a shorter
path, and validation must give an actionable error rather than relocate live
resources. [ConnectionPath](../../core/launch/operations_linux.go#L20-L32).

## Proposed decision to take back to the owner

Use a stable Burrow default parent plus an optional parent-location override;
create an ordinary Hovel workspace and keep the embedded Hovel CLI bound to it.
For a chain using Burrow's established connection, select that workspace rather
than independently creating another one. Preserve separate workspaces for
separate engagements when desired. Do not scan/adopt unrelated daemons, migrate
live sockets, add a resource-sharing registry or open a second SSH transport as
part of this UX correction. The accepted support scope already limits initial
attachment to verified Burrow-started pinned instances. [Setup decision](https://github.com/Bochner/burrow/issues/18),
[workspace runtime decision](https://github.com/Bochner/burrow/issues/32),
[current launch routing](../../core/launch/operations_linux.go).

This recommendation does not approve the default path, broaden chain scope, or
replace the existing single-master transport decision. The current question can
be settled without an upstream change: parent-folder branding is independent
of Hovel compatibility; exact workspace selection is the integration contract.
