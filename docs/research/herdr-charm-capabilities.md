# Herdr-inspired Burrow: native Charm capabilities

Date: 2026-09-12. Research and recommendations, not an implementation acceptance.
Inspected current `mvp1` source, pinned module sources in the local Go cache,
the [Wayfinder map](https://github.com/Bochner/burrow/issues/1), and Context7.

## Recommendation

Keep Bubble Tea v2 as the single frontend runtime. Build the requested sidebar,
midpoint New/Menu controls, shell navigation, top tabs, and daemon indicator with
the existing Lip Gloss compositor and Bubbles inputs. No new dependency is needed
for that management layout. Introduce one explicit focus/selection model and
one layout calculation shared by drawing and click routing; do not introduce a
general pane tree, docking framework, or second terminal renderer for three fixed
columns. This is a design recommendation based on the capabilities below.

## Actual dependencies and use

The authoritative pin set is [core/prototype_sdk/go.mod](../../core/prototype_sdk/go.mod),
also used by production Bazel targets; its directory name does not mean these
libraries are absent from production.

| Capability | Inspected pin | Actual Burrow status |
| --- | --- | --- |
| Bubble Tea | `charm.land/bubbletea/v2 v2.0.9` | Direct production TUI dependency. |
| Lip Gloss | `charm.land/lipgloss/v2 v2.0.6` | Direct production styling, static resource tables, completion and modal compositing. |
| Bubbles | `charm.land/bubbles/v2 v2.2.1` | Production uses key bindings and textinput; prototype additionally uses help, progress, spinner and viewport. |
| Ultraviolet | `v0.0.0-20260811164956-006e29f97886` | Already indirect; Bubble Tea renderer/events and Lip Gloss layers use it. No application import currently. |
| x/vt | `v0.0.0-20260906004030-3986e9119cf9` | Direct pin; terminal prototype emulates local fixture shells; production screen acceptance also uses it. Not an integrated production SSH shell pane. |
| PTY | Linux `/dev/ptmx`, `os/exec`, `golang.org/x/sys/unix` | Existing prototype allocates and resizes PTYs directly. `creack/pty` is not present in this pin set or Burrow imports. |

Sources: [production TUI](../../core/cmd/burrow/tui.go),
[screen check](../../core/cmd/burrow/screen_check.go),
[prototype app](../../core/prototype_terminal/app.go),
[Linux PTY implementation](../../core/prototype_terminal/shell_linux.go),
[VT attachment](../../core/prototype_terminal/screen_linux.go),
[Bubble Tea renderer at v2.0.9](https://github.com/charmbracelet/bubbletea/blob/v2.0.9/cursed_renderer.go),
[Lip Gloss layers at v2.0.6](https://github.com/charmbracelet/lipgloss/blob/v2.0.6/layer.go).

Claude's table therefore overstates production shell integration and incorrectly
identifies the PTY dependency. Its Ultraviolet observation is directionally right:
Burrow already benefits from it through Charm. These pins were inspected; this
research does not claim every pin is the latest upstream release.

## Native click targeting, focus, and widgets

Bubble Tea v2 exposes `tea.View.MouseMode`; `tea.MouseModeCellMotion` enables click,
release, wheel and drag reporting. Use `tea.MouseClickMsg` for activations and route
wheel input to the region under the pointer. `MouseModeAllMotion` is only needed
for hover without a pressed button. The production view currently only sets
`AltScreen`; it does not enable mouse input or handle clicks.
Sources: [v2 view/mouse modes](https://github.com/charmbracelet/bubbletea/blob/v2.0.9/tea.go),
[v2 mouse messages](https://github.com/charmbracelet/bubbletea/blob/v2.0.9/mouse.go),
[current view/update](../../core/cmd/burrow/tui.go).

Lip Gloss v2 already provides `Layer.ID`, positioned layers, `Compositor.Hit(x,y)`
and `LayerHit.Bounds()`. Hit testing returns the topmost **identified** layer and
ignores layers with empty IDs. Therefore assign IDs to actual controls and modal
backdrops; a visually covering unnamed modal does not automatically prevent
click-through. Rebuild or refresh the compositor when geometry changes. Use the
same computed rectangles for the displayed controls and routing; avoid a second
set of magic offsets in Update. Source:
[pinned compositor implementation](https://github.com/charmbracelet/lipgloss/blob/v2.0.6/layer.go).

Recommended routing precedence: modal first, application chrome next, then the
focused content region. A menu owns keyboard input until dismissed, and restores
the previous focus afterward. Clicking a workspace or tab selects it without
implicitly creating or closing a shell. Give every mouse action a keyboard route;
preserve Tab completion while the prompt is focused. Focus and workspace selection
remain application state: the compositor supplies geometry, not these policies.

Bubbles already supplies list selection/filtering, text inputs, viewports, help,
tables, progress and spinners. The current center uses **Lip Gloss static tables**,
not Bubbles table models; keep these while preserving the requested overview.
Use Bubbles list for an overflowing workspace/shell list only when its behavior
fits. Pinned Bubbles table/list primarily handle keyboard navigation; do not assume
they automatically map arbitrary screen clicks to selected rows. A small
application row-to-selection mapping is still needed. Sources:
[Bubbles v2.2.1 list](https://github.com/charmbracelet/bubbles/blob/v2.2.1/list/list.go),
[table](https://github.com/charmbracelet/bubbles/blob/v2.2.1/table/table.go),
[current resource tables](../../core/cmd/burrow/tui.go).

## Shell panes are a separate production boundary

The accepted VT proof releases Bubble Tea's terminal and opens `/dev/tty` for its
own fullscreen attachment loop. It performs full redraws on changes with 30 ms
idle polling. It demonstrates VT restoration, background emulator state, and PTY
resize, but cannot simply keep clickable Burrow chrome visible around that raw
attachment. Source: [screen_linux.go](../../core/prototype_terminal/screen_linux.go).

If tabs and sidebar must remain visible during shell interaction, prove an embedded
terminal viewport under the one Bubble Tea renderer. Pinned x/vt has `Draw`,
`SendKey`, `SendMouse`, and a synchronized emulator wrapper; these are useful
building blocks, not proof that Burrow's integration is complete. Keyboard
encoding, paste, mouse coordinates relative to the pane, cursor placement,
focus escape, emulator replies, background output, and PTY resize must be checked.
Sources: [pinned x/vt emulator](https://github.com/charmbracelet/x/blob/3986e9119cf9/vt/emulator.go),
[keyboard input](https://github.com/charmbracelet/x/blob/3986e9119cf9/vt/key.go),
[mouse input](https://github.com/charmbracelet/x/blob/3986e9119cf9/vt/mouse.go),
[synchronized wrapper](https://github.com/charmbracelet/x/blob/3986e9119cf9/vt/safe_emulator.go).

A direct Ultraviolet import may become justified at this VT input/drawing seam;
it would use the already selected module, not require a new UI framework. Avoid
adopting its standalone event loop or renderer alongside Bubble Tea. No reason
was found to add Ghostty bindings, bubblezone, a separate menu/tab framework, or
creack/pty solely for the requested management redesign.

## Small acceptance boundary before more feature UI

Implement and review the fixed layout in production before scattering new
features through the current monolithic view. Preserve central overview tables,
Dracula styling, command completion, right metadata, and narrow-terminal behavior.
Reserve sidebar space for workspace navigation above the New/Menu row and shell
navigation below; scrolling must not move those controls out of reach. Keep
daemon health visible when the lower-priority metadata panel collapses.

One focused Aspect-driven interaction check should cover resize followed by
clicking each control, modal click interception and focus restoration, keyboard
equivalents, long lists, and narrow mode. Add an async workspace-switch scenario:
old responses must not overwrite the newly selected workspace, and background
shells must retain their original workspace identity. A later VT acceptance
should cover a fullscreen terminal app, resize, switching away/back with output
continuing, mouse translation, and exiting the frontend. These are recommended
checks, not tests claimed to have run during this research.

## Documentation provenance and limits

Context7 was used via resolve-library-id followed by query-docs for Bubble Tea,
Lip Gloss, and Bubbles. Selected IDs were
`/websites/pkg_go_dev_github_com_charmbracelet_bubbletea`,
`/charmbracelet/lipgloss`, and `/charmbracelet/bubbles/v2.0.0`.
The Bubble Tea result returned v1-style `WithMouseCellMotion`, and some other
results mixed main-branch and older examples. They were treated as discovery,
then corrected against cached **pinned** v2 sources; copy-pasting those snippets
would introduce outdated API usage. Pinned source establishes the API claims
above. No production code or dependency versions changed in this investigation.
