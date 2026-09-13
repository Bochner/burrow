# Herdr sidebar and selection polish

Researched 2026-09-13 for the owner's sidebar, SSH tab and selection feedback.
Primary source: the existing read-only `.references/herdr` checkout at
`9ad65d9031e8cb16a7b553c0e6f74809e9811e92`. This is an inspected pin, not a
claim about the latest release. No upstream code was changed or run.

## Source findings

- Herdr's default workspace card has two rows: state icon plus workspace,
  then branch/git details. Its agent card similarly places state and identity
  first, with agent information underneath. This supplies the requested visual
  hierarchy, not SSH-specific behavior.
  [Default card rows](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/src/config/sidebar.rs#L440-L475).
- Non-indented workspace cards begin their first row one cell inside the card;
  secondary rows begin three cells inside. Status, workspace and secondary
  tokens receive separate styles. Names can be emphasized without recoloring
  their status circles; secondary information is quieter.
  [Row alignment and token styles](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/src/client/shell/sidebar.rs#L652-L718).
- Selection is a background-only pass over the rendered cells. It preserves
  individual foreground colors. The renderer distinguishes active workspace
  from navigation selection, using different backgrounds.
  [Background-only selection](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/src/client/shell/sidebar.rs#L721-L735).
  Catppuccin Mocha uses `#1e1e2e` for active rows and `#313244` for navigation
  selection, rather than a saturated blue selection fill.
  [Palette roles and values](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/src/app/state.rs#L32-L99).
- Icon, text and color derive from the same status. Unknown has a distinct
  symbol/color. Herdr's statuses describe agents (working/blocked/done/idle),
  so its exact state-to-color mapping must not be copied as SSH semantics.
  [Status presentation](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/src/client/shell.rs#L198-L258).
- Card height and inter-card gaps drive scrolling and hit rectangles, so a
  two-line visual card remains one bounded workspace target.
  [Row measurement and targets](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/src/client/shell/sidebar.rs#L223-L339).

## Minimal Burrow adaptation

Use the existing frame, theme roles, observation state and identified layers:

1. Align each workspace's status circle/name and its dimmed shell detail with
   fixed gutters. Give the shell its own observed status circle. Recalculate
   visible row capacity and targets together; keep narrow layouts usable.
2. Use green for observed connected/running, yellow for pending or uncertain,
   red for failed/disconnected, with textual state available. Workspace SSH
   status must not be inferred from Hovel daemon health or current selection.
   Keep unknown distinguishable from a known disconnection, as required by
   [Burrow's TUI standard](../agents/tui.md).
3. Preserve semantic foregrounds when highlighting resource rows: replace only
   backgrounds, and reserve a fixed textual selection gutter. The inspected
   Burrow `frame.go` selected-row branches strip ANSI before repainting with
   `selectedStyle`; that is the direct source of lost field colors.
   [Burrow frame](../../core/cmd/burrow/frame.go),
   [shared theme](../../core/cmd/burrow/theme.go).
4. Clicking outside resource targets should clear resource selections without
   ending a shell or changing workspace ownership. Keep active tab identity
   separate: an SSH tab should use the same selected marker/style as Burrow and
   Hovel. These behaviors come from the owner's request, not an assertion that
   Herdr implements identical click-away semantics.

No renderer transplant, configurable card schema, new dependencies, backend
state or SSH lifecycle changes are needed. Verify final VT cell colors,
selection clearing, fixed column positions, tab markers and status text in
color and NO_COLOR at 80x24, 120x30, 160x40 and a larger size. This source
research alone does not establish subjective visual acceptance.
