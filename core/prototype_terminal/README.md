# Disposable command-first terminal prototype

Current status: owner accepted the base UI on 2026-09-12. Help now opens as a large centered overlay with a dimmed backdrop, isolated input focus and Esc restoration. See `docs/research/terminal-design.md` and `docs/research/terminal-mvp-coverage.md` for the accepted reference and remaining runtime work. Earlier walkthrough notes below retain historical context.

Current theme: **Dracula Classic**, researched against the [official spec](https://draculatheme.com/spec). This supersedes earlier Hovel/rose references below. Tables, help, prompt, dialogs, progress and the management background share the official palette. `--color` forces Dracula truecolor; `NO_COLOR` retains terminal defaults.


This is the owner-review artifact for [What initial terminal design should Burrow build on before milestone planning?](https://github.com/Bochner/burrow/issues/42).

The current candidate uses the newest verified stable Charm v2 family, a persistent
resource overview, Dracula Classic throughout, and the accepted command-first workflow.
See [current design, version provenance and walkthrough](../../docs/research/terminal-design.md).

The revised design follows LazySSH's stacked table order and adds a visible
Up/Down-selectable completion dropdown, required-first connection arguments,
F1/`help TOPIC`, structured file listings, and `mget PATTERN` → review → `confirm`
with overall/current-file progress, speed, elapsed time, ETA and per-file results.
See [full MVP coverage](../../docs/research/terminal-mvp-coverage.md) for the product
requirements and remaining runtime integration. This is still a local design proof.
The owner accepted this base UI on 2026-09-12; production integration remains subsequent work.
It runs in a Linux/WSL terminal, not a browser. The real Go frontend uses a
contextual bottom prompt, typed modes, completion/history, Hovel colors and
readable file listings. All connections and tunnels are explicitly simulated.

## Internal VT shell walkthrough

For [How should local shells restore full-screen terminal state?](https://github.com/Bochner/burrow/issues/31):

```sh
aspect burrow-prototype terminal -- --vt
aspect burrow-prototype vt-check
```

Type `connect`, then `shell`. Try a local full-screen program such as `vi` or
`top`, press **Ctrl-]**, open another `shell`, background it, and `resume 1`.
Resize while in management, then resume. `quit` ends the frontend's local shells.
This is the same local-only disposable fixture; no SSH/Hovel operations occur.
VT is now the default. `--vt=false` preserves the earlier plain-output baseline.

The candidate embeds `github.com/charmbracelet/x/vt` at
`v0.0.0-20260906004030-3986e9119cf9` ([source](https://github.com/charmbracelet/x/tree/3986e9119cf9/vt)).
Each shell continuously feeds its own VT screen while hidden. Bubble Tea's public
`Exec` handoff releases the terminal while the attachment handles raw bytes,
then restores management. Return redraws cells, styles and cursor rather than a
truncated byte tail. Supported input modes are mirrored; terminal replies return
to the PTY. The old 64 KiB tail remains only for baseline diagnostics/checks.

[Herdr comparison](../../docs/research/herdr-terminal-state.md) explains its
embedded Ghostty approach and Rust/Zig cost. This candidate adds no managed
runtime executable or new build toolchain. It does add six Go modules (VT,
Ultraviolet, ordered, termios, Windows helpers and x/sync), and raises six existing
module versions (ANSI, colorprofile, displaywidth, uax29, go-colorful, runewidth).
Exact versions and hashes are in the shared proof go.mod/go.sum. The current design revision upgrades Bubble Tea, Bubbles and Lip Gloss to v2;
see the version table in the current design reference. Upstream changes do not enter
a build until we deliberately update those pins and rerun the checks; pinning
does not remove upgrade/security maintenance. Library access stays in the local
shell screen code so an eventual replacement need not affect SSH operations.

The screen and executable checks cover fragmented UTF-8/CSI, main/alternate
buffers, styled render reconstruction, terminal cursor replies, bounded history,
independent shells, raw NUL, Ctrl-C, management switching, resize and exact termios
restoration. The existing shell behavior check also feeds background output beyond
64 KiB through a real PTY and verifies its current VT screen and history bound.

Limits: 320×120 maximum screen, 128 history lines, the library's 4 MiB control-data
parser bound, and full-screen redraw with 30 ms idle polling. These are not a total process
memory guarantee or a throughput benchmark. No claim of universal xterm, graphics,
kitty keyboard, arbitrary Unicode grapheme or full application compatibility.
Ctrl-] is reserved for management; cursor position/visibility are restored, but
cursor shape/color are not yet mirrored. Management's fixture transfer timer pauses
during the blocking shell handoff; real transfer execution must stay independent
of rendering during implementation. Clipboard/title forwarding is not enabled.
The shell mechanism and base UI are accepted; production acceptance remains separate.

An exploratory host-pinned tmux 3.6 comparison parsed fragmented screen state,
but its raw-NUL attachment check failed; no tmux parity claim was established.
It was not carried into the candidate or dependency graph after the owner expressed
an internal preference. That failure is a bounded fixture observation, not proof
that tmux cannot deliver the behavior with different integration.

## Run in WSL

```sh
cd /home/bochner/dev/burrow
aspect burrow-prototype terminal
```

On another checkout, run the Aspect command from that repository root. A real
terminal is required; start at 80 columns or wider, then resize to try the compact
layout. `NO_COLOR=1` or `aspect burrow-prototype terminal -- --no-color` disables
colors. The terminal controls the background; no Nerd Font is required. Mouse
capture is off so normal terminal selection/copy remains available.

Try typing this sequence, using Ctrl-N/P to choose suggestions, Tab to accept, and Up/Down history:

```text
connections
connect
scp
ls
cd /var/log
ls
get system.log
lls /downloads
back
shell
```

Inside the shell, type `sleep 2; echo finished`, then press **Ctrl-]** to return
to Burrow management. Type `sessions`, open another `shell`, background it, and
`resume 1`. The first command continues while backgrounded. Type `exit` to end
one shell; `quit` from management ends the frontend and all local fixture shells.
Use `close --yes` to explicitly end the selected simulated connection and its
shells/transfers. `loss` demonstrates disconnect; reconnect does not restore
old tunnels. `proxy 1080`, `tunnels`, and `run` demonstrate selection/context,
but open no sockets and execute no remote tools.

Files under remote-looking paths are ordinary temporary local files. `get` and
`put` actually copy bytes inside the fixture, refuse overwrites, and report
measured progress. The UI deliberately copies one 64 KiB chunk per 100 ms so the
progress is visible; this is demonstration pacing, not a transport benchmark.
`cancel` removes the incomplete destination. `lcd`/`lls` address the download directory, matching LazySSH; downloads default
to `/downloads`. Uploads use the separate `/uploads` fixture directory. Both
download and upload paths appear in the file-mode prompt. Quoted filenames are accepted; input is not evaluated as
a shell command by management/file mode.

**Shells are real local `/bin/sh` processes, not SSH and not a security sandbox.**
Their working directory starts in the selected fixture. They run with your normal
user permissions. The header and shell prompt identify this throughout. The default VT path supports the bounded full-screen walkthrough described above;
`--vt=false` supports plain output only.
All scratch files are removed when the prototype exits. Production daemon retention
is an already-decided contract; this throwaway fixture does not implement it.

## Scripting and checks

```sh
aspect burrow-prototype terminal -- --json connections
aspect burrow-prototype terminal -- --json --script /absolute/path/to/commands.txt
aspect burrow-prototype terminal-check
aspect burrow-check ci
```

The script file contains ordinary management/file commands, one per line; `-`
reads stdin. A script shares one ephemeral fixture, stops at the first failed
command, and exits nonzero. The executable emits one JSON result per command;
Aspect adds its own build/run diagnostics outside the executable. The declared
CLI check invokes the built executable directly and validates clean JSON stdout,
actual transfer results, nonzero failure status and no-TTY refusal. Interactive
shell input is intentionally a separate TTY path, not a headless command replay.

The Go behavior check covers byte equality, overwrite refusal, cancellation,
path/symlink containment, quoted arguments, explicit reconnect, completion/history,
no-color/narrow views, multiple real local PTYs, background execution, bounded
output, resize on resume and connection-wide shell cleanup. A real PTY launch was
also exercised through the same Aspect command, including shell input, background/
resume, transfer progress and terminal restoration on quit. These checks do not
prove remote SSH, Hovel lifecycle/confirmation/audit, or arbitrary VT restoration.

## Component proposal and limits

This revision pins **Bubble Tea v2.0.9, Bubbles v2.2.1 and Lip Gloss v2.0.6**.
The owner selected newest stable dependencies unless they conflict with Hovel.
Hovel's daemon/API and public module SDK do not require its frontend's Charm v1
versions in Burrow. See the current design reference for verified sources.
Dependencies and transitive checksums are recorded in the existing shared proof
manifest `../prototype_sdk/go.mod` and `go.sum`; this is not a new production SDK.
The frontend imports public Charm packages, not Hovel internals. Styling follows
[Hovel clistyle](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/clistyle/styles.go).

Hovel itself uses go-prompt for command entry. This candidate uses Bubbles
textinput inside a single Bubble Tea event loop so one renderer owns command
entry, asynchronous progress, resizing and shell switching. That is a tested
candidate for owner feedback, not an automatic production-library decision.
Command behavior stays in `commands.go`, separate from terminal rendering.

The Linux PTY mechanism follows the earlier `prototype_transport/local_terminal.py`
proof. Shell output retains at most 64 KiB per shell and strips terminal control
sequences for plain-output display. Management keeps 400 lines and in-memory
history keeps 100 commands. **The optional plain-output baseline is not a VT terminal emulator**: cursor-addressed
screens, exact raw-input parity and arbitrary full-screen restoration remain the
[separate restoration decision](https://github.com/Bochner/burrow/issues/31).
The prompt shows known file-mode paths, not an inferred remote shell cwd.

The owner tested and accepted this command-first interaction on 2026-09-11,
with full color treatment and real functionality still to be developed. The
[terminal interaction ticket](https://github.com/Bochner/burrow/issues/15) records
the canonical resolution and carry-forward limits. The full-screen restoration mechanism was subsequently accepted; the current visual
revision remains open in the terminal-design decision. Preserve this bounded prototype on the shared
milestone branch until its next validated consolidation.

# Earlier browser sketch

For [Which terminal interaction model fits the agreed boundaries?](https://github.com/Bochner/burrow/issues/15).
The owner’s latest direction is a command-first interactive CLI. This earlier
panel-and-button sketch illustrates resource states; it is not the literal
production interface. Hovel colors, color plus text status, selected-target
highlighting and truthful progress remain approved. Preserve a connection-specific
transfer prompt with live filesystem navigation, alongside on-demand shells.
Clickable views may offer shortcuts, but every action must remain available
through the CLI, including noninteractive scripting. The owner prefers the
LazySSH/Meterpreter/Evil-WinRM prompt-and-mode base with restrained Charm
enhancements; a Midnight Commander-style workspace is too heavy.

The owner accepted this initial look and requested Hovel’s color theme. Dark-mode
accents, borders, text and status colors follow [Hovel clistyle](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/clistyle/styles.go):
cyan and magenta with semantic green/yellow/red. Browser background surfaces and
darker light-mode variants are adaptations; Hovel leaves terminal backgrounds
to the terminal.

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

The browser sketch is preserved as earlier evidence; review the real terminal
prototype above for the current command-first direction.
Keep this asset on the shared SSH-specification milestone branch until its next
validated consolidation. Approval of the direction is not acceptance of an
unimplemented frontend.


Current workspace controls: **Ctrl+K** opens the command menu, **Ctrl+C** opens
quit confirmation (Keep working is the default), **F1** opens help, and
**Ctrl+Up/Down** scrolls overflowing resource tables. **Up/Down** selects prompt
suggestions and **Tab** advances through command arguments. The sidebar appears
at 110 columns; smaller terminals retain the prompt and compact resource context.

The Aspect launcher now renders through `/dev/tty` when stdout is captured so
terminal size and color detection work. `NO_COLOR` remains respected; explicitly
preview Dracula truecolor with `aspect burrow-prototype terminal -- --color`.

Help follows LazySSH's semantic highlighting: rose commands/headings, cyan flags,
yellow arguments, muted optional brackets, and green examples. Try `help connect`
for required/optional parameters; arrows scroll and Tab/Shift+Tab cycles topics.
The pink accent is softened to `#b84b8a`. SOCKS appears in the connection's PROXY
column, while the tunnel table contains local/reverse forwards only.
