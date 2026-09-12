# Disposable command-first terminal prototype

This is the next review artifact for [Which terminal interaction model fits the agreed boundaries?](https://github.com/Bochner/burrow/issues/15).
It runs in a Linux/WSL terminal, not a browser. The real Go frontend uses a
contextual bottom prompt, typed modes, completion/history, Hovel colors and
readable file listings. All connections and tunnels are explicitly simulated.

## Run in WSL

```sh
cd /home/bochner/dev/burrow-ssh-specification
aspect burrow-prototype terminal
```

On another checkout, run the Aspect command from that repository root. A real
terminal is required; start at 80 columns or wider, then resize to try the compact
layout. `NO_COLOR=1` or `aspect burrow-prototype terminal -- --no-color` disables
colors. The terminal controls the background; no Nerd Font is required. Mouse
capture is off so normal terminal selection/copy remains available.

Try typing this sequence, using Tab completion and Up/Down history:

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
old tunnels. `tunnel 1080`, `tunnels`, and `run` demonstrate selection/context,
but open no sockets and execute no remote tools.

Files under remote-looking paths are ordinary temporary local files. `get` and
`put` actually copy bytes inside the fixture, refuse overwrites, and report
measured progress. The UI deliberately copies one 64 KiB chunk per 100 ms so the
progress is visible; this is demonstration pacing, not a transport benchmark.
`cancel` removes the incomplete destination. `lcd`/`lls` address local fixture
paths; downloads default to `/downloads`, uploads to the local working directory
(initially `/uploads`). Quoted filenames are accepted; input is not evaluated as
a shell command by management/file mode.

**Shells are real local `/bin/sh` processes, not SSH and not a security sandbox.**
Their working directory starts in the selected fixture. They run with your normal
user permissions. The header and shell prompt identify this throughout. Use plain
commands for this walkthrough: arbitrary full-screen applications are not supported.
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

This proof uses Hovel's pinned **Bubble Tea v1.3.10, Bubbles v1.0.0 and Lip Gloss
v1.1.1-0.20250404203927-76690c660834** from its
[pinned module](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/go.mod).
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
history keeps 100 commands. **This is not a VT terminal emulator**: cursor-addressed
screens, exact raw-input parity and arbitrary full-screen restoration remain the
[separate restoration decision](https://github.com/Bochner/burrow/issues/31).
The prompt shows known file-mode paths, not an inferred remote shell cwd.

The owner tested and accepted this command-first interaction on 2026-09-11,
with full color treatment and real functionality still to be developed. The
[terminal interaction ticket](https://github.com/Bochner/burrow/issues/15) records
the canonical resolution and carry-forward limits. The separate full-screen
restoration proof remains open. Preserve this bounded prototype on the shared
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
