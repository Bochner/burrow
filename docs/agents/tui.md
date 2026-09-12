# TUI presentation standard

Owner-required project convention, recorded 2026-09-12. Apply when implementing
or changing terminal views, table columns, command output, help, dialogs, themes,
or operational metadata. This is an implementation requirement; research files
provide its evidence, not its authority.

## Semantic colors and syntax

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

Require a minimum terminal of **160 columns × 40 rows**. Below it, show a
resize notice with required/current dimensions, preserve state, block hidden
input, and allow Ctrl+C to exit. Above it, keep both sidebars and all table
columns; extra space can expand the center. Use one layout, without compact
column sets, hidden sidebars or abbreviated section variants.

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
below-minimum resize notices and changed modal states. Use
`aspect burrow run -- --demo` for sample data.
A passing automated check does not establish subjective owner acceptance.

Evidence: [LazySSH fields and metadata](../research/lazyssh-table-metadata.md)
and [UI repair references](../research/ui-reference-repair.md).
