# Command-first interaction evidence

Research date: 2026-09-11. Inspected Hovel at
`c461ba282a8aecc7aa3a079a4613bf5e2640c388` and LazySSH at
`9eb84452c31cb527bf8e938e23ffc92974fb91cb`. Source/test review only; no upstream
tests or remote operations were run. These findings support the terminal
interaction discussion; they do not select a production frontend library.

## Hovel facts

Hovel already uses `github.com/c-bata/go-prompt v0.2.6` for its interactive CLI.
Its `App.Prompt` constructs a `prompt.Prompt`, passes command execution to
`ExecuteLine`, and supplies a context-sensitive completer and live prompt
prefix. This is the concrete Go prompt library relevant to the owner's
prompt-toolkit comparison; it is separate from Charm's Bubble Tea components,
which also appear in Hovel's dependencies. Matching Hovel does not require
making the default interaction a full-screen dashboard.
[Dependency pin][hmod], [actual prompt construction][hprompt].

The prompt explicitly uses fuchsia and turquoise for its prefix, input,
selection, and scrollbar. Completion routes through the command registry and
contextual suggestions, including an interactive configuration mode. Submitted
`exit` and `quit` terminate the prompt. This constructor configures neither
custom key bindings nor persistent command history; do not infer a durable
history contract from the dependency alone. Session output history elsewhere
in Hovel is a different feature.
[Prompt options][hprompt], [completion entry][hcomplete],
[exit checker][hexit].

Hovel has a prompt surface for asynchronous logs and tests covering prompt
refresh below a log, command-active logging, and activity animation. This
provides source evidence for a styled command prompt with live feedback;
it is internal frontend code, not an SDK component Burrow may import.
[Prompt surface][hsurface], [surface tests][hsurfacetests].

## LazySSH facts

The main command registry exposes `scp`; `cmd_scp` accepts an optional named
connection, validates it, creates `SCPMode`, and resumes the main prompt after
that mode returns. Without a name, SCP mode offers a connection chooser. Its
prompt includes the selected connection, remote directory, and local upload
and download directories. The existing SSH control socket is reused for
remote navigation commands.
[Entry and return][lentry], [mode loop and prompt][lloop].

The transfer prompt supports remote `ls`, `tree`, `cd`, `pwd`; local `local`,
`lls`, `lcd`; and `get`, `put`, `mget`, plus help and exit. It uses Python
`prompt_toolkit.PromptSession`, a dedicated `FileHistory` at
`/tmp/lazyssh/scp_history`, and completion while typing. Command completion
dispatches to remote-file, remote-directory and local-path handlers; remote
completion uses cached SSH listings and throttles typing-triggered requests,
while explicit Tab bypasses the throttle. These are command-driven filesystem
interactions, not a requirement for a graphical file browser.
[Command registry and history][lsetup], [completion dispatch][lcomplete],
[throttling and SSH execution][lthrottle].

In the SCP prompt, Ctrl+C continues the loop, EOF leaves it, and `exit` returns
to management. The main prompt also has its own file history and completer.
Those are observed legacy behaviors, not approval to carry forward every
signal or history-storage policy.
[SCP loop][lloop], [main prompt][lmain].

Tests exercise SCP connection selection/validation, command/path completion,
local navigation, cache expiry and throttling; main-command tests check SCP
entry and connection-name completion. The interactive SCP loop is marked
`pragma: no cover`, so these tests do not prove a real terminal workflow or
network behavior. No upstream suite was executed for this note.
[SCP tests][ltests], [main command tests][lmaintests], [loop][lloop].

## Owner direction

The owner clarified during this discussion that optional clickable and
interactive GUI affordances are welcome, using Herdr as an analogy, provided
every action also has a CLI path, including scripting. Keep the approved Hovel
colors, connected/disconnected indicators and selected-target emphasis.
This is owner input, not a conclusion drawn from upstream source; Herdr was
not researched for this note.

## Recommendation for the next walkthrough

Make the typed command prompt the primary surface. Preserve the dedicated
connection-scoped transfer mode and obvious return to management. Keep the
approved Hovel colors, textual connection status and selected-target emphasis
as supporting context; expose tables/progress through command output or
optional interactive views backed by the same actions. Demonstrate the whole
workflow through typed commands and a script before choosing
the frontend library. Treat completion, history policy, async output and
terminal handoff as explicit behavior to prove, rather than capabilities
automatically supplied by a library name.

[hmod]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/go.mod
[hprompt]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/cli/cli.go#L347-L377
[hcomplete]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/cli/cli.go#L593-L620
[hexit]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/cli/cli.go#L2345-L2352
[hsurface]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/cli/prompt_writer.go
[hsurfacetests]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/cli/prompt_writer_test.go
[lentry]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/command_mode.py#L1446-L1465
[lloop]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/scp_mode.py#L790-L843
[lsetup]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/scp_mode.py#L478-L531
[lcomplete]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/scp_mode.py#L126-L164
[lthrottle]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/scp_mode.py#L746-L788
[lmain]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/command_mode.py#L344-L356
[ltests]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_scp_mode.py
[lmaintests]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_command_mode.py
