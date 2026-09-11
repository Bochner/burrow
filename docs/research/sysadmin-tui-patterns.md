# Sysadmin TUI interaction patterns

Research date: 2026-09-11. Status: evidence and recommendations for the terminal
interaction decision; this is not an approved implementation specification.

## Question and scope

Which established terminal tools offer useful patterns for Burrow's command-first
SSH workflow, optional clickable views, live file management, and scripting?

This is a focused comparison of eight tools, not a measured popularity ranking.
The sources establish their capabilities, not what percentage of sysadmins use
them. LazyGit and K9s are the owner's requested reference points; file managers,
resource monitors, data explorers and pipeline tools broaden the comparison.
Documentation was inspected on the date above; moving documentation URLs are
research provenance, not dependency pins. No new dependency is selected here.

## Comparison

| Tool / useful context | Keyboard and command interaction | Mouse and visual interaction | Automation boundary | Burrow lesson (recommendation) |
| --- | --- | --- | --- | --- |
| **LazyGit** — Git inspection and changes | Contextual panel shortcuts, `/` filtering, and custom shell commands bound to keys. | Configurable mouse events, theme and selected-panel styling. | Custom commands extend an interactive Git frontend. They do not by themselves establish a general headless LazyGit API; Git remains the underlying command-line tool. | Borrow focused previews, visible selection and contextual help. A panel-first default is a weaker fit for the owner's persistent-prompt preference. Sources: [project](https://github.com/jesseduffield/lazygit), [configuration](https://github.com/jesseduffield/lazygit/blob/master/docs/Config.md), [custom commands](https://github.com/jesseduffield/lazygit/blob/master/docs/Custom_Command_Keybindings.md). |
| **K9s** — Kubernetes operations | Typed `:pod`, `:ctx`, namespace/context arguments, `/` filtering, `?` help and operation shortcuts. | Mouse is explicitly optional and disabled by default; context skins and icon suppression are configurable. | Launch arguments select context/view. Plugins execute external commands with selected-resource context; this is not proof that every action has a headless K9s command. Its `headless` UI setting means **hide the header**, not batch execution. | Strong model for visible target/context, fast named navigation and an optional mouse. Keep Burrow's operation CLI explicit. Sources: [commands](https://k9scli.io/topics/commands/), [configuration](https://k9scli.io/topics/config/), [plugins](https://k9scli.io/topics/plugins/). |
| **Midnight Commander** — file operations | A shell command line coexists with two file panels; normal typing enters a command. Function keys operate files; Ctrl-O switches command screen/panels when supported. | Mouse selects files and activates function labels; `--nomouse` disables it. | Shell integration and external filesystem scripts support extension, but the inspected manual does not establish full headless parity for panel actions. SFTP VFS supports remote navigation and manipulation. | Closest file-mode precedent: maintain a working prompt beside optional local/remote views. Explicitly label which filesystem commands address. Source: [upstream manual](https://raw.githubusercontent.com/MidnightCommander/mc/master/doc/man/mc.1.in). |
| **Yazi** — modern file navigation | Keyboard navigation, search/filter, file operations, `;`/`:` shell commands, and a documented shell escape. | Mouse events are configurable; previews provide detail on demand. | Shell wrapper returns the chosen working directory. `ya emit`/`emit-to` sends actions to an existing instance; DDS reports events. Instance control is not equivalent to a standalone transfer API. | Borrow quick navigation, previews and smooth shell return; retain explicit CLI transfers. Sources: [quick start](https://yazi-rs.github.io/docs/quick-start/), [shell escape](https://yazi-rs.github.io/docs/tips/), [mouse configuration](https://yazi-rs.github.io/docs/configuration/yazi/), [DDS](https://yazi-rs.github.io/docs/dds/). |
| **btop** — resource/process observation | Arrow-key selection, filtering, sorting, process details and signal actions. | Highlighted-key buttons are also clickable. Supports themes, selectable graph symbols, 256-color conversion and 16-color TTY mode. | The inspected feature/usage documentation describes an interactive monitor; do not infer a stable headless resource API from its launch options. | Good model for compact status, readable progress and matching click/keyboard affordances; not Burrow's overall interaction model. Source: [upstream project](https://github.com/aristocratos/btop). |
| **VisiData** — inspecting and transforming tabular data | Commands have descriptive longnames separate from keybindings; commands and help can be inspected. | Mouse can be disabled with the named `mouse-disable` command or configuration. | Saved command logs replay with `vd -p`; `-b` runs noninteractively and `-o` writes results. Logs contain sheet/row/column context, so reusing them with different data may require adjustment. | Strong evidence that rich interactive operations can have named, repeatable commands. Prefer explicit target/path arguments over replaying cursor positions. Sources: [commands](https://www.visidata.org/docs/api/commands), [mouse](https://www.visidata.org/docs/mouse/), [replay and limits](https://www.visidata.org/docs/save-restore/). |
| **fzf** — selecting shell data | Takes candidates on stdin, performs interactive selection, returns selection on stdout; supports shell integration. | `--height` embeds the picker below the cursor; preview supplies optional detail. | `--filter` performs noninteractive matching. It composes with surrounding commands rather than requiring ownership of the complete workflow. | Use optional pickers without displacing the command prompt or making selection the only way to name a target. Sources: [getting started](https://junegunn.github.io/fzf/getting-started/), [reference](https://junegunn.github.io/fzf/reference/). |
| **lnav** — log exploration | Separate command, search and SQL prompts; contextual help and optional panels. | Status bars and clickable controls coexist with keyboard/command alternatives, such as reload via click, F5 or `:reload-view`. | `-n` runs without the UI; repeated `-c` and `-f` execute commands/queries and command files; `:write-json-to` exports SQL results. | Particularly strong combined example of readable terminal views, named commands and real headless execution. Sources: [UI](https://docs.lnav.org/en/latest/ui.html), [CLI](https://docs.lnav.org/en/latest/cli.html), [commands](https://docs.lnav.org/en/latest/commands.html). |

## What the evidence supports

Keyboard-driven, command-driven and scriptable describe different capabilities.
LazyGit-style shortcuts demonstrate rapid interactive control. K9s-style typed
navigation adds a discoverable command vocabulary. Yazi's instance messaging
controls a running UI. VisiData replay and lnav's noninteractive command execution
go further: documented operations run without a person driving a screen.
These distinctions follow from the interfaces cited above, rather than an
assumption that every terminal program provides all four capabilities.

Mouse support is compatible with a terminal workflow: K9s makes it opt-in,
Midnight Commander can disable it, and btop pairs clickable controls with visible
key labels. None of that evidence demonstrates that a mouse is required for
Burrow. The owner's requirement is stronger: every meaningful operation must
also be available through a CLI for people and scripts.

File management offers an especially relevant bridge. Midnight Commander's
documented layout combines shell input and file panels, and its SFTP filesystem
supports remote browsing. Yazi demonstrates shell return and contextual previews.
Together they support exploring an optional rich file mode while retaining
the live command-based navigation and transfer experience required from LazySSH.
Their features do not establish Burrow parity; that still requires the existing
[LazySSH source-backed inventory](lazyssh-parity.md).

## Owner refinement after reviewing the findings

The owner explicitly considers Midnight Commander too heavy for Burrow. The
interaction base should resemble LazySSH, Meterpreter and Evil-WinRM: a typed
prompt and focused command modes, with restrained Charm/TUI enhancements.
Treat the comparison as a source of individual affordances, not a menu of whole
interfaces to reproduce. In particular, do not make a dual-pane file workspace
or permanent dashboard the default. Optional clickable controls remain welcome,
with CLI access to every operation and support for scripting. This is an
interaction analogy, not Meterpreter protocol compatibility or a new execution
scope.

## Recommendations for the next Burrow walkthrough

1. **Make the prompt the starting point.** Keep command entry, completion,
   history and readable command output primary. Rich target/status views support
   it. The existing browser prototype is an interaction sketch, not a mandate
   for a dashboard-first terminal application.
2. **Share operations across prompt, CLI and optional UI actions.** A click or
   shortcut invokes the same operation and confirmation policy as a named
   command. Display the equivalent command where it helps learning. UI-only
   actions such as resizing a panel should remain keyboard-accessible, but need
   no standalone operation API.
3. **Prove scripting separately.** Run representative connection, listing,
   transfer and lifecycle operations without a TTY, with explicit target
   arguments, machine-readable results and meaningful exit status. Automation
   must not depend on a focused panel, current row or fake keystrokes. Hovel's
   confirmation/audit contracts remain applicable.
4. **Preserve a command-based file mode.** Show local and remote paths in the prompt/output, support
   typed navigation/transfers and small optional previews; avoid a permanent
   dual-pane file manager. Distinguish
   management input, file-mode input and the actual remote shell; never make
   arbitrary typing ambiguous between local and remote execution.
5. **Retain the approved visual cues.** Hovel colors, connection-state labels
   and active-target outline are useful. Pair state colors with text, maintain
   a visible keyboard focus, and permit ordinary terminal text selection. Add
   plain/low-color rendering as a compatibility requirement; MC's `--nocolor`
   and btop's TTY modes provide precedents, not proof of screen-reader support.
6. **Test the middle ground on one complete workflow.** From the prompt,
   select a target, connect, enter the real shell, return, browse remote files,
   transfer a file and detach. Repeat applicable operations from an ordinary
   shell script, then exercise the optional clicks. That reveals whether the
   views help without becoming mandatory navigation.

These are recommendations for discussion. They neither select a new Go prompt
library nor resolve the existing terminal prototype decision. Hovel component
reuse and the prompt-toolkit comparison are covered by the companion
[command-first interaction research](command-first-interaction.md).
