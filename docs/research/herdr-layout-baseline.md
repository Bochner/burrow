# Herdr-inspired Burrow layout baseline

Investigated 2026-09-12. This is source-backed research and a recommended
implementation sequence, not a claim that the revised interface is implemented
or visually accepted. The owner requested the sidebar, midpoint New/Menu,
workspace/shell list, Hovel tab, retained overview/right metadata, and daemon
health display. The exact sizing and interaction policy below are recommendations.

## Recommendation

Establish the application frame before integrating more shell, transfer, or Hovel
screens. Keep the accepted Dracula theme, resource-table order, prompt completion,
and dimmed overlays. Add the Herdr navigation around those existing roles. The
important early decision is who owns input, shell lifetime, and workspace context;
the panel borders are inexpensive to change afterward.

The [Wayfinder map](https://github.com/Bochner/burrow/issues/1) and
[accepted baseline](https://github.com/Bochner/burrow/issues/42) require the
command-first overview and public Hovel contracts. The
[shell decision](https://github.com/Bochner/burrow/issues/24) deliberately keeps
interactive shells local to the frontend, while daemon-owned connections and
tunnels survive quit. This layout does not change those ownership decisions.

## What the reference actually shows

The owner-supplied `/home/bochner/dev/herdr.png` was inspected directly. Its left
rail has spaces above, New/Menu at the divider approximately halfway down, and
grouped agents below. Numbered tabs begin at the content area's left edge. The
rail is about 12% of this unusually wide screenshot. That ratio should not become
a width rule: it would leave only nine cells at 80 columns. No click behavior or
runtime guarantee can be proven from the screenshot.

Herdr's [previously inspected source](herdr-terminal-state.md) supports the
terminal-emulation principle, but its server-owned shell persistence is different
from Burrow's accepted frontend-local lifetime. Borrow the visible information
hierarchy, not that ownership model or its Rust/Ghostty dependency stack.

```text
┌──────────────────────┬─────────────────────────────────┬──────────────────────┐
│ WORKSPACES           │ [Burrow] [Hovel]                 │ ● Hovel: verified    │
│ › homelab            ├─────────────────────────────────┤ last check: 2s ago   │
│   engagement         │ SAVED CONNECTION CONFIGURATIONS │                      │
│                      │ ...existing ordered table...    │ SELECTED CONNECTION  │
│                      │                                 │ host / user / state  │
│                      │ ACTIVE SSH CONNECTIONS          │                      │
│                      │ ...existing ordered table...    │ RESOURCES            │
│ [New]        [Menu]  │                                 │ counts / activity    │
├──────────────────────┤ TUNNELS · grouped by connection │                      │
│ WORKSPACES / SHELLS   │ ...existing ordered table...    │ DAEMON DETAILS       │
│ homelab              │                                 │ PID / endpoint       │
│   web · shell 1      │ COMMAND OUTPUT                  │ last successful check│
│   db  · shell 2      │                                 │                      │
│ engagement           ├─────────────────────────────────┤                      │
│   No shells          │ contextual help / prompt        │                      │
└──────────────────────┴─────────────────────────────────┴──────────────────────┘
```

The Burrow tab remains the management overview. The owner subsequently clarified
that the Hovel tab should run Hovel's existing interactive CLI inside Burrow,
without building Hovel-specific TUI screens. It uses the same frame and binds
to the workspace selected when that terminal is opened; switching workspaces
must not silently retarget a running CLI or its pending confirmation. Shell
selection opens an embedded shell view in the center and returning to Burrow
restores its tables and draft. If the owner means tables must stay visible even
during an interactive shell or Hovel operation, use a compact resource-summary
strip above that center view; do not silently imply the full table stack and a
usable shell fit simultaneously at 80×24.

## Current code and the costly trap

| Area | Observed implementation | Implication |
| --- | --- | --- |
| Production management | [`core/cmd/burrow/tui.go`](../../core/cmd/burrow/tui.go) renders saved/active/tunnel sections, command output, prompt, and a right rail at 110 columns. Only active connections are live; saved/tunnel rows explicitly say unimplemented. | Preserve roles and accepted table styling; do not report fixture capabilities as production features. |
| Production input | The same `Update` has prompt, completion, help and quit routing; there are no mouse handlers, workspace picker, tabs, or interactive shells. | Implement explicit focus and hit targets once in the application frame. |
| Accepted visual reference | [`core/prototype_terminal/design.go`](../../core/prototype_terminal/design.go) has richer content-sized tables, selected connection/counts/activity in the right rail, a command menu and preserved prompt state. | This remains the accepted visual reference; production's reduced rail is not the full acceptance target. |
| Local shell proof | [`shell_linux.go`](../../core/prototype_terminal/shell_linux.go) owns PTY reads and independent emulators. [`app.go`](../../core/prototype_terminal/app.go) enters `tea.Exec` for attachment. | PTY/emulator behavior is useful evidence, but it is not production SSH. |
| Full-screen attachment | [`screen_linux.go`](../../core/prototype_terminal/screen_linux.go) opens `/dev/tty`, takes raw input and paints a full-screen frame while Bubble Tea is released. | Copying this attachment loop would hide the tabs/sidebar and prevent their mouse use. It must not become the production embedded-shell path. |
| Existing verification seam | [`launch.Status`](../../core/launch/daemon_linux.go) checks receipt, process, socket and public daemon identity; [`launch.Call` / `HovelCLI`](../../core/launch/operations_linux.go) preserve verified workspace execution. | Reuse these operations and their refusal behavior. Avoid a parallel health or throw implementation. |

The production `connectionList` message contains no workspace/request identity.
That is currently a single-workspace application. Once switching exists, results
must identify their originating workspace and request, or a slow result from A
can replace B's tables. The `connectionResult` and `checked` messages need the
same treatment. Keep drafts, selected resource identities, and scroll offsets per
workspace; never use list position as resource identity.

## Layout and input policy

Use one computed set of cell rectangles for drawing, pointer hit testing, and
child sizing. The left action row is anchored to the midpoint of the available
sidebar body, independent of how many workspaces exist. Both lists scroll within
their own bounds; New/Menu never scroll away. Give each label its whole button
rectangle, including padding, and keep targets disjoint at every width.

Start a terminal walkthrough with 24 cells for the left rail, 30 for the existing
right rail, and two-cell gaps. Show all three when at least 60 center cells remain
(118 columns with those values). At 80 columns, collapse right metadata into an
on-demand overlay and use a 22-cell left rail; preserve the production compact
connection presentation in the remaining center. At narrower sizes, expose the
left rail as a drawer with a persistent workspace label and New/Menu entry point.
These are initial content constraints, not immutable magic breakpoints. At short
heights, preserve the prompt, reachable actions, and selected rows; show explicit
overflow rather than squeezing every table into unreadable rows. Test 80×24,
120×40, 160×48, and transient tiny resize events.

Pointer and keyboard actions should reach the same state transition. A single
primary-button click selects a workspace, opens New/Menu, activates a tab, or
focuses a shell; secondary clicks do not perform mutations. Mouse wheel scrolls
the region under the pointer. Text entry owns printable characters. Keep Tab
completion while the prompt is focused; use a documented focus-cycle key such
as F6/Shift+F6 so existing completion does not regress. Within navigational
controls, arrows move and Enter activates. Closing a modal restores its prior
valid focus target and draft. Preserve current F1 help and quit confirmation.

In shell focus, terminal keystrokes belong to the selected PTY; retain a documented
management escape such as the existing Ctrl-]. Sidebar and tabs always belong to
the frame. Only pointer events inside the focused terminal rectangle may reach
that PTY, translated to pane-local cell coordinates and honoring its negotiated
mouse modes. The shell cursor must also be offset and clipped to its rectangle.
The frontend must remain the single owner of the outer terminal input and
rendering while hidden PTYs continue draining output. This is the essential
acceptance condition for in-place tabs and shell switching.

New opens a workspace creation form showing the target path before creation;
activating New is not itself a directory/daemon mutation. Reuse the existing
verified launch contract. Switching does not close another workspace's resources
or silently reconnect stale connections. A shell row identifies workspace,
connection and shell, so similarly named connections in different workspaces are
not ambiguous. Shell activation can select its owning workspace atomically.

## Daemon indicator

Place `● Hovel: verified` in the persistent header, with fuller details in the
right rail or its narrow-screen overlay. Pair color with labels and retain an
ASCII/no-color presentation. Do not invent a second Burrow daemon: the current
application has a Hovel daemon and a Burrow connection-owner module.

Recommended indicator semantics: green means a recent successful verified daemon
check; yellow means checking, stale, or unverified; red means a failed/unavailable
or identity-refused endpoint, with the reason retained. Expose last successful
check time and measured check duration. Call that duration a verification round
trip, not SSH latency or a pure network RTT. If Burrow module reachability is
also displayed, label it separately from Hovel daemon identity and from SSH
connection state; one cannot substitute for the others.

`launch.Info` currently has workspace, PID, start time, health and access fields,
not latency/freshness. Production refreshes connection rows every two seconds
after each request completes, but the right rail's `info.Health` only changes
through manual `status`. Therefore a colored copy of that string could stay green
after failure. Record observation time/outcome in the presentation snapshot;
reuse existing checks, avoid concurrent overlapping polls, and label old data
stale while a new request is pending. Determine freshness thresholds from the
chosen timeout/cadence and cover them with a fake clock in the behavior check.
Sources: [`Info` and `Status`](../../core/launch/daemon_linux.go),
[`connectionTimer`, `connectionList`, `checked`, and `View`](../../core/cmd/burrow/tui.go).

## Minimum module seams

Apply the repository's deep-module conventions without introducing a general
window manager or extension framework. Three modules earn their place:

1. **Application frame**: its interface accepts terminal events and application
   snapshots, yielding a view and requested actions. Its implementation owns
   rectangles, focus, tabs, modal routing and stable per-workspace presentation
   state. Keep small layout helpers private rather than exporting every panel.
2. **Existing operation modules**: retain `core/launch` and `core/connection` as
   the seam for validated work. The frame neither duplicates daemon state nor
   reads daemon internals. Add only the real workspace or Hovel operation needed
   for an accepted screen; do not invent a generic backend interface now.
3. **Shell host**, when integrating real shells: its small interface owns
   open/input/resize/snapshot/close behavior and asynchronous results. Hide PTY,
   emulator, mode negotiation and cleanup behind it. Screen visibility must not
   control process lifetime. A frame resize passes the center's dimensions, not
   the full terminal dimensions.

These are module responsibilities, not a demand for three new packages or a Go
interface for each. Keep callers and behavior checks crossing the same seam.
Promote proven behavior from the prototype deliberately; do not import fixture
state or maintain two production renderers.

## Behavior acceptance before feature expansion

Extend the existing Aspect-declared production checks with one focused frame
scenario covering pointer and keyboard parity, fixed midpoint controls, resize
hit rectangles, retained drafts/selection, modal isolation, out-of-order workspace
results, and health freshness. Reuse the current VT screen replay facility in
[`core/cmd/burrow/BUILD.bazel`](../../core/cmd/burrow/BUILD.bazel) and
[`screen_check.go`](../../core/cmd/burrow/screen_check.go); the existing real-PTY
setup check already exercises terminal startup and dialogs.

The shell slice then proves two PTYs continue while hidden, alternate-screen
content/cursor restore, frame clicks still work during shell focus, pane-sized
resize and terminal replies function, background operations keep advancing, and
normal exit ends local shells while daemon connections remain. The Hovel slice
separately proves workspace targeting and public confirmation/audit behavior.
Passing a layout snapshot does not prove either runtime contract. Finish with an
owner walkthrough of the real terminal to accept spacing and navigation before
those screens multiply.

No implementation gate was run for this note: it adds research only. Library
selection and current Context7 verification belong to the companion Charm
capability investigation; the claims above about existing calls come from the
checked-in source. No new dependency is justified merely to draw the reference
layout.
