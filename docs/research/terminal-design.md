# Accepted base terminal design

Owner acceptance (2026-09-12): the owner confirmed the popup background fix and
accepted this base UI for now. This supersedes rejection/pending-review notes in
the historical iterations below. It accepts the visual and interaction foundation,
not production SSH parity. The resolution of issue #42 links the preserved source on the shared milestone
branch; #16 owns the final implementation sequence.

The accepted frontend uses pinned Bubble Tea, Bubbles and Lip Gloss, with Dracula
Classic and a centered native brand panel. Help is a large centered overlay with
a dimmed workspace, semantic syntax highlighting, scrolling/topic navigation and
Esc restoration. It owns focus and preserves the prompt. Quit is centered, with
Quit Burrow left and Keep working right/default. Popup composition fills missing
background cells while preserving explicit selection backgrounds.

Validation: `aspect burrow-check ci` passed all 11 targets after the final shading
fix, including a cell-by-cell help-surface regression. Real-terminal help and quit
were exercised. Run `aspect burrow-prototype terminal -- --color`.

Spacing refinement: ordinary layouts now reserve a blank line between resource
tables, between inventory and output, below the header and above prompt controls.
Short terminals omit redundant table-header rules to preserve this spacing;
overflow remains scrollable. Wide layouts use one centered Burrow
brand panel in the sidebar, styled entirely with Lip Gloss.
Narrow layouts show one small Burrow label, with no duplicate branding.

## Current theme: Dracula Classic

