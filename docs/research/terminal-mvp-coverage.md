# Full terminal MVP — workflow and design coverage

Owner direction: the MVP is a complete daily-use terminal application with useful
LazySSH parity, rebuilt and improved with Charm. A connection picker, bare prompt,
technical proof, or attractive browser mockup does not satisfy that requirement.
This is the design input for [the initial terminal design decision](https://github.com/Bochner/burrow/issues/42),
not a claim that the production application has shipped.

## Source and visual baseline

Reinspected LazySSH at
[`9eb84452c31cb527bf8e938e23ffc92974fb91cb`](https://github.com/Bochner/lazyssh/tree/9eb84452c31cb527bf8e938e23ffc92974fb91cb):
`command_mode.py` command dispatch, `show_status`, `cmd_help`, `_help_overview`;
`ui.py` saved configurations, active connections, per-connection tunnels and
standard tables; `scp_mode.py` dispatch, completion, progress factories and all
four `mget` stages. The earlier browser sketch is preserved at
`core/prototype_terminal/index.html`; it supplies color/interaction reference,
not a literal panel-first replacement for the accepted command-first interface.

The main screen follows this vertical reading order:

1. Burrow/workspace identity and current operating context.
2. Saved connection configurations: name, host, user, port, key/authentication,
   shell, proxy and headless settings. Optional settings follow required ones.
3. Active SSH connections: connection identity and endpoint, state, shell and
   tunnel counts. Saved settings never masquerade as live access.
4. Tunnels grouped by connection: qualified ID, L/R type, listening endpoint,
   destination and actual lifecycle. Removal targets an unambiguous resource.
5. Background shell/run summaries and generous command output.
6. Persistent transfer activity when present, contextual help/completion, and the
   editable prompt at the bottom.

Content-sized tables replace the rejected equal-width resource boxes. Dracula
Classic supplies purple headings, pink commands/hosts, cyan flags/focus, green
success/users/examples, orange ports/sizes, yellow arguments and red errors.
Grouping, labels and selection remain readable without color. File mode keeps a
compact resource summary. Help opens over the unchanged workspace in a large,
centered, dimmed overlay; it does not replace the output pane or sidebar.
Narrow layouts preserve the prompt, scrolling and explicit overflow handling.

## Prompt contract

Charm Bubbles textinput supplies editing and suggestion selection inside Bubble
Tea; progress, spinner, help and viewport supply their respective UI components.
Static resource tables use Lip Gloss tables; selectable table widgets belong where
rows actually own focus. Hovel currently uses `github.com/c-bata/go-prompt`; Burrow reproduces the
useful interaction with one Charm input owner rather than installing competing
terminal input loops. Context7 was consulted for the pinned v2 textinput and
progress APIs.

Required arguments are presented first, in LazySSH order:

```text
connect -ip HOST -port PORT -user USER -socket NAME
        [-proxy PORT] [-ssh-key PATH] [-shell SHELL] [-no-term]
```

The final canonical command name remains a design choice; the ordered fields and
required/optional distinction are owner requirements. Completion guides the next
missing required field before offering optional fields. Validation identifies the
first missing required field. Explicitly typed valid options may be reordered;
presentation order is not a reason to reject ordinary CLI syntax.

- A visible dropdown shows suggestions and descriptions, with a selected row.
- Up/Down selects; Tab accepts; Enter submits; Escape dismisses the dropdown.
- When completion is closed, Up/Down recalls history and restores the edited draft.
- Completion is contextual to command, argument, connection/profile, remote path
  or local upload/download root. Slow remote discovery cannot block typing.
- Required arguments, optional switches and example values are distinguished.
- Quoting and Unicode paths work consistently; bracketed paste cannot execute
  embedded newlines. Secrets are excluded from persisted history and diagnostics.
- Main and SCP histories retain their separate contexts. F1 and `help COMMAND`
  show readable syntax, descriptions, examples and relevant keybindings. Help
  closes back to the same command draft and selection.

## MVP acceptance inventory

All rows below are required production outcomes unless an earlier owner decision
explicitly replaces the legacy mechanism. The prototype column records evidence,
not completion credit for the runtime.

| Workflow | Required MVP outcome | Current design/proof coverage |
| --- | --- | --- |
| Startup and setup | Transparent pinned Hovel setup, workspace selection, actionable dependency/authentication failures, useful initial screen | Separate setup proofs; visual fixture |
| Connection creation | Required-first arguments and guided entry, aliases/jumps, passwords, encrypted keys and agent, verified host keys, explicit confirmation/cancellation | Ordered prompt and simulated creation; separate real authentication proofs |
| Saved configurations | List, create/save, select/connect, edit, delete and backup settings; never auto-connect on load | Sample configuration table; persistence and guided forms still need runtime implementation |
| Active connections | Multiple named transports, truthful connecting/connected/lost states, detailed inspection and explicit reconnect | Simulated state plus separate real ownership/loss proofs |
| Shells | Open, background, list, resume, resize, full-screen programs, terminal restoration; shell exit retains connection | Real local PTY/VT design and separate real SSH shell tests |
| Tunnels | L/R forward creation; connection-owned SOCKS proxy, bind/destination details, per-connection grouping, unique IDs, conflict handling and verified removal | Simulated L/R forward creation; connection-owned SOCKS proxy and qualified-ID removal; separate full forwarding proofs |
| Close and quit | Review exact teardown consequences; retain daemon connections/tunnels on quit, end frontend-local shells; report uncertain cleanup | Existing lifecycle proofs; production dialogs and live UI integration outstanding |
| SCP entry | Choose a live connection or name it, reuse authentication, contextual prompt, return to management | Fixture command flow; real subsystem-pipe proof |
| Remote navigation | `ls`, `tree`, `cd`, `pwd`, complete metadata, hidden files, Unicode/spaces, symlinks, denied paths, useful cached completion | Structured local fixture listing/tree; full remote UI pending |
| Local navigation | `local`, `lcd`, `lls`, clear separate upload/download directories and contained upload selection | Local fixture behavior; workspace-backed runtime pending |
| Single transfer | `get`/`put`, destination preview, overwrite handling, measured bytes, speed, elapsed/remaining time, final result | Real local copying and separate live remote round-trip |
| Batch discovery | Nonrecursive `mget` pattern, exact file list and total size, destination, confirmation, explicit zero-match/error state | Runnable fixture review/confirm/cancel flow |
| Batch execution | Overall and current-file bars; counts, bytes, speed, elapsed time, ETA; queue remains inspectable | Charm progress view over measured local bytes; separate large live collection check |
| Batch completion | Per-file complete/failed/cancelled results; successful bytes/count/time; failures never become a green total; durable retrievable summary | Fixture partial-result/overwrite/cancel checks; production evidence retention pending |
| Background work | Input remains responsive during slow I/O; transfers continue while browsing, reading help, or attached to a shell | Commands run outside Update; blocking VT attachment still pauses fixture copying and must be removed in production |
| Help and prompt | Categorized help, `help COMMAND`, argument order, dropdown selection, history, quoting, diagnostics/debug and completion | Runnable help/dropdown; full command catalog and persistent contextual histories pending |
| Logs and evidence | Per-workspace/connection transfer totals and logs, collected files and reports, redacted credentials, retained failure details | Existing Hovel collection proofs; operational UI pending |
| Scripts and AI operations | Accepted Hovel-backed remote scripts/commands, local automation/tunnel consumers, report viewing and CLI-oriented agent skills | Separate accepted proofs; end-to-end product workflows pending |
| Accessibility and terminal capability | Keyboard-only use, plain/no-color, reduced animation, narrow resize, readable labels, safe remote text and restored terminal | Bounded layout/input/PTY checks; final supported-terminal acceptance pending |

Existing deliberate differences remain: verified host keys, daemon retention on
quit, frontend-local shell lifetime, Hovel execution/skills instead of unchanged
LazySSH plugins, and no legacy imports or Terminator requirement. “Everything
LazySSH does” preserves useful outcomes and polish; it does not reinstate removed
unsafe behavior or previously rejected compatibility mechanisms.

## Transfer details that must survive implementation

`mget` has four visible stages: discovery, review, copying and completion. Review
shows all matched regular files, known/unknown sizes and the frozen destination.
No bytes are written before confirmation. Pattern matching does not invoke a
remote shell. Filesystem changes after review must be reconciled explicitly.

Copying shows two levels of progress: the batch and the current file. Speed must
be measured from byte/time samples, with averaging labeled; elapsed time uses a
monotonic clock. ETA is unknown before a useful measurement and for unknown
totals. A stalled transfer must not retain an apparently healthy instantaneous
rate indefinitely. Zero-byte files and already-existing destinations have clear
outcomes. Failure, cancellation, overwrite and retry policies must preserve data.

Successful files remain available after a later failure. A partially completed
batch is labeled PARTIAL, not COMPLETE. Cancellation distinguishes stopped waiting,
stopped transport, retained partial bytes and completed files; it never invents
rollback. The fixture deletes its own partial scratch file; this is not a decision
to delete production partial evidence. Final summaries remain retrievable after
the bars disappear and after returning to the resource overview.

## Live evidence

The authorized server contained a 4,439,457,792-byte ISO. A read-only collection
of that file and the 400-byte target of `/etc/os-release` passed with both SHA-256
hashes matching: 4,439,458,192 bytes total, 51.1 seconds including verification,
roughly 110.8 MiB/s during the large transfer. Original remote files were left in
place; temporary local downloads, test shells, forwards, master and remote scratch
were removed. The symlink exposed and fixed a metadata check: measure the target
size, not the link-text length.

Reproduce with existing trusted host keys and terminal authentication:

```sh
aspect burrow-prototype live -- HOST USER /absolute/large-file /absolute/second-file
```

This check uses OpenSSH's SFTP-mode `scp` for the owner-selected files, plus the
existing Go SFTP round-trip proof. It verifies real transfer measurements and
cleanup, not an integrated production Charm-to-Hovel collection workflow.

The owner accepted the base UI on 2026-09-12. The resolution of issue #42 links the preserved source; #16 carries the
implementation sequence. A full-feature MVP remains
the implementation target; fixture/proof coverage does not complete runtime work.

Owner refinement: PROXY is an active-connection column (`No` / `Yes :PORT`); SOCKS does not consume tunnel IDs or counts. The accepted palette is Dracula Classic.
