# Shared interactive session contract: source evidence

Investigation for [#87][issue], checked on 2026-09-20. This is evidence and
proposed behavior for owner review, not an approved production migration.
Runtime proof results belong beside the runnable prototype; source availability
alone does not count as successful CLI, SSH or terminal behavior.

The [completed bounded proof](../../core/prototype_sessions/README.md) verifies
that the released headless CLI works and demonstrates both native gaps and a
typed session-command candidate. That candidate has independent cursors,
explicit gap detection, controller fencing and resize without a second registry.
It still needs the documented production terminal-state/backpressure work and
owner acceptance; the proposals below remain review material.

## Provenance

| Component | Inspected revision |
| --- | --- |
| Latest official Hovel `main`, retrieved through GitHub's commits API | [`a4cbfdf7769a9551695088c11061e3cabc368e07`][head], committed 2026-09-15 06:05:12 UTC |
| Burrow public Go SDK pin | The same `a4cbfdf7769a9551695088c11061e3cabc368e07`, declared in `MODULE.bazel` |
| Burrow runtime and latest official release | [v0.4.2][release], source `c461ba282a8aecc7aa3a079a4613bf5e2640c388`; Linux amd64 wheel SHA-256 `7edccdabe04e30098e417d27ab48064c11a8612149124b7df0797251e88a0933` |

Context7 `/vibepwners/hovel` was queried for interactive session contracts and
identified the official module/daemon/user documentation. Claims below were
then traced to the owning source. A read-only clone was inspected at
`/tmp/burrow-87-hovel-source`. Comparing runtime source with latest main produced
no changes in the session SDK, SDK dispatcher and PTY implementation, session
broker/history, daemon session handlers, CLI attach and one-shot session
handlers, root CLI routing, or run-service event handling. Updating to latest
main alone therefore supplies none of the missing contracts described below.
[Upstream comparison][comparison], [local pins](../../MODULE.bazel).

## Current support and limits

| Requirement | Current public path | Limit established by source inspection |
| --- | --- | --- |
| Retain an SSH shell | Return a session from `Context.OpenSession`; the daemon adopts its module process. | Retention is in the running daemon/module, not durable recovery after either restarts. [SDK][session], [runner][runner] |
| Headless input | `hovel session send ID DATA`, optional `--no-newline` / `--end`; daemon `WriteSession`. | Handlers have no TTY requirement. A real invocation against the released binary remains necessary; command registration and documentation are insufficient proof. [CLI handlers][commands], [root dispatch][root] |
| Headless output | `session read`, `session tail`; daemon `ReadSession`, `TailSession`. | `read` drains one shared pending queue. `tail` is a retained suffix snapshot, not a cursor. [History implementation][history] |
| Independent viewers | `TailSession` with `Consume:false` leaves the shared queue alone. | No reader ID, byte offset, epoch, next cursor or truncation indication exists. Two `ReadSession` clients compete for bytes. CLI connect calls `TailSession(Consume:true)` before using the shared read queue. [Wire shapes][rpc], [attach][attach] |
| Initial and live geometry | No native resize method, size-bearing session option or controller attachment exists. | `Session` has Open/Write/Read/Close/Closed. The Linux SDK PTY allocator opens a PTY without setting rows/columns. No public `ResizeSession` route exists. [SDK][session], [allocator][allocator], [daemon reference][rpc-doc] |
| Detach/reattach | CLI Ctrl+] or input EOF ends attachment without closing the session; attach can replay history. | History is bounded bytes, not a terminal-state snapshot. Attaching also consumes other readers' pending bytes. [Attach implementation][attach], [history][history] |
| Input arbitration | Any client allowed to call `WriteSession` can send bytes to the selected session. | The request carries only session ID and data; no controller identity/lease or takeover operation is forwarded to `Session.Write`. [Wire/dispatch][rpc], [SDK][session] |
| Structured retained operations | `session call` / `RunSessionCommand` routes to `PayloadCommandProvider` on the retained session. | Works by interface assertion, with no installed-payload check; public documentation still describes installed payloads. This is the existing [#34][commands-issue] documentation gap. [Provider][provider], [SDK dispatch][session], [daemon reference][rpc-doc] |
| Close and loss | Explicit close calls the module and removes its broker record; pump errors mark the record closed. | EOF/error closure does not take the explicit last-session shutdown/removal path. Polling can distinguish active/closed, but pump errors collapse to closed without an exposed cause. [Broker][runner] |

The shared output queue defaults to 10 MiB and returns at most 32 KiB per read.
History is a separate 10 MiB suffix. A tail with `Consume:true` clears all
pending bytes, even when the requested tail is only one line. Upstream's
`TestBrokerSessionTailConsumeClearsPendingBytes` explicitly tests this behavior.
Repeated non-consuming tails are useful for looking at recent text; comparing
their lengths or overlapping contents cannot provide reliable positions after
truncation or repeated output. These are algorithmic limits of the current
response shape, not a claim that the tail endpoint is broken.
[Implementation][history], [upstream tests][history-tests].

`PTYSession` keeps an additional unbounded module-side output queue. A daemon
history limit does not bound that queue. The SDK frontend receives slave files
as `io.Reader` / `io.Writer`, but no native terminal geometry is supplied through
them. A Burrow-specific structured resize operation could be a candidate using
the already available session-command extension; it would need its own public
schema, validation and real end-to-end proof. It would not retroactively make
Hovel's native connect command forward resize. [PTY implementation][pty].

The existing connection reuse seam is
[`connection.ShellCommand`](../../core/connection/connection_linux.go): it
verifies the selected owner and socket and constructs a multiplexed OpenSSH
client with `ControlMaster=no`, `ProxyCommand=/usr/bin/false` and `BatchMode=yes`.
The proof should reuse this seam so master loss cannot become a fresh login.
Neither a second public module nor a second connection registry is needed.

## What live observation actually means

These three facts must remain separate in CLI output and in any eventual TUI
activity view:

1. **Submission:** `session send` receives a byte count after `WriteSession`
   returns. The daemon forwards `session/write`, and the SDK invokes
   `Session.Write`. This acknowledges input handling; it does not report that a
   newline completed a command, that a program ran, or that it succeeded. There
   is no command identifier or generic submission event on this route.
   [CLI handler][commands], [daemon forwarding][rpc], [SDK dispatch][session].
2. **Execution result:** `session call` returns a provider-authored
   `PayloadCommandResult`. Its meaning depends on that capability: a resize
   acknowledgement, a launched-job reference and a completed process result are
   different results. The retained `RunSessionCommand` route directly invokes
   the broker; it does not pass through the separate
   `RunService.RunPayloadCommand` path that emits
   `hovel.payload.command.completed`. No generic confirmed-throw or completed
   command event should be inferred for the retained route.
   [Result type][provider], [session forwarding][rpc], [payload event path][run-service].
3. **Terminal I/O:** arbitrary bytes can contain echoed input, prompts, output,
   cursor movements, screen changes and no command boundary at all. Hovel
   buffers these bytes separately from its log/event stream. Seeing `false`
   echoed or seeing a shell prompt is not a structured exit status. A process
   that accepts input and then loses its transport has an uncertain execution
   outcome. [PTY][pty], [history][history].

Session creation is observable as `hovel.session.created`: after a successful
run returns its session references, the runner explicitly appends one event per
session with operation, chain, run, module, target and session references. The
SDK also emits `module/session` notifications for `session.created` and
`session.closed`. However, the ordinary inspected runner installs `onLog` and
does not install an `onEvent` callback; it therefore does not turn arbitrary
SDK close notifications into a guaranteed public live lifecycle event feed.
Use `ListSessions` and the read/tail `Closed` status for the currently available
state, distinguishing explicit removal from an error-marked closed record.
[SDK emission][server], [runner callbacks and creation events][runner].

SDK logs can carry bounded non-secret milestones through Hovel's normal event
and `PollLogs` path, but are not an unlimited command transcript: the RPC client
retains at most 256 module log notifications per process lifetime. Do not log
every keystroke or use this route to duplicate terminal output. This ceiling
remains unchanged in latest source. [Notification handling][runner],
[earlier diagnostics investigation](hovel-events.md).

## Concrete behavior to review with the owner

The following is a proposed owner-facing policy. It is not implemented or
accepted by this research. It preserves the previously selected one-controller
direction in [#24][local-shell-decision] and [#29][geometry-issue].

| Situation | Proposed visible behavior | Example |
| --- | --- | --- |
| Attach an observer | Show the current controller, terminal size and synchronization status. Do not change geometry or accept observer input. | A human watches an agent's 100×30 shell; opening a 140×40 view does not resize the agent's `top`. |
| Take control | Explicitly transfer control; serialize takeover with writes and resize. Previous controller becomes an observer before new input is accepted. | The human selects Take control, the shell adopts that view's 140×40 dimensions, and the agent gets a visible refusal on its next write. |
| Headless agent control | Require an explicit controller claim and validated geometry; distinguish no controller from unavailable dimensions. | An agent that wants a shell chooses 100×30. A request with zero rows fails instead of silently using a default. |
| Competing input | Refuse input from anyone except the current controller. Report who controls the session; do not merge or queue hidden keystrokes. | Human typing cannot splice `rm` into the middle of the agent's `printf` command. |
| Background/switch | Retain the remote shell and its size until another controlling attachment deliberately changes it; observers keep their independent positions. | Switching to shell B leaves A's `top` running; returning to A restores its current screen. |
| Detach/keep running | Release control, retain the shell and last valid geometry, and continue bounded output capture. Reattach requires a new explicit claim. | The frontend exits after Keep running; the shell PID and working directory remain the same on return. |
| Close shell | Explicitly close this channel; report completion or uncertainty. Preserve its connection, other shells and tunnels. | Close A ends A's `sleep`; B and the connection remain usable. |
| Close connection | Preserve the existing reviewed connection-wide teardown contract. End dependent shells as well as transfers/tunnels. | Quit review names affected shells before Close connections. |
| Controller disappears | The bounded candidate retains the controller claim until explicit takeover; the label does not assert client liveness. Automatic revocation needs a separately proven attachment/lease rule. | After a human explicitly takes over, any delayed write using the vanished agent's old token is refused. |
| Output gap | Expose a missed-output/screen-unsynchronized state and use a proven replay/snapshot or explicit redraw path. | A slow observer falling behind the buffer cannot display its stale screen as current. |
| Master/module/daemon loss | Show closed/lost/unavailable truthfully. Preserve uncertainty about remote commands and cleanup; no automatic reconnect or login. | Relaunching the daemon never claims to have restored the previous shell or its working directory. |

Normal frontend quit should review retained shells along with connections and
offer Keep running, Close and Cancel. Acceptance of this behavior would change
the explicit [#24 resolution][local-shell-decision]: today frontend-local shells
end on Burrow exit while the connection/tunnels may remain. It would not change
the existing rule that restart/loss requires explicit reconnect. There is no
accepted promise that a shell survives daemon or module loss.

Native Hovel CLI attach currently restores termios on cleanup, but does not
explicitly reset the alternate screen, cursor visibility or other terminal
presentation modes. Test both clean detach and failure after a full-screen
application changes those modes; restored termios alone is insufficient.
[Attach cleanup][attach], [existing follow-up #29][geometry-issue].

## Smallest candidate and production prerequisites

First record the current native route's positive and negative results using the
existing owner and a single `burrow` module: actual headless send/read/call,
independent-read competition, dimensions, detach/reattach, two shells, Ctrl+C,
full-screen restoration and loss. A passing assertion that observes a missing
capability is negative capability evidence, not a successful shared-session
acceptance check.

A typed session-command candidate may carry validated geometry, explicit
controller operations and a bounded observer contract without disguising control
as shell bytes. It must be proven separately and retain one authoritative owner;
ordinary native writes/reads must not bypass the candidate's arbitration or
steal observer data. Do not assume that adding `resize` and `read` command names
solves attachment fencing, independent cursors, terminal reconstruction or
backpressure. The current [#34][commands-issue] usage is a proven precedent for
retained structured requests, not proof of those additional semantics.

After owner review, scope the production work around the chosen public
contract: (1) retained shell lifecycle and same-owner teardown; (2) headless
control and independent observation with input/geometry enforcement;
(3) Burrow TUI attachment, screen synchronization, quit review and restoration;
(4) truthful activity/result presentation and CLI-first agent workflows. Use
native issue dependencies to make the accepted prerequisites block [#64][agent-issue].
If native Hovel changes are necessary, keep their handoff in Burrow for the
owner, extending [#29][geometry-issue] and [#34][commands-issue] where appropriate.
These are candidate boundaries, not published or build-ready tickets.

## Validation boundary

This file records GitHub issue/release/head inspection, Context7 discovery, and
source tracing at exact revisions. No upstream build, runtime test or production
change was performed by this source investigation. The bounded Aspect proof is
responsible for runtime evidence; the repository metadata gate validates the
declared graph, not these prose claims or interactive correctness.

[issue]: https://github.com/Bochner/burrow/issues/87
[agent-issue]: https://github.com/Bochner/burrow/issues/64
[geometry-issue]: https://github.com/Bochner/burrow/issues/29
[commands-issue]: https://github.com/Bochner/burrow/issues/34
[local-shell-decision]: https://github.com/Bochner/burrow/issues/24
[head]: https://github.com/vibepwners/hovel/commit/a4cbfdf7769a9551695088c11061e3cabc368e07
[release]: https://github.com/vibepwners/hovel/releases/tag/v0.4.2
[comparison]: https://github.com/vibepwners/hovel/compare/c461ba282a8aecc7aa3a079a4613bf5e2640c388...a4cbfdf7769a9551695088c11061e3cabc368e07
[session]: https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/sdk/go/hovel/session.go
[server]: https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/sdk/go/hovel/server.go
[provider]: https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/sdk/go/hovel/payload.go
[pty]: https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/sdk/go/hovel/pty_session.go
[allocator]: https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/sdk/go/hovel/pty_linux.go
[runner]: https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/core/internal/moduleruntime/pythonrpc/runner.go
[history]: https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/core/internal/moduleruntime/pythonrpc/session_history.go
[history-tests]: https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/core/internal/moduleruntime/pythonrpc/session_history_test.go
[rpc]: https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/core/internal/adapters/daemonrpc/daemonrpc.go
[rpc-doc]: https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/docs/site/src/content/spec/daemon-rpc.html
[attach]: https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/core/internal/adapters/cli/session_connect.go
[commands]: https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/core/internal/app/commands/session_commands.go
[root]: https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/core/internal/adapters/rootcli/rootcli.go
[run-service]: https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/core/internal/app/services/run.go
