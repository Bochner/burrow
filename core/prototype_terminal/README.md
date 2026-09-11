# Disposable terminal interaction walkthrough

For [Which terminal interaction model fits the agreed boundaries?](https://github.com/Bochner/burrow/issues/15).
The owner approved the connection-first layout, full-screen shells, responsive
management panes, color plus text status, and truthful progress on 2026-09-11.

Open `index.html` directly in a browser. It is one self-contained file, requires
no server or dependencies, and sends no network requests. Use the three guided
scenarios and the narrow/no-color/light-theme/no-animation controls. Tab and Enter operate buttons;
`s` opens a sample shell, `q` quits management, and Ctrl-] backgrounds a shell.
`/` finds connections, Ctrl-P opens contextual actions, and `?` shows help.
Collapsed details keep the main screen compact. Native selection/copy is preserved.
Animations are enabled by default: an indeterminate spinner while connecting and
short progress-bar transitions where the browser supports them. Reduced-motion
preferences and the explicit no-animation switch turn those effects off.

This is a simulated interaction model, not a Charm implementation or a terminal
emulator. Advance activity explicitly to simulate output and transfer byte counts.
The existing SSH/PTY proof remains in `../prototype_transport/OWNERSHIP.md`;
this walkthrough does not establish real redraw, input, resizing, or persistence.
Eight retained plain output lines deliberately illustrate truncation rather than
attempting to emulate terminal screens. Browser controls stand in for terminal
widgets; exact keyboard bindings remain subject to real terminal testing.

Run `aspect burrow-prototype interaction` to check the model's background work,
bounded output, quit/close distinction, loss, and explicit reconnect behavior.
This check uses the repository's pinned Node toolchain through the existing
[rules_js test rule](https://github.com/aspect-build/rules_js/blob/v3.2.2/js/defs.bzl).
It also parses both embedded scripts, but is not browser rendering validation.
No new dependencies or upstream source were added.

Pending: owner feedback on this walkthrough, compatible Charm-family selection
and integration proof, and the separate full-screen terminal restoration decision.
Keep this asset on the shared SSH-specification milestone branch until its next
validated consolidation. Approval of the direction is not acceptance of an
unimplemented frontend.
