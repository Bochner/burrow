# Shared interactive SSH sessions

Accepted by the owner on 2026-09-20 in [#87](https://github.com/Bochner/burrow/issues/87).
Use Hovel-retained SSH shells through its public session-command extension so
an external agent and the Burrow TUI can operate and observe the same shell.
The bounded proof establishes a viable direction; production migration remains
separate work. This supersedes [#24](https://github.com/Bochner/burrow/issues/24)'s
frontend-local shell lifetime for MVP 5, without promising recovery after daemon
or owning-module loss.

## Accepted operator behavior

- One controller may send input and set initial/live terminal dimensions.
  Independent observers see the same shell without consuming each other's output,
  sending input or changing its dimensions.
- Takeover is explicit and invalidates the previous controller's input and resize
  authority. Either a human or an agent can explicitly take control again.
- Detach releases control and retains the shell and its last valid dimensions.
  Normal frontend quit reviews dependent shells across opened workspaces and
  offers Keep running, Close connections or Cancel; the daemon remains running.
- Closing a shell ends only that channel and preserves its connection and sibling
  resources. Closing a connection ends its dependent shells, transfers and tunnels
  under the existing exact-resource review and ownership checks.
- A controller that disappears leaves its claim until explicit takeover. Displayed
  controller identity is not a liveness assertion; automatic expiry/reassignment
  is outside this initial contract.
- Missed output must visibly mark a view as out of sync. Production must provide
  a proven replay/snapshot or explicit redraw/resynchronization path before
  presenting the view as current again.
- Connection, owning-module or daemon loss is reported truthfully, including
  uncertainty about remote commands and cleanup. Reconnect remains explicit;
  a new connection does not recreate the lost shell or its state.

## Integration and evidence

Reuse the exact existing connection owner and OpenSSH master, the single base
`burrow` module and Hovel's public session registry. Preserve the
[connection approval contract](0001-manual-connection-approval.md); controller
claims are input arbitration, not a new user security boundary. Control tokens
stay out of normal arguments, logs and evidence. Acknowledged terminal input is
not a structured command completion result.

The [preserved proof](../../core/prototype_sessions/README.md) demonstrates typed
control, independent cursors, resize and explicit gaps. Native shared reads,
unrestricted concurrent input and raw attach do not satisfy this contract.
Production must bound PTY buffering and input, handle close/control races, restore
terminals, and integrate retained shells with normal TUI tabs, quit review and
the activity follower. No second registry, audit store, private Hovel import or
automatic login fallback is authorized by this decision.
