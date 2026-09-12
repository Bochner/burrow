# Hovel and LazySSH interaction references

Inspected 2026-09-11 for [Which terminal interaction model fits the agreed
boundaries?](https://github.com/Bochner/burrow/issues/15). This is source-backed
design evidence, not a parity claim. The inspected local checkouts are Hovel
`c461ba282a8aecc7aa3a079a4613bf5e2640c388` and LazySSH
`9eb84452c31cb527bf8e938e23ffc92974fb91cb`.

## Hovel: reuse its interaction vocabulary

Hovel's catalog demonstrates dense command tables, inspection panels, structured
log events, status overviews, and transfer lifecycle displays. Its shared styles
use rounded borders, cyan/magenta accents, and semantic success/warning/danger
colors. Its renderer consumes structured command results to choose tables and
inspection views, including contextual next-command hints. These are visual and
interaction references; Burrow must not import their `core/internal` packages.
[Catalog](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/ui-components.html),
[styles](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/clistyle/styles.go),
[renderer](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/commandview/renderer.go).

The interactive prompt uses contextual suggestions with descriptions, an explicit
selection highlight, and a live context prefix. This favors searchable actions
against the selected connection in Burrow. It does not imply that Hovel already
supplies a Bubble Tea shell multiplexer.
[Prompt and completion](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/cli/cli.go#L347-L380).

Transfer rendering derives its bar from bytes/total and emits plain lifecycle
messages when live rendering is disabled. Unknown totals do not get a percentage.
Tests check plain output without ANSI and a completed live transfer. The browser
walkthrough applies the same distinction using clearly labeled sample bytes;
real measurements remain an implementation requirement.
[Transfer source](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/progressview/transfer.go),
[checks](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/progressview/transfer_test.go).

## LazySSH: preserve useful access and discovery

The actual command-mode entry path uses `prompt_toolkit`, history, and
`LazySSHCompleter`; initial status calls the saved-profile, connection, and tunnel
table renderers. Rich is the presentation mechanism, not the whole interaction
model. Keep completion and named access, rather than translating every Rich
factory into a new Charm abstraction.
[Command flow](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/command_mode.py#L301-L380),
[completion checks](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_command_mode.py).

Connection tables expose name, host, user, port, dynamic port, tunnel count and
socket path. Tunnel tables expose identity, type, local port and remote endpoint.
Burrow can put name/status in the list, user/host in the selected header, and
endpoint details beneath a tunnel summary. Saved settings must remain distinct
from live access; visual presence alone must never imply a connected transport.
[UI renderers](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/ui.py#L91-L191),
[renderer checks](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_ui.py).

LazySSH also has plain-text/no-color and animation-disable tests. Preserve the
capability, without copying its environment-variable names or all styling
factories. The owner explicitly clarified that some animation is desirable in
Burrow: keep activity animation on by default, with an optional off switch.
[Plain output checks](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_plain_text_mode.py),
[animation checks](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_animation_disable.py).

One caution from tracing callers: the live-monitor helper assigns the literal
status `Connected`, while the main command-mode path calls the ordinary status
tables. Existence of that dashboard helper is not proof of live health monitoring
or the primary user experience. Burrow's future status must follow verified
connection events, not a row's presence.
[Helper](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/ui.py#L487-L527),
[main status callers](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/command_mode.py#L327-L342).

## Applied to the disposable walkthrough

- Search connections by name, host or user; show identity beside semantic status.
- Contextual action picker and visible help; management shortcuts inactive in shells.
- Compact summaries with expandable tunnel/run details; preserve open sections.
- Dark/light previews, explicit no-color and no-animation controls; normal text copy.
- Indeterminate connection activity and determinate sample-byte transfer progress.

The [public feedback research](charm-operator-feedback.md) supplies the independent
Crush/Gum/Glow/Soft Serve comparison. No upstream source was copied. These readings
did not run either upstream UI or its tests, and do not establish complete LazySSH
workflow parity or a production Charm dependency selection.