The owner superseded Hovel colors and the interim rose accent with **Dracula
Classic** on 2026-09-12. Online research used the official
[Dracula specification](https://draculatheme.com/spec) and
[contribution palette](https://draculatheme.com/contribute). LazySSH's pinned
`console_instance.py` explicitly defines `LAZYSSH_THEME` using Dracula colors.

Burrow now uses the exact Classic palette: background `#282a36`, foreground
`#f8f8f2`, comment/borders `#6272a4`, selection `#44475a`, floating surfaces
`#343746`, purple `#bd93f9`, pink `#ff79c6`, cyan `#8be9fd`, green `#50fa7b`,
yellow `#f1fa8c`, orange `#ffb86c`, and red `#ff5555`. Purple is used for headings,
pink for commands and host/type accents, cyan for flags/focus, orange for numeric
columns, and green for success/examples. Filled selections use the selection
surface rather than a bright pink background. Bold is reserved for headings,
selection and errors. These application-role choices follow LazySSH; they are
not a claim that command-help tokens are programming-language syntax scopes.

All TUI colors use shared constants. The full management canvas uses the Classic
background, including empty cells; menus have floating surfaces. NO_COLOR keeps
terminal defaults. Attached shell programs retain their own output colors.
The older browser interaction sketch now uses Dracula Classic and its optional
light switch uses the official Alucard Classic palette. Historical Hovel/rose
notes below are superseded, not the active design.


## Historical review iterations — superseded

The following records preserve earlier feedback and evidence; their colors and
acceptance status are superseded by the accepted reference above.

Latest owner corrections: help now follows LazySSH's grouped syntax/descriptions,
separate command/flag/argument colors, green examples, and required/optional
connection-parameter guide. Arrow keys scroll help; Tab/Shift+Tab cycles topics
without editing the prompt. Pink is now the owner-requested muted rose `#b84b8a`
instead of Hovel's neon magenta; the other Hovel colors remain unchanged.
SOCKS proxies belong to the connection: the active table shows PROXY as `No` or
`Yes :PORT`. Only local/reverse forwards receive tunnel IDs, rows and counts.
The preview command for setting SOCKS is `proxy PORT`, also available through
`connect ... -proxy PORT`; loss/close clears it. This supersedes earlier references
to SOCKS tunnel rows in the historical notes below.


## Latest owner revision: persistent workspace and Crush references

The owner rejected the previous candidate's weak accents, stretched tables,
hidden profile rows, completion behavior and unused lower area. This revision
keeps every home resource in content-sized, titled tables; removes fixed row
caps; scrolls overflowing inventory with Ctrl+Up/Down; and refreshes rendered
state after operations. There is no production remote poller in this fixture.
At 110 columns and wider, a right sidebar retains the selected connection,
resource counts and activity, with Burrow branding above. Startup guidance sits
near the bottom prompt, below a labelled command-output area.

Ctrl+K opens an arrow-selectable command menu. Enter prepares the command rather
than silently executing an operation. Completion is a floating, highlighted
picker: Up/Down selects, Tab accepts a token and advances into ordered arguments,
and Esc dismisses it. Ctrl+C opens a quit dialog with Keep working selected;
Tab/arrows change the choice, Enter confirms, Esc cancels, and a second Ctrl+C
quits. Modal input preserves the draft. Attached shells retain their own Ctrl+C.
The fixture's dialog accurately states that its local shells and scratch files
are removed; production quit retains daemon-owned connections and tunnels.

A launcher defect contributed to the earlier missing colors and initial size:
Aspect captures stdout, which defeats automatic terminal detection on that file
descriptor. Interactive mode now uses the controlling terminal for rendering
when stdout is captured; JSON/script output still uses stdout. Automatic mode
respects NO_COLOR, while `--color` explicitly enables Hovel truecolor.

Source traced on 2026-09-12: Crush commit
[`19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149`](https://github.com/charmbracelet/crush/tree/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149),
particularly [sidebar](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/model/sidebar.go),
[logo](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/logo/logo.go),
[command menu](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/dialog/commands.go),
and [quit dialog](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/internal/ui/dialog/quit.go).
The current [license](https://github.com/charmbracelet/crush/blob/19e7467ed6aac753dc8ab42a5c0c91e7a0aa9149/LICENSE.md)
is FSL-1.1-MIT, not an unconditional current MIT grant. These are interaction
references; no Crush source, wordmark assets or application dependency is vendored.
Burrow's implementation composes its existing Charm primitives. Context7 was
queried for Bubbles textinput and Lip Gloss composition APIs; exact pinned
Bubbles 2.2.1 source was checked for suggestion update behavior.

Validation includes input-driven completion across background ticks, modal focus
and cancellation, uncapped resource rows, 20x8 through 160x48 bounds, and a real
controlling-PTY check with captured stdout proving color, dimensions, dropdowns,
quit confirmation and terminal restoration. This is a revised design candidate,
not owner acceptance or completed production parity.


The initial bare candidate below was rejected. The revised executable now has
stacked saved/active/tunnel tables, a visible arrow-selectable completion dropdown,
ordered required/optional connection arguments, contextual paged help, structured
file listings and a reviewed `mget` batch with measured progress. See the
[full MVP coverage and live collection evidence](terminal-mvp-coverage.md) for the
current contract and explicit implementation gaps. No visual acceptance is inferred.

Current walkthrough: `connect` → `shell` → Ctrl-] → `resume 1` → Ctrl-] →
`proxy 1080` → `scp` → `cd /var/log` → `mget *.log` → `confirm` → `transfers` →
`back`. Try `help mget`, F1/Esc, and type `con`, Down, Tab. Type `connect ` to
inspect required-first argument completion. Use 120×40 and 80×24.

The descriptions below preserve the previous iteration and dependency provenance;
the linked full-MVP coverage supersedes its compact three-column table design.

Owner review: rejected as too bare. The next design must reflect LazySSH's
organized saved-configuration, active-connection and per-connection tunnel
tables, richer information hierarchy and complete contextual help, using Hovel's
palette. Passing behavior checks did not establish visual readiness. The intended
destination remains a fully implemented Hovel-backed application; this document
records an unsuccessful design iteration, not an accepted implementation spec.

Decision: [What initial terminal design should Burrow build on before milestone planning?](https://github.com/Bochner/burrow/issues/42).
This revision preserves the accepted interaction and shell mechanism; its visual
quality still requires the owner's real-terminal walkthrough. It does not close
the decision or claim production SSH parity.

## Owner direction

- Initial demonstration: connect → shell → background/resume → tunnel → browse
  and transfer files → return to the resource overview. LazySSH source and docs
  are the workflow reference.
- Balanced density: compact resource tables, generous command output, restrained
  borders, persistent management overview and contextual bottom prompt.
- Use Hovel colors for every application-controlled component. Its exact
  [clistyle palette and emphasis](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/clistyle/styles.go)
  provide cyan headings/selection, magenta accents, muted metadata, neutral table
  text, green success, yellow activity and red errors. Remote programs retain
  their own terminal output. Color is optional; labels and selection survive
  `NO_COLOR`.
- Prefer newest stable Burrow dependencies unless they conflict with Hovel.
  Burrow owns its terminal frontend; Hovel supplies its daemon/API and public
  module SDK. Hovel's internal frontend dependency family is not a shared ABI.

## Walkthrough

From this checkout, in a real terminal:

```sh
aspect burrow-prototype terminal
```

Use 120×30 first, then 80×24 and a narrow terminal. If the environment sets
`NO_COLOR`, unset it for the color walkthrough; keep it set for the plain check.

1. Type `con`, use Ctrl-N/P to select a suggestion, and Tab to accept `connect`.
2. Type `shell`. Run `top` or `vi`; Ctrl-] returns to management. Open another
   shell, background it, and `resume 1`. Full-screen VT is the default;
   `--vt=false` retains the old plain-output comparison.
3. Type `proxy 1080`. The connection’s PROXY cell updates without a status refresh.
4. Type `scp`, `cd /var/log`, `ls`, `get system.log`. The bottom prompt identifies
   target, remote path, download directory (↓), and upload directory (↑).
5. `lcd /uploads` changes the download destination, matching LazySSH. `lls`
   lists that destination; `put notes.txt` reads the separate upload directory.
6. `back` returns to management. Up/Down recalls history and restores an edited
   draft; PgUp/PgDn reviews output. `clear` leaves the overview visible.
7. Resize, try `loss`, inspect disconnected state, then reconnect explicitly.
   `quit` ends the prototype and its local shells.

Wide terminals show the three resource tables alongside each other. Ordinary
terminals stack compact sections; short terminals retain resource summaries and
commands for detail. Extra rows are counted explicitly. This is bounded design
navigation, not a resolved large-fleet navigation scheme.

## What is real

The design executable uses real local PTYs, the pinned VT emulator and actual
temporary-file copying with measured, deliberately paced progress. Its connection
and tunnel rows remain explicitly simulated. It does not connect to the owner's
server merely because the design starts. No saved passwords or command history
are created.

The separate live check exercises the accepted OpenSSH/subsystem-pipe transport:

```sh
aspect burrow-prototype live -- HOST USER
```

It requires an existing trusted host key and prompts through OpenSSH for
authentication. The test creates a dedicated remote `/tmp/burrow-design.*`
directory, then removes its two files and directory, test shells, forwards and
temporary master. It validates SFTP with the newly pinned Go package, concurrent
SSH shells/background execution, resize, local tunnel data exchange, remote
listener allocation, and actual listener removal. Remote-listener removal uses
the original port-zero forwarding request; OpenSSH's successful process exit
alone did not establish successful removal when passed the allocated port.

The owner's authorized Ubuntu server passed a 983,040-byte SFTP round trip,
30,720-byte explicitly partial cancelled file, two shells, resize 80×24 → 120×40,
local-forward data, and local/remote listener removal. This transport check does
not establish that the design executable has integrated SSH or Hovel operations.

## Versions verified 2026-09-11

| Dependency | Current pin | Evidence |
| --- | --- | --- |
| Bubble Tea | 2.0.9 | [Latest stable release](https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.9) |
| Bubbles | 2.2.1 | [Latest stable release](https://github.com/charmbracelet/bubbles/releases/tag/v2.2.1) |
| Lip Gloss | 2.0.6 | [Latest stable release](https://github.com/charmbracelet/lipgloss/releases/tag/v2.0.6) |
| SFTP | 1.13.11 | [Release and remote-attribute allocation fix](https://github.com/pkg/sftp/releases/tag/v1.13.11) |
| x/crypto | 0.57.0 | [Official module metadata](https://proxy.golang.org/golang.org/x/crypto/@v/v0.57.0.info) |
| x/sys | 0.48.0 | [Official module metadata](https://proxy.golang.org/golang.org/x/sys/@v/v0.48.0.info) |
| x/vt | 20260906004030-3986e9119cf9 | [Newest inspected snapshot](https://github.com/charmbracelet/x/tree/3986e9119cf9/vt); no tagged stable release |
| colorprofile / x/ansi / x/term | 0.4.3 / 0.11.8 / 0.2.2 | Official Go module metadata; already current |
| Harmonica (Bubbles progress dependency) | 0.2.0 | [Official module metadata](https://proxy.golang.org/github.com/charmbracelet/harmonica/@v/v0.2.0.info); latest tagged release inspected |
| Hovel | 0.4.2 | [Latest published release](https://github.com/vibepwners/hovel/releases/tag/v0.4.2); original source/archive pin retained |

Exact module checksums and resolved transitive versions live in
`core/prototype_sdk/go.mod` and `go.sum`. Refresh this chosen set through
`aspect burrow-prototype deps`; it uses explicit versions, not floating build
dependencies. This audit covers direct runtime dependencies and Hovel, not a
claim that every unrelated documentation/build tool is the newest release.

Context7 MCP successfully resolved and queried the official Bubble Tea, Bubbles
and Lip Gloss libraries after the owner configured the local MCP server.
It supplied the [v2 view migration](https://github.com/charmbracelet/bubbletea/blob/v2.0.0/UPGRADE_GUIDE_V2.md),
[textinput style/key/cursor changes](https://github.com/charmbracelet/bubbles/blob/v2.0.0/UPGRADE_GUIDE_V2.md),
and current Lip Gloss table API. Context7's version index does not contain every
latest patch, so release endpoints and the downloaded pinned source verify the
exact patch versions and APIs. The optional ignored `context7.local.json` is an
owner-only credential file, not Hovel-style public library metadata or an
automatically loaded Codex configuration.

## Handoff and remaining limits

The first usable implementation should follow the owner's demonstration sequence
with actual Hovel-owned resources and public confirmation/audit contracts. Saved
profiles, credential prompts, L/R/SOCKS tunnel controls, robust transfer operations,
script/report workflows and AI/CLI acceptance remain required planning inputs;
they are not dropped because this visual walkthrough is narrower.

LazySSH's [main command flow](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/command_mode.py)
and [SCP flow](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/scp_mode.py)
use `list`, `open NAME`, named tunnels, separate transfer history and download/upload
contexts. The current proof still uses `connections`, `use`, `shell`, `back` and
a single simulated tunnel command. Exact final command vocabulary, persistent
history policy and unimplemented `tree`/`mget` behavior are not proven parity.
Earlier decisions intentionally replace unsafe host-trust bypass, legacy
exit-close behavior and external-terminal requirements.

The fixture copy timer still pauses while the blocking VT attachment owns the
terminal. Production transfers must execute independently of rendering. VT screen
bounds and redraw/compatibility limits in the prototype README still apply.
Full Hovel-backed first-use/setup, live UI integration and arbitrary terminal
compatibility are separate from these passing bounded checks.

The ticket stays open until the owner reviews the actual terminal's spacing,
color treatment, prompt/completion and workflow, and accepts this design reference.
