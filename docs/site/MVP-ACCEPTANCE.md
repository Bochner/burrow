# Daily-use release verification — #65

Candidate work starts at `c00563df028385c4932c9510871fc630548e2090` on
`mvp5`. This record distinguishes implemented behavior, executable evidence,
and owner acceptance. **The complete MVP is not yet accepted.** Final evidence
must identify the candidate commit; earlier ticket checks are useful history,
not a substitute for that run or the human walkthrough.

## Source-backed LazySSH mapping

The baseline is LazySSH
[`9eb84452c31cb527bf8e938e23ffc92974fb91cb`](https://github.com/Bochner/lazyssh/tree/9eb84452c31cb527bf8e938e23ffc92974fb91cb).
Paths in the upstream column are relative to `src/lazyssh/`; their corresponding
`tests/test_*.py` files were inspected alongside the implementation. This is a
source review, not execution of LazySSH's tests or engagement plugins. Every row
of `docs/research/lazyssh-parity.md` is retained below. Earlier research proposals
about verified host keys, imports or frontend-local shells are superseded by
the owner decisions named here.

Production test targets below are under `//core/cmd/burrow:` unless qualified.
`ssh_*_test` targets exercise real disposable OpenSSH servers and the shipped
binary. `interaction_test` exercises the production terminal model; it is not a
live-server test. Historical `prototype_*` targets never earn completion credit.

| Inventory row | LazySSH implementation and tests | Burrow production outcome / selected evidence |
| --- | --- | --- |
| Startup | `__main__.py`, `test_main.py`, `test_init.py` | Pinned setup, explicit workspace, offline reuse and actionable refusal: `setup_test`, `workspace_test`, `//core/launch:online_test`. |
| Connection creation | `ssh.py:create_connection`, `command_mode.py:cmd_lazyssh`, `test_ssh.py`, `test_command_mode.py` | Required-first guided entry and one exact recap; independent named transports: `ssh_lifecycle_test` (`connection_lab.py`, `authentication_lab.py`). |
| Authentication | `ssh.py:create_connection`, `test_ssh.py` optional-argument cases | Real default/explicit keys, agent, password, passphrase, alias/jump and cancellation: `ssh_lifecycle_test` (`authentication_lab.py`). Legacy mocks alone do not prove authentication. |
| Host identity | `ssh.py:create_connection`, `test_ssh.py` | ADR 0001 explicitly retains `StrictHostKeyChecking=no` and `UserKnownHostsFile=/dev/null`; no retained host-trust claim. Production configuration and authentication checks cover the accepted policy. |
| Connection state | `models.py`, `ssh.py:list_connections`, `test_models.py`, `test_ssh.py` | Daemon-owned manager observations, distinct creation identities, loss and explicit reconnect: `ssh_lifecycle_test` (`manager_lab.py`, `workspace_lab.py`). |
| Shells | `ssh.py:open_terminal_native`, `test_ssh.py` native-terminal cases | Retained shared SSH shells, independent attachments, resize, exit/reopen and restoration: `ssh_shell_test` (`sessions_lab.py`, `shell_lab.py`). ADR 0002 replaces frontend-local lifetime. |
| Terminal choice | `ssh.py:open_terminal`, `command_mode.py:cmd_terminal`, `test_ssh.py` selection cases | Managed Charm tabs and explicit headless connections; Terminator compatibility intentionally omitted by #7/#16. Real two-shell Vim/less/top checks: `ssh_shell_test`. |
| SOCKS | `ssh.py:create_connection`, `test_ssh.py` proxy arguments | Connection-owned SOCKS, actual traffic, conflicts and removal: `ssh_lifecycle_test`. No separate tunnel ID is invented for the proxy. |
| Local forwarding | `ssh.py:create_tunnel`, `test_ssh.py` forward cases | Reviewed qualified listener, traffic and selected removal: `ssh_shell_test` (`forward_lab.py`). |
| Remote forwarding | `ssh.py:create_tunnel`, `test_ssh.py` reverse cases | Explicit destination, random high port, actual traffic and occupied-port refusal: `ssh_reverse_test` (`forward_lab.py`). |
| Tunnel removal | `ssh.py:close_tunnel`, `command_mode.py:cmd_tund`, `test_ssh.py` | Connection-qualified identity fixes legacy ambiguous integer IDs; sibling preservation: shell/reverse partitions. |
| Connection closure | `ssh.py:close_connection`, `test_ssh.py` close cases | Reviewed exact creation, dependent-resource cleanup and truthful uncertainty: lifecycle/files/shell partitions. |
| Exit | `command_mode.py:cmd_exit` and prompt signal handling, `test_command_mode.py` | Keep running / Close reviewed connections / Cancel; daemon retained, shared claims released on Keep: lifecycle/shell partitions and `terminal_test`. ADRs 0001/0002 supersede legacy lifetime. |
| Saved profiles | `config.py`, `command_mode.py:cmd_connect/cmd_save_config/cmd_delete_config/cmd_backup_config`, `test_config.py`, `test_command_mode.py` | Load-only JSON collections, CRUD, save/reconnect, revisions and backup: `profile_test`, lifecycle partition. Legacy import is explicitly out of scope (#7/#16). |
| Transfer entry | `command_mode.py:cmd_scp`, `scp_mode.py:connect`, associated tests | Named live connection, reused master, return without reconnecting: files partition. |
| Remote navigation | `scp_mode.py:cmd_ls/cmd_tree/cmd_cd/cmd_pwd`, `test_scp_mode.py` | Structured SFTP, complete metadata, Unicode/spaces, links, denied paths, reduced metadata and bounded discovery: files partition (`files_lab.py`). |
| Local navigation | `scp_mode.py:cmd_local/cmd_lcd/cmd_lls`, `test_scp_mode.py` | Separate selected upload/download roots, contained selection, history and completion: `files_test`, files partition. |
| Download/upload | `scp_mode.py:cmd_get/cmd_put`, `test_scp_mode.py` | Reviewed byte-exact transfer, overwrite, measured progress, cancellation, labelled partials and durable evidence: files partition. Upload containment intentionally replaces permissive outside-path copying (#33/#55). |
| Batch download | `scp_mode.py:cmd_mget`, `test_scp_mode.py` | Nonrecursive discovery, frozen exact review, per-file/overall progress, zero matches and partial results: files partition. Patterns never become a shell command. |
| Prompt UX | `command_mode.py` completion/dispatch/help, `scp_mode.py` prompt, corresponding tests | Quoting, required-first suggestions, separate history, contextual help and keyboard navigation: `interaction_test`, `terminal_test`, lifecycle/files partitions. |
| Visual/accessibility | `console_instance.py`, `ui.py`, `test_plain_text_mode.py`, `test_animation_disable.py`, `test_ui_env_vars.py`, `test_refresh_rate_bounds.py` | Catppuccin Mocha (#67), no-color/reduced-animation, resize, text labels and safe management text: `interaction_test`, `terminal_test`, shell partition. Actual terminal support still needs the owner walkthrough. |
| Logging/evidence | `logging_module.py`, `test_logging_module.py` | Workspace-isolated Hovel evidence and private operation notes, explicit collection, complete/partial distinctions and secret exclusion: lifecycle/files/runs/reports/follow partitions. Shared shell input counts do not establish command completion. |
| Plugin discovery | `plugin_manager.py` discovery/metadata, `test_plugin_manager.py`, `test_command_plugin.py` | Hovel module/chain discovery and six installed workflow skills replace the legacy plugin API (#13/#33). `agent_test`, `capabilities_test`, chains/runs partitions. No unchanged-plugin compatibility claim. |
| Plugin execution | `plugin_manager.py:execute_plugin/execute_plugin_streaming`, associated tests | Explicit remote script modes and local tools, separate stdout/stderr, exit status, timeout, cancellation and collection: runs/automation/follow partitions. Bundled engagement plugins are not copied (#7/#16). |

## Daily-use and MVP 5 story coverage

This preserves every row of `docs/research/terminal-mvp-coverage.md`, including
the later shared-session requirements. A check binding identifies what to run;
only a matching passing report proves it ran for the candidate.

| Daily-use story | Production evidence / current contract |
| --- | --- |
| Startup and setup | `setup_test`, `workspace_test`, `//core/launch:online_test`; explicit workspace and pinned runtime. |
| Connection creation | Lifecycle partition; authentication and the single accepted recap. |
| Saved configurations | `profile_test`, lifecycle partition; load never connects. |
| Active connections | Lifecycle partition; independent named owners and explicit recovery. |
| Shells | Shell partition; retained create/inspect/control/observe/resize/close, real full-screen applications. |
| Tunnels | Lifecycle/shell/reverse/chains partitions; SOCKS and qualified forward identities. |
| Close and quit | Lifecycle/shell/files partitions; exact reviewed teardown and keep/cancel. |
| SCP entry | Files partition; named authenticated connection and return to management. |
| Remote navigation | Files partition; structured listings, tree, metadata, errors and completion. |
| Local navigation | `files_test`, files partition; selected roots and upload containment. |
| Single transfer | Files partition; exact bytes, overwrite and observed transfer outcome. |
| Batch discovery | Files partition; nonrecursive exact review and explicit zero/error state. |
| Batch execution | Files partition, `interaction_test`; both progress levels and responsive frontend. |
| Batch completion | Files partition; per-file outcome, retained partials and durable summary. |
| Background work | Files/shell/follow partitions; browsing/help/shells while work continues. |
| Help and prompt | `interaction_test`, `terminal_test`; completion, quoting, contextual help and history. |
| Logs and evidence | Runs/reports/files/follow partitions; viewing is distinct from collection. |
| Scripts and AI operations | Runs/automation/chains partitions and `agent_test`; the #64 external exercise is separately recorded in `docs/research/agent-workflows.md`. |
| Accessibility and terminal capability | `interaction_test`, `terminal_test`, shell partition; 80×24 through 200×50, smaller layout checks, keyboard, no-color, reduced animation and safe remote text. |
| Offline contract discovery (#88) | `capabilities_test`, `site_test`; inventory, API deep links and drift detection. Catalog presence is not behavior evidence. |
| Headless workspace/lifecycle (#89) | `workspace_test`, lifecycle partition; no-TTY review/confirm/refusal and no implicit replacement. |
| Independent live observation (#90/#96) | `activity_test`, follow partition; independent cursors, gaps, shared identities, truthful failure and no control side effects. |
| Installed skill suite (#63/#64) | `agent_test`, installed recipe in runs partition; six workflows, six destinations, conflict preservation and interrupted-update recovery. Native client discovery is opt-in. |
| Shared ownership and control (#93/#94) | Shell partition (`sessions_lab.py`); one controller, fenced takeover, private requests, independent observers and truthful loss. |
| Shared TUI (#95) | Shell partition (`shell_lab.py`); human/agent takeover, observer isolation, history/snapshot recovery, two full-screen shells and retained detach/quit. |
| Shared owner walkthrough (#65) | **Pending.** The owner must actually observe and manually operate the TUI/follower while the agent uses the same workspace. Automated PTYs and the #64 model exercise do not replace this. |

## Hovel report comparison

Inspected Hovel's published report on 2026-09-24: generated
`2026-09-24T05:05:53Z`, source
`b4190bb548c49263003d7d398571c3837717890f`.
The [overview](https://vibepwners.github.io/hovel/reports/tests/latest/index.html#view=overview)
and [coverage](https://vibepwners.github.io/hovel/reports/tests/latest/index.html#view=coverage)
are a moving reference, not evidence produced by Burrow.

| Hovel surface | Applicable Burrow evidence and boundary |
| --- | --- |
| Test overview, suites, target attempts and logs | Burrow's BEP-backed Reports tab binds each attempt, source tree, environment and artifact hash. Required and #86 advisory suites stay separate. |
| Agent reachability, typed MCP and semantic contracts | Burrow's contract is CLI-first; optional MCP is not a required or measured typed surface. Report usable operational routes, documented JSON shapes and passing selected production semantics separately. Do not count a refusing route or catalog entry as completion. |
| Go/Python/Rust SDK branch coverage | Burrow publishes no SDK and imports Hovel's pinned public Go SDK. Hovel SDK percentages cannot measure Burrow application code. Burrow currently measures production Go lines; application branches remain **unmeasured**, not equivalent to line coverage. |
| Linter/static-analysis evidence | Burrow's declared workflow syntax and site checks appear as test targets. It does not currently provide Hovel's full multi-language linter report. This is an explicit reporting difference. |
| Skill packages and validation | Burrow packages six CLI workflow skills. Deterministic installer checks and opt-in native discovery/external-agent evidence have different scopes. OpenCode native discovery remains unverified when absent. |

Hovel's sampled report itself lists failed 100% SDK-branch thresholds (Go
1950/1971, Python 743/762, Rust 487/508); its operator-contract inventory reports
107/107. Neither the thresholds nor those results are Burrow release evidence.
No claim of equal numerical coverage or exhaustive semantic equivalence is made.

## Required evidence and acceptance status

- Candidate preflight: require every production partition to pass. The generated
  report and #65 closeout identify the exact commit and outcomes; this source
  mapping alone is not execution evidence. Preserve failed runs before any new attempt.
- Coverage and staged API/Reports: require the exact unchanged candidate and
  `aspect burrow-report release`; the required CI report job enforces this gate.
- Separate #86 diagnostics: record the candidate's actual results; the exception covers
  only the three tagged diagnostic targets, not production SSH/follower failures.
- Native skills: #64 verified all six skills installed and updated in Codex and
  Claude user/project scopes; OpenCode was absent. This is prior evidence.
- Owner walkthrough: pending. Use the isolated fixture, have the agent operate
  installed skills while the owner observes the same TUI/follower, manually
  takes over and returns control, then reviews keep/close/cancel. Record only
  outcomes, candidate identity and unresolved defects; omit credentials, tokens,
  private hosts and raw terminal transcripts from tracked files.

## Runner stability and regression review

The required pipeline runs ten independent partitions without matrix fail-fast,
retains failure artifacts for 14 days, repeats setup/terminal checks three times,
and permits no failed-test retries. Only the three tagged #86 diagnostics run
in the advisory workflow. Local report regeneration archives previous inputs
under `.report-input/archive/`; archives are not promoted as current evidence.

Reviewed prior hosted failures
[`35489842429`](https://github.com/Bochner/burrow/actions/runs/35489842429)
(WAL lifetime and the two retained-manager proofs) and
[`35488920596`](https://github.com/Bochner/burrow/actions/runs/35488920596)
(retained-manager refusal and a production reports daemon loss). The existing
scoped exception covers the former diagnostics. Production daemon loss remains
known and is not excused by that tag or by a later passing run. The #65 portable
development gate reproduced the WAL diagnostic; it is not evidence of a new fix.

New checks use public CLI/SSH and report-generation boundaries. They exposed
and corrected cancellation being disguised as SFTP unavailability, default
discovery request syntax missing from dispatch, and cached completion bypassing
a cancellation. The headless handshake check uses a server hold marker rather
than a race against a four-second sleep. Standards/spec review also corrected
shell-resume metadata (a controller can resize) and bound headless historical
log checks to Reports. No runtime retries, replacement state store, new public
module or dependency was introduced.

## Explicit follow-up status

| Follow-up | Status and release boundary |
| --- | --- |
| #86 SQLite WAL lock lifetime | Open upstream defect; three diagnostics advisory by owner decision. Prior required SIGBUS failures remain known, including the #64 follower run; a passing retry does not resolve them. Official runtime fix/pin and restoration of the strict gate remain outstanding. |
| #28 daemon compatibility | Open upstream contract; only verified Burrow-started pinned daemons are supported. General local/remote attachment remains deferred. |
| #29 terminal geometry | Open upstream native API; accepted typed public session commands implement Burrow's geometry contract now (#87/#93–96). |
| #30 lifetime module logging | Open upstream; bounded diagnostics and operation evidence remain distinct. No claim that suppressed SDK logs are retained. |
| #34 structured retained controls | Open upstream API improvement; current public session-command workaround remains the accepted supported boundary. |
| #39 background retained-run results | Open upstream; explicit collection remains required; follow/view does not collect. |
| #75 attached Hovel catalog refresh | Open upstream; Burrow verifies registration and the embedded frontend's availability; general attached-shell refresh is not claimed. |
| Prebuilt Linux archive | Specified, unimplemented in `DISTRIBUTION-FOLLOWUP.md`; existing v0.1.0 has source only. Publication requires a separate owner action. |
| Other platforms, SMB/WinRM, generic Mesh routing, automatic recovery, plugin distribution | Deferred outside this MVP by #16/#33 and the Wayfinder map. No placeholder runtime adapters. |
| PR, merge, Pages/distribution visibility, LazySSH archive | Owner authorized staging a PR and closing #65 after acceptance on 2026-09-24. Merge remains explicitly withheld; publication/distribution and archiving require separate actions. Work stays on `mvp5` through review. |
