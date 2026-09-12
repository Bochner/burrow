# Herdr-inspired Burrow implementation recommendation

2026-09-12. Research completed by three parallel agents and reconciled against
production source, the owner-provided Herdr screenshot, Context7 and pinned
upstream code. No production redesign was implemented in this research turn.

## Direction and sequence

Establish the frame now, before more feature screens depend on the current
single-workspace layout. Preserve Dracula Classic and the Burrow management
overview. The owner clarified that Hovel should run as its existing interactive
CLI inside a tab; do not build another Hovel-specific TUI.

The owner explicitly reaffirmed that entering a terminal must keep surrounding
navigation, tabs, New/Menu and metadata visible and clickable. Only the selected
center content changes; terminal attachment must not take over the outer screen.
Responsive narrow-screen adaptations are separate from that requirement. The
original visual reference is `/home/bochner/dev/herdr.png`.

Two implementation issues were added to **MVP 1: Setup, profiles and connections**,
as native children of [the daily-use MVP](https://github.com/Bochner/burrow/issues/43):

1. [Establish Herdr-inspired workspace navigation and live daemon status](https://github.com/Bochner/burrow/issues/67).
   Deliver workspace creation/selection, midpoint New/Menu, grouped shell-list
   placement, existing overview/right metadata, shared mouse/focus geometry and
   a truthful daemon indicator. Its setup and connection prerequisites are done.
2. [Run the Hovel CLI in an embedded workspace tab](https://github.com/Bochner/burrow/issues/68).
   Depends on the baseline. Deliver the real interactive CLI and reusable
   terminal host, with workspace isolation, reviewed throw and lifecycle checks.

[Open and close a real interactive SSH shell](https://github.com/Bochner/burrow/issues/48)
now has a native dependency on the Hovel CLI tab's shared terminal host.
[Switch independent full-screen shells](https://github.com/Bochner/burrow/issues/49)
already depends on that first SSH-shell slice. Authentication/profile backend
work does not need artificial blocking edges; integrate its screens into the
baseline. Later automation and tunnel-consumer tickets keep their own acceptance.

## Layout contract

| Region | Responsibility |
| --- | --- |
| Left upper | Known Hovel workspaces, selection and overflow. |
| Left midpoint | Fixed, clickable New and Menu; equivalent keyboard actions. |
| Left lower | Frontend shells grouped by owning workspace and connection. |
| Top of content | Burrow management and workspace-bound Hovel CLI tabs. |
| Center, Burrow tab | Existing saved/active/tunnel overview, command output and prompt. |
| Center, terminal selected | Embedded Hovel CLI or SSH terminal; switching back restores the overview. |
| Right | Relevant selected-resource and overall metadata, with narrow-screen overlay. |
| Persistent header | Labelled daemon bubble, freshness and access to measured verification details. |

The supplied screenshot guides hierarchy; exact cell widths need a real terminal
walkthrough. At 80x24, collapse secondary metadata before squeezing the center.
Keep New/Menu reachable as lists scroll. Full stacked tables and a usable terminal
cannot both occupy the same small center; a persistent compact summary strip is
an optional refinement if the owner wants status visible during terminal use.

## Decisions that avoid expensive rework

- **One terminal owner.** Bubble Tea owns outer input/rendering; PTYs feed x/vt
  screens embedded in the center. The old proof's raw `/dev/tty` attachment loop
  would remove clickable chrome and must not be copied into production.
- **One origin per operation.** Tag asynchronous results with workspace/request
  identity and bind terminal processes to their creation workspace. Switching
  must not redirect a pending confirmation, result or shell.
- **Reuse Hovel's CLI.** Launch the verified pinned executable with
  `shell --workspace /canonical/path` and explicit `HOVEL_DAEMON_ENDPOINT`.
  Hovel owns prompts, commands, plans and confirmations. The existing one-shot
  `run` wrapper is evidence for launch policy, not an interactive terminal host.
- **Truthful health.** There is one Hovel daemon per workspace with Burrow modules
  under it. A recent verified daemon check is green; checking/stale is yellow;
  observed failure/refusal is red. Pair colors with text. Show verification
  duration and last-success age; do not call executable/receipt verification
  network latency or infer SSH health from daemon reachability.
- **Small modules, real seams.** Keep layout/focus private to the frame, operations
  in existing launch/connection modules, and terminal lifecycle behind a small
  concrete host interface. No speculative pane tree, plugin system or second
  resource database. This follows the installed Matt Pocock research and
  deep-module conventions and Ponytail's reuse-first discipline.

## Dependency correction and evidence

Ultraviolet is already an indirect dependency used by Bubble Tea/Lip Gloss.
Lip Gloss has native identified layers/hit testing; Bubbles supplies existing
inputs and useful widgets. Tabs are application selection state. No new library
is justified for this layout. `creack/pty` is absent: the current Linux proof
uses `/dev/ptmx` and Unix ioctls. x/vt is pinned and proven in bounded checks,
but embedded production SSH terminal behavior is still forthcoming.

Context7 returned some older Charm snippets; agents checked proposed APIs against
the actual pinned sources. Read the detailed evidence rather than copying v1
examples into the v2 application:

- [Layout, wireframe, state and acceptance](herdr-layout-baseline.md).
- [Charm versions, native capabilities and source citations](herdr-charm-capabilities.md).
- [Hovel invocation, workspace ownership and daemon contracts](herdr-hovel-integration.md).

Research remains local on the shared `mvp1` checkout; the GitHub tickets contain
their own acceptance criteria and do not rely on unpublished blob links.
Exact visual acceptance and PTY compatibility remain implementation checks,
not claims made by this recommendation.

## Validation performed

`aspect burrow-check ci` passed: production/proof packages and research metadata
built, and all 13 existing portable checks passed from cache. This confirms the
existing gate remains green; it does not test the proposed UI or validate research
prose. New issue milestones, parent relationships and the baseline → CLI tab →
SSH-shell native dependency sequence were read back from GitHub. Both new issues
were added to the Burrow project. No production code or dependency pin changed.
