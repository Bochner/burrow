# TUI presentation standard

Owner-required project convention, recorded 2026-09-12. Apply when implementing
or changing terminal views, table columns, command output, help, dialogs, themes,
or operational metadata. This is an implementation requirement; research files
provide its evidence, not its authority.

## Semantic colors and syntax

The color scheme is mandatory across the entire Burrow-owned interface: every
screen, panel, popup, form, recap, help view and operational output. The accepted
sidebar and resource tables are the baseline, not exceptions. Use shared theme
roles and form styles; do not add default-white blocks of structured information
or a separate palette for a new feature. Ordinary explanatory prose may use the
base or secondary text role. Embedded terminal programs keep their own output
colors; Burrow's surrounding controls still follow this standard.

Connect and close recaps must distinguish category labels (for example `Key:`),
connection names, usernames, hostnames/IPs, ports, paths, states and command
tokens. Help must distinguish commands/subcommands, flags, placeholders and
keybindings from explanatory prose. Generated SSH command/config previews must
use these same roles when implemented. NO_COLOR preserves text and controls.

Use the shared Catppuccin Mocha roles in `core/cmd/burrow/theme.go` (Herdr's
regular `catppuccin`). Keep field meaning consistent across tables, metadata,
help and output. Use the shared role functions rather than local RGB literals.

| Meaning | Role |
| --- | --- |
| Names, IDs, metadata headings, column headers | Lavender |
| Hosts and remote endpoints | Pink |
| Usernames and successful states | Green |
| Ports and pending/warning states | Yellow |
| Keys, shells, proxy information | Teal |
| Types and terminal methods | Mauve |
| Counts, sizes, durations, numeric JSON | Peach |
| Failure/refusal/disconnected | Red, accompanied by a text label |
| Paths, unavailable fields, explanatory labels | Subtext |
| Section titles, command keywords/flags and JSON keys | Blue |

Color syntax by token: distinguish commands/flags, argument placeholders,
strings, numbers, booleans and errors. Preserve the exact underlying text and
respect NO_COLOR. Selected rows may replace field colors with the high-contrast
selection foreground/background; retain a textual selection marker.

## Table detail and formatting

Trace the relevant pinned LazySSH source and tests before implementing a table.
Preserve useful fields and document intentional differences. The reference
inventory is:

- Saved configurations: name, host, username, port, SSH key/authentication,
  shell, proxy, no-terminal setting.
- Active SSH: name, host, username, port, dynamic port, terminal method,
  tunnel count, socket path; expose observed connection state in metadata.
- Tunnels: ID, owning connection, type, local port, remote destination.

Use aligned columns and shared per-field styles, with headers distinct from
values. Reserve selection space so selecting a row never shifts its columns.
Keep consistent section gaps, interior padding and sidebar separators.

Keep table section titles left aligned and center headers and values within each
column, including selected rows. Shrink the center with the available terminal
width using native table sizing; retain columns and truncate long cell text.
Keep both sidebars visible, allowing them to narrow when needed. Preserve drafts
and selection through resize. A 160x40 terminal is a useful full-detail preview,
not a launch or viewing requirement: never replace the application with a
blocking resize notice. At extremely small sizes, keep a clipped application
view and allow Ctrl+C to exit while suppressing hidden controls.

Keep command instructions in help. Empty states remain concise and truthful; sample data belongs only in explicitly labeled preview mode.

Center confirmation titles, messages and action groups. Keep searchable menus
and data lists aligned for scanning. Dialog geometry must be independent of
scroll/filter position. Preserve visible focus, pointer targets and cursor
placement through resize and NO_COLOR.

## Operational metadata and future implementation

The right sidebar prioritizes connection state, download totals and Hovel health
before diagnostic paths. Derive connected/disconnected/connecting/failed from
observed connection state, independently of daemon verification. Unknown or
failed observations must remain distinguishable from disconnected.

When transfers are implemented, obtain download count and downloaded bytes from
the capability that owns completed transfer records. Specify selected-connection
versus workspace scope. Count completed downloads consistently across single and
batch operations; exclude uploads, failed/partial attempts and duplicate events.
Until that source exists, display Unavailable with the capability limitation;
a missing metric is not zero. Demo values must remain visibly labeled samples.
Never synthesize operational success from renderer state or scan arbitrary
workspace files to infer transfer history.

## Required completion checks

For presentation changes, extend the existing interaction/presentation checks
with assertions that would catch the changed behavior: final VT cell colors,
retained text in NO_COLOR, column alignment, stable bounds, selection or truthful
metadata. Run `aspect test //core/cmd/burrow:interaction_test` while iterating and
the relevant Aspect gate before completion. `aspect burrow-check` includes these
checks and the production PTY/SSH checks.

Inspect empty and populated captures at 160x40 and a larger terminal, plus
smaller terminals (120x30 and 80x24) and changed modal states. Use
`aspect burrow run -- --demo` for sample data.
A passing automated check does not establish subjective owner acceptance.

Evidence: [LazySSH fields and metadata](../research/lazyssh-table-metadata.md)
and [UI repair references](../research/ui-reference-repair.md).
