# LazySSH parity baseline

Research date: 2026-09-11. Source revision: [`Bochner/lazyssh@9eb84452c31cb527bf8e938e23ffc92974fb91cb`](https://github.com/Bochner/lazyssh/tree/9eb84452c31cb527bf8e938e23ffc92974fb91cb). This is a source and test review, not a claim that the upstream tests or remote workflows were executed. No engagement plugins were run.

## The transport decision changes the migration

Current LazySSH declares `paramiko>=3.0.0`, but its application source does not import or call Paramiko. Its actual transport is the system `ssh`/`scp` executables, with a background OpenSSH ControlMaster and filesystem control socket. Terminal sessions, forwarding, file transfer and both bundled plugins reuse that socket. This is an OpenSSH workflow rewrite, not a port of a Paramiko client. [Dependencies][dependencies], [SSH implementation][ssh], [SCP implementation][scp], [plugin environment][plugins].

A native Go SSH client does not automatically expose an OpenSSH multiplexing socket. Therefore reproducing `LAZYSSH_SOCKET_PATH` by putting a path in an environment variable is insufficient: existing scripts call `ssh -S` or `scp -o ControlPath=...` against it. The native Go direction needs migrated plugins or a deliberate OpenSSH compatibility path. This is an architectural inference from the call chain above; resolve it explicitly before promising full plugin compatibility. A hybrid is also possible: the current Go SSH package has `NewControlClientConn`, which connects a Go client through an existing OpenSSH ControlMaster proxy connection. That can preserve an OpenSSH-owned master/socket while letting Go use SSH channels; it does not make an ordinary Go-owned connection export an OpenSSH socket. See the companion library research for versions and the integration recommendation, and [the official API](https://pkg.go.dev/golang.org/x/crypto/ssh#NewControlClientConn).

## Reachable command and behavior matrix

Acceptance checks below are proposed Burrow criteria, not existing verified Burrow behavior. Preserve operator outcomes; exact command spelling can follow Hovel conventions if migration aliases or a mapping are documented.

| Area | Current reachable behavior | Go rewrite implication | Acceptance check |
| --- | --- | --- | --- |
| Startup | Click entry point; `--debug`, `--config PATH`; startup dependency checks, banner, configuration display and interactive command prompt. | Preserve startup diagnostics and explicit config selection; follow Hovel CLI conventions. | Missing prerequisite gives actionable error; explicit config governs the entire run. |
| Connection creation | `lazyssh -ip HOST -port PORT -user USER -socket NAME`; optional `-ssh-key`, `-shell`, `-no-term`, `-proxy [PORT]`. Shows SSH command and asks confirmation; proxy defaults to 9050. Wizard provides port 22 default. | Structured connection inputs and validation shared across TUI, CLI and saved profiles. | Invalid name/port rejected consistently; cancellation leaves no live connection; failed authentication does not create active state. |
| Authentication | OpenSSH handles key/default identity/agent and interactive authentication; explicit key expands `~`. No application password vault or Paramiko session. | Choose explicit key, encrypted key, agent, password and keyboard-interactive behavior; inventory implicit OpenSSH configuration support before claiming equivalence. | Exercise each intended authentication method against a controlled server, including passphrase cancellation and unavailable agent. |
| Host identity | Current creation forcibly sets `UserKnownHostsFile=/dev/null` and `StrictHostKeyChecking=no`. | Intentional change needed: known-host verification and clear changed-key failures; any engagement override explicit and scoped. | Unknown host can be approved deliberately; changed host fails; normal profile never silently ignores verification. |
| Connection state | In-memory map keyed by socket path; `list` shows connections, shell/key/proxy details, terminal method and tunnels. | Separate saved configuration from live state. | Two named connections remain independent; stale/disconnected state is accurate. |
| Shells | `open NAME` reuses connection with `ssh -tt`; optional remote shell. Closing native shell returns to prompt while master survives. | Coordinate terminal ownership with Charm/Hovel; support PTY, resize, input and terminal restoration. | Open shell, resize, run a benign command, exit and reopen without reconnecting; terminal restored on failure/interruption. |
| Terminal choice | `terminal auto/native/terminator`; `auto` tries Terminator then native; env selects initial method. `-no-term` suppresses automatic shell opening. | Hovel pane integration should preserve both managed sessions and headless connections; external terminal launch is a compatibility decision. | Headless connection keeps working; managed shell returns to UI; launch failure reports failure. |
| SOCKS | `-proxy` adds OpenSSH `-D` at connection creation. | Native client requires a SOCKS listener coupled to the connection lifecycle. | SOCKS connection reaches a controlled endpoint; bind conflict fails visibly; closing SSH closes listener. |
| Local forwarding | `tunc NAME l LISTEN_PORT DEST_HOST DEST_PORT` uses `-O forward -L`. | Track listener endpoint separately from destination and connection identity. | Controlled TCP echo traverses tunnel; cancellation closes listener without closing SSH. |
| Remote forwarding | `tunc NAME r REMOTE_LISTEN_PORT LOCAL_HOST LOCAL_PORT` uses `-O forward -R`. | Keep the reversed endpoint semantics explicit despite legacy variable names. | Controlled reverse echo works; refusal from SSH server is surfaced. |
| Tunnel removal | `tund ID`; IDs increment from 1 independently for each connection; command searches all connections and removes first match. | Fix ambiguity with connection-qualified IDs or globally unique IDs. | Two connections each with a first tunnel: deleting one never affects the other. |
| Connection closure | `close NAME` attempts tunnel cancellation, sends `-O exit`, removes tracked state; tolerates absent socket. | Preserve idempotent cleanup but retain/report unresolved live resources on real failure. | Close twice safely; failure is distinguishable from success; no leaked listener or session. |
| Exit | `exit`/`quit` asks to close active connections, then closes each. Ctrl+C/Ctrl+D in prompt instead breaks its loop. | Define detach versus close semantics and handle signals/EOF consistently. | Cancel exit keeps state; confirmed close releases resources; EOF/signal follows documented policy. |
| Saved profiles | `config`/`configs`, `connect NAME`, `save-config NAME`, `delete-config NAME`, `backup-config`. Save selects a current connection; wizard can offer save. | Import existing TOML fields and distinguish profile name from runtime connection ID. | Import, reconnect, edit, delete and backup round-trip; overwrite/delete confirmation preserved. |
| Transfer entry | `scp NAME` or `scp` with active-connection chooser; separate transfer prompt; exit returns to main prompt. | Reuse authenticated SSH connection; TUI may combine views without losing workflow. | Choose connection, transfer, return, and open shell without additional authentication. |
| Remote navigation | `ls [PATH]`, `tree [PATH]`, `cd PATH`, `pwd`; SSH commands use `ls`, `find`, `stat`; completions cache/throttle listings. | Prefer structured SFTP operations where available; document any required remote shell/tool fallback. | Spaces, Unicode, hidden files, symlinks, denied directories and non-GNU server utilities handled deliberately. |
| Local navigation | `local [PATH]`, `lcd PATH`, `lls [PATH]`; per-connection upload/download workspace. | Use Go filesystem APIs and clear local/remote path labels. | Local directory selection and completion consistently affect intended operation. |
| Download/upload | `get REMOTE [LOCAL]`, `put LOCAL [REMOTE]`; system `scp -q` with ControlPath; size, elapsed time, progress and transfer logging. | SFTP is a candidate; preserve outcomes and error reporting even if command remains named `scp`. | Byte-for-byte round-trip, empty and large files, overwrite, denied write, cancellation and disconnect; partial results never reported complete. |
| Batch download | `mget PATTERN`; nonrecursive `find` discovery in current remote directory, size summary and confirmation, sequential file downloads and aggregate progress. | Match current nonrecursive semantics initially; remove shell interpolation from pattern handling. | Zero/one/many matches; cancellation; one failed file doesn't become a successful total. |
| Prompt UX | Shell-like quoting with `shlex.split`, command/config/connection/plugin/path completions, command and SCP history; `help [COMMAND]`, `clear`, `debug` toggle or on/off forms. | Modern Charm components need equivalent keyboard discovery, error visibility and history decisions. | Quoted names/paths work; malformed quoting is nonfatal; help reachable with keyboard. |
| Visual/accessibility behavior | Dracula theme, panels, tables, trees, spinners/progress; plain text, no Rich, high contrast, colorblind and no-animation modes; bounded refresh rate. | Preserve accessible outcomes using Charm and Hovel theme ownership; do not rely on color alone. | Narrow terminal, non-TTY output and no-animation modes remain readable; focus/status identifiable without color. |
| Logging/evidence | General/component logs plus per-connection logs; connection, command, tunnel and transfer events; transfer counts/bytes and summary files. | Follow Hovel paths/logging contracts; retain evidence while protecting credentials and sensitive history. | Isolated connections/engagements don't mix evidence; secrets absent; failures and partial transfer distinguishable. |
| Plugin discovery | `plugin`/`plugin list`, `plugin info NAME`, `plugin run NAME CONNECTION [ARGS...]`; Python/shell scripts, metadata and validity display. | Decide legacy script compatibility versus Hovel extension contract; these are distinct plugin systems. | Invalid plugin inspectable but not runnable; plugin gets selected connection, not a global default. |
| Plugin execution | Streaming stdout/stderr, optional arguments, elapsed time, exit status and five-minute global timeout; Python launched with interpreter, shell by executable path. | Cancellation must close subprocesses/streams and identify output owner; no accidental local shell interpretation. | Large stdout+stderr, nonzero exit, timeout and cancellation do not hang UI or orphan work. |

Sources: actual command dispatch and wizards [command mode][commands]; transport/terminal lifecycle [SSH manager][ssh]; transfer implementation [SCP mode][scp]; persisted state [configuration][config] and [models][models]; visual contract [console][console] and [UI][ui]; logging [logging implementation][logging]; extension behavior [plugin manager][plugins].

## Saved data and plugin compatibility details

The default TOML file is `/tmp/lazyssh/connections.conf`; required profile fields are `host`, `port`, `username`, `socket_name`; optional fields are `ssh_key`, `shell`, `no_term`, `proxy_port`. Directory permissions are set to 0700 and saved config files to 0600; writes use a temporary file then rename. Comments are preserved through textual section replacement. Backups actually overwrite `connections.conf.backup`; the reference's timestamped-backup description is inaccurate. Burrow should use a real TOML encoder, validate input and choose permanent config versus runtime/evidence locations following Hovel; `/tmp` is not durable configuration storage. [Configuration source][config], [reference][reference].

Runtime artifacts are `/tmp/NAME` control sockets, `/tmp/lazyssh/NAME.d/{uploads,downloads,logs}`, `/tmp/lazyssh/{command_history,scp_history}`, and general logs under `/tmp/lazyssh/logs`. Active connection and tunnel metadata is not persisted as a reconnectable registry; existing sockets are not enumerated on startup. [Models][models], [command mode][commands], [SSH manager][ssh], [SCP mode][scp], [logging][logging].

Plugin search precedence is absolute directories from `LAZYSSH_PLUGIN_DIRS` (left to right), `~/.lazyssh/plugins`, `/tmp/lazyssh/plugins`, then packaged plugins. `.py`/`.sh` files beginning with `_` or `.` are skipped; paths resolving outside their search root are skipped. Metadata comes from first-50-line `PLUGIN_NAME`, `PLUGIN_DESCRIPTION`, `PLUGIN_VERSION`, `PLUGIN_REQUIREMENTS` comments. Validation checks shebang/executable status and may repair the Python execute bit. This is local executable code, not a sandbox. [Plugin manager][plugins].

Plugin API version 1 injects `LAZYSSH_SOCKET`, `LAZYSSH_SOCKET_PATH`, `LAZYSSH_HOST`, `LAZYSSH_PORT`, `LAZYSSH_USER`, `LAZYSSH_PLUGIN_API_VERSION`, `LAZYSSH_CONNECTION_DIR`, optional `LAZYSSH_SSH_KEY`/`LAZYSSH_SHELL`, and terminal width `COLUMNS`; it also inherits the parent environment. The workspace variable exists in source but is absent from the reference's environment table. [Environment construction][plugins], [reference][reference].

## Bundled engagement workflows in the parity scope

These are existing capabilities to track, not an instruction to execute them during research.

| Workflow | Existing scope | Burrow acceptance boundary |
| --- | --- | --- |
| `enumerate` | Single batched remote collection script; system/user/network/filesystem/security probes; structured probe outputs and failure status; priority findings, quick wins, category summaries; GTFOBins cross references and kernel-version exploit suggestions; JSON and text reports in connection logs, plus `--json` output. | Keep collection, interpretation and rendering separable; fixture-based checks for representative success/failure outputs and JSON/text agreement. Version-only exploit suggestions remain hypotheses, not verified vulnerability claims. |
| `upload-exec` | Architecture/platform detection; operator-provided local file staging, transfer and execution; arguments, background operation, timeout, output file and cleanup controls; dry-run; optional external msfvenom generation and handler instructions. | Record this explicitly so “full LazySSH parity” does not accidentally omit it. Implementation should be a later bounded workflow with explicit operator invocation, accurate cleanup/error state and a controlled test fixture. No execution was performed for this research. |

Primary source: [enumeration implementation][enumerate], [probe plan][plan], [GTFOBins data][gtfobins], [kernel suggestions][kernel], [upload/execute implementation][upload]. The plugin's displayed name is `upload-exec`, whereas its source filename is `upload_exec.py`.

## Source findings that should not become compatibility requirements

1. **Security defaults:** disabled host verification, predictable shared `/tmp` paths, raw command/history logging and shell-interpolated paths/patterns deserve explicit migration decisions. Preserve capability, not unsafe defaults. [SSH][ssh], [SCP][scp], [command mode][commands].
2. **Configuration divergence:** `--config PATH` loads/displays that file at startup, but prompt commands call default-path helpers. `load_config()` reads `LAZYSSH_SSH_PATH`, `LAZYSSH_TERMINAL`, `LAZYSSH_CONTROL_PATH`; the actual manager hardcodes `ssh`, Terminator lookup and `/tmp`. Documented overrides are not end-to-end behavior. [Main][main], [config][config], [SSH][ssh].
3. **False success:** `cmd_open` ignores the terminal opener's Boolean result; `close_connection` drops tracked state and returns success even on subprocess errors; transfer connect reports connected after a failed initial `pwd`. Burrow needs truthful failure state. [Commands][commands], [SSH][ssh], [SCP][scp].
4. **Lifecycle discrepancy:** explicit exit cleans up, while prompt EOF/Ctrl+C breaks the loop without calling the same cleanup path. “Persistent terminal” means OpenSSH master reuse across shell invocations; it does not prove supported reconnect/discovery after restarting the manager. [Command loop][commands], [main][main].
5. **Transfer details:** upload progress is time-estimated at 10 MB/s, not measured bytes. Remote listings depend on GNU-style `stat -c` and `find -printf`; some paths are quoted and others interpolated. `put` reads the given local path directly, so configured upload directory semantics need a deliberate specification. [SCP source][scp].
6. **Dead command:** `cmd_disconnectall` exists but is absent from `self.commands`; do not list it as a working user command. [Command dispatch][commands].
7. **Plugin dry-run limits:** `upload-exec` calls architecture detection before dispatching dry-run, so dry-run is not necessarily network-free. Background mode skips default cleanup; several early failure paths return before cleanup. Record this when specifying dry-run and artifact ownership. [Upload workflow][upload].

## Test evidence and minimum migration checks

Upstream has tests for commands, config, models, SSH, SCP, plugins, enumeration findings, GTFOBins/kernel data, upload-exec, logging, console/themes, plain text and animation/refresh settings. SSH tests largely monkeypatch subprocesses and filesystem existence; many interactive/remote paths are explicitly excluded with `pragma: no cover`. This is useful behavior evidence, not proof of live protocol/terminal portability. [Test tree][tests], [SSH tests][sshtests], [config tests][configtests], [SCP tests][scptests], [plugin tests][plugintests], [test isolation fixtures][fixtures].

Do not run the upstream suite unchanged alongside a real LazySSH session: its `pytest_configure` recursively removes contents of `/tmp/lazyssh` at test-session start. Use an isolated container or change the upstream test paths in a disposable copy before any execution. Burrow tests should use test-specific temporary directories. [Exact fixture behavior][fixtures].

Before declaring a Go slice complete, use the smallest relevant automated checks plus a controlled local SSH-server integration exercise. Prioritize authentication and host keys; PTY/resize/return-to-UI; local/remote/SOCKS forwarding and cleanup; file-byte equality and interrupted transfers; and importing a real-format LazySSH profile. Tests must not require live homelab targets, secrets or engagement tooling. Plugin assessment can use inert scripts and captured probe fixtures. These are recommendations from the gaps above, not tests already added or run.

## Questions for Wayfinder

1. Does full parity require existing third-party Python/shell scripts unchanged, or their capabilities rewritten behind Hovel/Burrow operations?
2. Which inherited OpenSSH features are used in practice: SSH config aliases, ProxyJump/ProxyCommand, agents/hardware keys, certificates and keyboard-interactive authentication? Current Python source alone cannot inventory the operator's implicit OpenSSH behavior.
3. Should sessions/tunnels survive closing Burrow or a Hovel pane, and which component owns their lifetime?
4. Which existing profiles, history and evidence need migration, and where should Hovel own engagement scoping?
5. Is Terminator compatibility required, or can Hovel pane creation and a standalone native terminal preserve the intended workflow?

[dependencies]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/pyproject.toml#L33-L46
[ssh]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/ssh.py
[scp]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/scp_mode.py
[commands]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/command_mode.py
[config]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/config.py
[models]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/models.py
[console]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/console_instance.py
[ui]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/ui.py
[logging]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/logging_module.py
[plugins]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/plugin_manager.py
[reference]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/docs/reference.md
[main]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/__main__.py
[enumerate]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/plugins/enumerate.py
[plan]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/plugins/_enumeration_plan.py
[gtfobins]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/plugins/_gtfobins_data.py
[kernel]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/plugins/_kernel_exploits.py
[upload]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/plugins/upload_exec.py
[tests]: https://github.com/Bochner/lazyssh/tree/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests
[sshtests]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_ssh.py
[configtests]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_config.py
[scptests]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_scp_mode.py
[plugintests]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_plugin_manager.py
[fixtures]: https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/conftest.py
