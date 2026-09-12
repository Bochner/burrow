# Terminal presentation regression repair references

Researched 2026-09-12 for the owner's post-#67 visual feedback. These are source-backed
repair recommendations, not evidence that the production frame already passes them.
The owner's latest request supersedes the earlier Dracula preference; Catppuccin
Mocha is the owner-selected default (Herdr’s regular `catppuccin`, not Latte).

## Sources and scope

- Herdr, inspected at `9ad65d9031e8cb16a7b553c0e6f74809e9811e92`, read-only
  checkout in `.references/herdr`. This is the previously researched pin, not a
  claim about the latest release. Its Rust/Ratatui renderer supplies visual and
  interaction evidence; do not transplant its renderer or terminal architecture.
- LazySSH, inspected at `9eb84452c31cb527bf8e938e23ffc92974fb91cb`, existing
  checkout in `.references/lazyssh`. Source and relevant tests were inspected;
  upstream tests were not executed for this research.
- Burrow's preserved accepted prototype, inspected at
  `65ca264605caa9e0b460012823264acc02d050a9`. Its fixture behavior is not a
  production SSH specification. The owner now explicitly rejects its duplicated
  workspace header, so that part should not be restored.

## Findings and concrete repairs

### Semantic colors and backgrounds

Herdr explicitly names palette roles: primary accent, panel background, active
row, navigation selection, surfaces, primary/secondary text, and status colors.
Its default is Catppuccin Mocha: blue accent `#89b4fa`, panel `#181825`, active
row/base `#1e1e2e`, selection `#313244`, main text `#cdd6f4`, secondary text
`#a6adc8`. The palette separates status colors from decoration.
[Source: palette roles and values](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/src/app/state.rs#L35-L100).

Recommendation: use one dark base and a distinct opaque popup surface, primary
text for ordinary content, readable secondary text for metadata, blue for focus
and headings, and explicit green/yellow/red status roles. Reserve richer syntax
colors for actual syntax. Switching RGB values alone will not repair background
ownership or inconsistent styling.

The accepted Burrow prototype already solves a relevant compositing problem:
after ANSI parsing it fills only popup cells whose background is absent, leaving
explicit selection backgrounds intact. Its help-surface test checks every popup
cell for background patches. Reuse this invariant rather than overwriting every
cell's background during final frame painting.
[Source: popup composition](https://github.com/Bochner/burrow/blob/65ca264605caa9e0b460012823264acc02d050a9/core/prototype_terminal/design.go#L447-L465),
[source: cell-level regression check](https://github.com/Bochner/burrow/blob/65ca264605caa9e0b460012823264acc02d050a9/core/prototype_terminal/terminal_test.go#L18-L42).

### Spacing and sidebars

Herdr reserves the sidebar's last column for a vertical separator, excludes that
column from content, and insets workspace row text. The separator is rendered
independently of the sidebar's rows; it is not an incidental table edge.
[Source: full-height separator](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/src/client/shell/render.rs#L16-L29),
[source: content bounds and row inset](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/src/client/shell/sidebar.rs#L9-L85).

LazySSH's standard tables explicitly use two columns of horizontal cell padding;
information panels use one vertical and three horizontal cells. The accepted
Burrow prototype reserves two columns between its main content and sidebar and
adds vertical breathing room when height permits.
[Source: LazySSH table and panel spacing](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/ui.py#L190-L211),
[source: prototype layout budget](https://github.com/Bochner/burrow/blob/65ca264605caa9e0b460012823264acc02d050a9/core/prototype_terminal/design.go#L14-L74).

Recommendation: budget separators and at least one inset cell before assigning
panel content widths; keep middle-panel rules inside the middle panel. Restore
blank rows between sidebar groups and between major central sections at normal
terminal heights. Compact intentionally at small dimensions, rather than using
the compact layout everywhere. Remove the top workspace name as requested;
retain the sidebar's selected workspace and useful prompt context.

### Fixed help geometry

Herdr calculates a centered `76 × 22` popup, clipped to terminal size, before
measuring/wrapping help lines. Scrolling changes only the content offset inside
its body rectangle; the title, search row and footer have reserved space.
Its popup helper depends on terminal bounds, never remaining help lines.
[Source: bounded popup rectangle](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/src/client/shell/overlays.rs#L256-L268),
[source: help body and scroll calculation](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/src/client/shell/overlays.rs#L997-L1103).

Recommendation: calculate Burrow's help width/height once per terminal size,
reserve chrome, then render a fixed-height viewport into that rectangle. Do not
size the popup from a sliced string or strip trailing blank viewport rows.
Use the accepted prototype's near-full-terminal reference proportions rather
than copying Herdr's literal dimensions: Burrow documents command syntax, not
only keybindings.
[Source: accepted help geometry](https://github.com/Bochner/burrow/blob/65ca264605caa9e0b460012823264acc02d050a9/core/prototype_terminal/help.go#L183-L217).

### Quit controls and syntax readability

The prototype pads both quit actions and applies a selected background to only
the chosen action. Herdr paints the entire button rectangle with its style,
including padding, and uses high-contrast text on accent-filled controls.
[Source: prototype quit actions](https://github.com/Bochner/burrow/blob/65ca264605caa9e0b460012823264acc02d050a9/core/prototype_terminal/design.go#L480-L489),
[source: full button rectangle](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/src/client/shell/overlays.rs#L269-L273),
[source: confirmation control styles](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/src/client/shell/overlays.rs#L1130-L1189).

Recommendation: restore a visibly filled selected quit action with readable
foreground, padding, and a non-color cue. Preserve the existing safe default
and keyboard behavior. Test final composed cell colors, not merely the presence
of an ANSI escape sequence before composition.

LazySSH centrally distinguishes command keywords, values, numbers, comments,
table headers and ordinary rows. Its command help uses explicit markup for
command words and argument values. Burrow's accepted prototype additionally
styles flags, uppercase argument placeholders, optional brackets and examples.
[Source: LazySSH semantic roles](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/console_instance.py#L21-L61),
[source: command reference markup](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/command_mode.py#L1044-L1063),
[source: prototype syntax renderer](https://github.com/Bochner/burrow/blob/65ca264605caa9e0b460012823264acc02d050a9/core/prototype_terminal/help.go#L151-L178).

Recommendation: restore semantic highlighting in help and examples using
existing command metadata. Keep descriptions neutral, optional punctuation
muted, and required argument placeholders distinguishable. This does not need
a new general-purpose syntax parser or an embedded editor.

## Verification limits and repair checklist

LazySSH's table tests verify construction, title, and header visibility; they
do not prove spacing or final rendered color correctness.
[Source: table tests](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_ui.py#L157-L173).

The production repair should check final terminal cells for base/popup/selected
backgrounds, invariant help geometry at top/middle/bottom and short topics,
visible separators with gutters, and bounds at normal/narrow/short terminal
sizes. A real terminal walkthrough remains necessary for perceived readability.
This research did not run Herdr, LazySSH or Burrow interactively and does not
establish subjective visual acceptance. Charm API capability verification is
being conducted separately through Context7 in the implementation task.

## Crush-style command-help strip

Additional owner direction: adapt Crush's dark command-help strip below the
terminal/prompt. Inspected current Crush source at
`19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149`, cloned read-only into
`.references/crush`; no upstream build or interactive run was performed.

Crush's `Status` owns a Bubbles v2 `help.Model`, receives the application's
`help.KeyMap`, installs centralized key/description/separator styles, and sets
available width after subtracting horizontal padding. Drawing uses
`help.View(keymap)` inside the status help style. This is the existing Charm
component to reuse, not a reason to hand-build command spacing.
[Source: status component](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/model/status.go#L19-L77),
[source: differentiated help text roles](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/styles/quickstyle.go#L534-L542).

The layout budgets a one-line compact help strip at the bottom independently
of editor height. Full help increases its reserved height; app margins separate
the editor from the strip. Status is drawn after the editor. Short-help bindings
adapt to focus and busy state and finish with quit and help actions.
[Source: bottom layout allocation](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/model/ui.go#L3705-L3763),
[source: draw ordering](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/model/ui.go#L3130-L3144),
[source: contextual short help](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/model/ui.go#L3283-L3372).

Important distinction: at this pin, `Status.Help` sets horizontal padding but
does not itself set a background; nontransparent mode sets the application's
background through `tea.View.BackgroundColor`. A distinctly darker Burrow
strip is therefore an intentional adaptation of the owner's requested visual,
not a claim that current Crush paints a separate dark footer surface.
[Source: status style](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/styles/quickstyle.go#L1029-L1039),
[source: app background](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/model/ui.go#L3231-L3237).

Recommendation: reserve a bottom row beneath Burrow's prompt, fill its entire
width with Catppuccin mantle/crust, inset one cell, and use Bubbles help styling
to distinguish readable keys from quieter descriptions/separators. Populate it
from real active key bindings, keep it outside scrollable output, and preserve
the existing full help overlay rather than introducing a second help system.
Include bottom-row opacity, terminal width, prompt cursor placement, and narrow
terminal truncation in the rendering check. Preserve explicit key foregrounds
when applying the strip background; this is the same cell-background ownership
problem as popup selections.

## Crush Ctrl+P command palette

The palette was verified by reading the actual source at the same Crush pin,
including `internal/ui/dialog/commands.go`, `commands_item.go`,
`internal/ui/list/filterable.go`, `internal/ui/styles/quickstyle.go`,
`themes.go`, and the application's opening/keybinding path. It was not inferred
from screenshots.

The global command binding is Ctrl+P. Opening constructs a command dialog, or
brings the existing one to the front. Construction focuses a text input with
the placeholder `Type to filter` and selects the first list item. The palette
uses a centered rectangle with nominal maximum dimensions 70 by 20, bounded by
terminal dimensions. Title, filter input, fixed-height result list and footer
have separately budgeted space; filtering does not shrink the result area.
[Source: global binding](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/model/keys.go#L99-L102),
[source: opening](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/model/ui.go#L4621-L4648),
[source: focused filter](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/dialog/commands.go#L93-L105),
[source: dimensions](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/dialog/dialog.go#L15-L18),
[source: palette layout](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/dialog/commands.go#L291-L334).

The distinctive border is explicitly
`Border(lipgloss.RoundedBorder()).BorderForeground(o.primary)`. The title uses
the same primary role. Selected rows use primary background with `onPrimary`
foreground; both ordinary and selected rows have one-cell horizontal padding.
In the default Charmtone Pantera theme these roles are `charmtone.Charple` and
`charmtone.Butter`, respectively. Burrow should map those roles to its chosen
Catppuccin accent/contrasting text instead of importing a second palette.
[Source: title, border and row styles](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/styles/quickstyle.go#L933-L971),
[source: default theme roles](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/styles/themes.go#L34-L48).

Typing filters titles, aliases and descriptions using `fuzzy.FindFrom`; query
changes reset selection and scroll to the top. Up/down wrap through matches;
Ctrl+P inside the palette also moves upward. Enter (or Ctrl+Y) returns the
selected item's existing action, and does nothing when there is no selected
item. Escape cancels. Shortcut labels yield space before command names become
cramped. The selected item carries its existing action rather than dispatching
by a position in the filtered list.
[Source: keyboard and filtering behavior](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/dialog/commands.go#L180-L239),
[source: fuzzy filter](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/list/filterable.go#L70-L118),
[source: searchable fields and action](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/dialog/commands_item.go#L63-L111),
[source: optional shortcut column](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/dialog/commands.go#L312-L322).

Recommendation: evolve Burrow's existing action menu into a Ctrl+P palette
while retaining Alt+M as an alias. Reuse the existing five action definitions
and dispatch, add a focused filter input and stable result area, and preserve
the user's prompt draft on cancellation. Use an explicit accent-colored rounded
border and high-contrast filled selected row. For five entries, existing
filtering or case-insensitive substring matching is sufficient; that is a
deliberate difference from Crush's fuzzy matching, not fuzzy-search parity.
Keep existing confirmation and workflow ownership when executing an action.
Check empty results, selection after filtering, arrow wrapping, opening/closing
without changing the prompt, and final composed border/selection colors.

## Charm API verification and repair decision

Context7 was queried on 2026-09-12 after resolving `/charmbracelet/lipgloss`,
`/charmbracelet/bubbles`, and `/catppuccin/catppuccin`. Its viewport and compositor
examples were checked against the actual pins: Lip Gloss **v2.0.6**, Bubbles
**v2.2.1**, Bubble Tea **v2.0.9**. No dependency upgrade is needed.

- `lipgloss.Canvas` exposes `CellAt`, `SetCell`, `Compose` and `Render`; use it to
  fill unspecified foreground/background cells before composing panel layers,
  preserving explicitly colored selections. `Style.Render` around an already
  composed ANSI string does not establish every cell's background. The actual
  production VT captures confirm black/default-background patches on labels.
  [Pinned Canvas](https://github.com/charmbracelet/lipgloss/blob/v2.0.6/canvas.go),
  [pinned compositor](https://github.com/charmbracelet/lipgloss/blob/v2.0.6/layer.go).
- `viewport.New(WithWidth, WithHeight)`, `SetContent`, `SetYOffset`, and `View`
  provide bounded scrolling. Pinned `View` pads its content to the assigned width
  and height; scroll position need not affect popup dimensions. Keep modal title,
  footer and action rows outside that body. Use the existing frame key routing
  and native identified layers for clicks.
  [Pinned viewport](https://github.com/charmbracelet/bubbles/blob/v2.2.1/viewport/viewport.go).
- Text input has separate `Focused`/`Blurred` styles and a cursor style through
  `SetStyles`. Configure all of them from the same palette instead of retaining
  Bubbles' default terminal-color prompt and grey suggestions.
  [Pinned input styles](https://github.com/charmbracelet/bubbles/blob/v2.2.1/textinput/styles.go).
- Catppuccin's official style guide assigns base to the main pane, mantle/crust
  to secondary panes, subtext to labels, and green/yellow/red to status. The
  owner confirmed Herdr's regular `catppuccin`, which its source identifies as
  Mocha, explicitly excluding Latte. Adopt the Mocha values from that source;
  preserve text/markers when color is disabled.
  [Official style guide](https://github.com/catppuccin/catppuccin/blob/main/docs/style-guide.md).
- General code-document highlighting would justify Chroma when that viewer
  exists. Today's command help has known syntax and output is generated JSON:
  reuse the prototype's semantic token styling and standard-library JSON
  validation for that bounded content. Avoid adding an editor/lexer framework
  merely to color command labels and scalar JSON output.

The owner-reported defects are reproduced in Aspect-generated artifacts under
`/tmp/burrow-ui-before/`: 80×24 and 160×40 management, help top/bottom, and quit
choices. The captures pass the actual production ANSI view through pinned x/vt
cells; their SVG export preserves cell foreground/background values. These are
rendered production snapshots with synthetic identities, not a claim that a live
SSH connection exists. The checklist is `/tmp/burrow-ui-regressions-checklist.md`.

## Screenshot and cursor verification

The owner supplied `/home/bochner/dev/crush.png` (Crush v0.94.1 shown). It was
visually inspected for the dark dialog, accent border, slash title decoration,
filter placeholder, full-width selected row and bottom help placement. The
source pin above is independently recorded; it is not assumed to be that exact
screenshot release.

Crush explicitly calls `textinput.SetVirtualCursor(false)` for its filter and
returns its cursor through the dialog renderer. Burrow adopts that same native
cursor approach, with `tea.View.Cursor` offsets derived from the dialog bounds;
the terminal handles blinking rather than routing synthetic blink timers through
workspace command results. Context7's Bubbles v2 cursor documentation was checked
against pinned `textinput.Cursor()` and `Styles.Cursor.Blink`.
[Crush filter cursor](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/dialog/commands.go#L102-L106).

Burrow uses the pinned Lip Gloss `Blend1D` helper for the screenshot's slash
header treatment, with Catppuccin lavender/mauve rather than Crush's palette.
The footer uses Bubbles `help.Model.ShortHelpView`, including width-aware
truncation. Dialog selection remains a full native identified row, so its drawn
highlight and mouse target share bounds. No animation engine or dependency
upgrade is introduced.

## Implemented checks and review

`presentation_test.go` feeds production ANSI into the pinned VT emulator and
checks final backgrounds and quit-selection cells, stable help/palette geometry,
sidebar separators/gutters, no-color selection, semantic JSON output, palette
filter dispatch, cursor visibility/blink configuration, overflow, and narrow
recovery. Its SVG/ANSI artifacts are retained in `/tmp/burrow-ui-after/`; the
80×24 and 160×40 views were rendered to PNG and visually inspected. Static
captures do not prove animation; the real PTY check additionally verifies the
terminal's native blinking-block cursor request.

`setup_check.py` drives Ctrl+P, filtering, Enter and Escape in a real PTY, then
creates/switches workspaces and checks draft retention, resize, no-color output,
quit choices, daemon verification, and terminal restoration. Palette clipboard
callbacks carry workspace, modal and input-instance identity so a late paste
cannot alter the management prompt or a reopened dialog.

Separate standards and feedback/spec reviews found and resolved workspace-row
capacity drift, shell footer clipping, no-color tab identity, and hidden input
at narrow widths. The final reviews have no outstanding findings. Subjective
owner acceptance remains a separate walkthrough; this file does not declare it.

Final validation: `aspect burrow format` and `aspect burrow-check` passed on
2026-09-12. The gate passed all 15 portable checks and the digest-pinned Docker
SSH lab, including cross-workspace connection isolation and real PTY workflows.

## Owner refinement and sample preview

The follow-up critique removes the sidebar's “This session” and workspace
range/wheel labels, aligns New/Menu at opposite inset edges, renames the palette
heading to Menu, and centers confirmation text/action groups. Context7's Lip
Gloss placement and table styling documentation was checked before using
`PlaceHorizontal` and fixed per-column widths. Saved configurations and tunnels
now use consistent empty-state sections instead of empty header-only tables;
active SSH instructions remain in help. Active rows reserve a selection gutter
so highlighting does not shift their columns.

The requested sample data is available with `aspect burrow run -- --demo`.
It shows saved configurations, SSH states, and tunnels in the same frame, with a
persistent DEMO label. It skips workspace launch/polling and refuses operational
commands and workspace creation. It is a presentation preview, not evidence of
implemented saved-configuration or tunnel capabilities. A real PTY check starts
it without a workspace or Hovel cache and verifies sample rows and quit.
Updated empty/populated and centered-dialog captures are in
`/tmp/burrow-ui-refined/`.

Refinement validation: `aspect burrow format` and the full `aspect burrow-check`
gate passed, including all 15 portable checks and the Docker SSH lab.
