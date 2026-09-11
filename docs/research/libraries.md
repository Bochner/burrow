# Burrow library research

Researched 2026-09-11 against primary documentation, source, release metadata, and Hovel commit `c461ba282a8aecc7aa3a079a4613bf5e2640c388`. Recommendations are proposals for Wayfinder, not implemented or interoperability-tested functionality. Upstream `main` and release metadata can change; pin selected versions when the first implementation slice is approved.

## Recommended direction

Build the user experience with Bubble Tea, Bubbles, and Lip Gloss. Keep network operations independent of rendering. For SSH, evaluate OpenSSH session management plus Go's `golang.org/x/crypto/ssh` and `github.com/pkg/sftp`: there is now an official bridge from Go SSH to OpenSSH ControlMaster sockets, which could preserve existing SSH behavior while providing native transfer progress. The first spike should settle that choice before committing to a complete native SSH implementation.

Use Charm v2 inside a separately launched Burrow executable. If Burrow instead shares Hovel's Go UI models in-process, align with Hovel's existing v1 family. A process boundary permits different Go dependencies; it does not eliminate the need to match Hovel's command, session, output, and lifecycle contracts.

## Hovel compatibility and current versions

Hovel currently uses Go 1.26.0. Its core module pins the following, including Bubble Tea indirectly. Its Bazel dependency module separately pins Lip Gloss and `x/crypto` v0.53.0, while core uses `x/crypto` v0.54.0. Preserve awareness of both manifests when following Hovel's build conventions. [Hovel core module](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/go.mod), [Bazel Go dependency module](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/third_party/go_deps/go.mod).

| Library | Hovel core snapshot | Upstream latest release observed | Burrow implication |
| --- | --- | --- | --- |
| Bubble Tea | `github.com/charmbracelet/bubbletea v1.3.10` | [v2.0.9](https://github.com/charmbracelet/bubbletea/releases/tag/v2.0.9), 2026-08-19 | Separate executable can use `charm.land/bubbletea/v2`. |
| Bubbles | `github.com/charmbracelet/bubbles v1.0.0` | [v2.2.1](https://github.com/charmbracelet/bubbles/releases/tag/v2.2.1), 2026-08-24 | Match the Bubble Tea major version. |
| Lip Gloss | `github.com/charmbracelet/lipgloss v1.1.1-0.20250404203927-76690c660834` | [v2.0.6](https://github.com/charmbracelet/lipgloss/releases/tag/v2.0.6), 2026-08-11 | Use `charm.land/lipgloss/v2` with the v2 UI. |
| Huh | `github.com/charmbracelet/huh v1.0.0` | [v2.0.3](https://github.com/charmbracelet/huh/releases/tag/v2.0.3), 2026-03-10 | Huh v2 exists; do not follow obsolete advice that it only supports Tea v1. |

V2 changes imports, keyboard messages, rendering, and the `View` return type. A v1 `tea.Model` is not a v2 model. Huh's current main module uses all three `charm.land/*/v2` dependencies; verify the chosen release's module graph rather than mixing examples from different generations. [Bubble Tea migration guide](https://github.com/charmbracelet/bubbletea/blob/main/UPGRADE_GUIDE_V2.md), [Huh module](https://github.com/charmbracelet/huh/blob/main/go.mod).

## Charm ecosystem: what earns a dependency

The catalog below covers Charm's advertised libraries and adjacent projects relevant to this SSH manager; it does not imply every Charm application should become a Burrow dependency. The decision column is Burrow-specific judgment. [Official catalog](https://charm.land/).

| Project | Relevant capability | Decision |
| --- | --- | --- |
| [Bubble Tea](https://github.com/charmbracelet/bubbletea) | Event-driven TUI framework; inline/full-screen rendering, keyboard/mouse, resize events. | Core. One owner of the terminal; asynchronous connection/transfer results update its model. |
| [Bubbles](https://github.com/charmbracelet/bubbles) | Lists, tables, text inputs, viewport, spinner, progress, help/key components. | Core. Host/session selection, transfer progress, tunnel tables, and searchable output without custom widgets. |
| [Lip Gloss](https://github.com/charmbracelet/lipgloss) | Terminal styles and layout, borders, padding, tables and lists. | Core. Recreate Rich-style panels and coherent status displays; reuse Hovel's visual conventions. |
| [Huh](https://github.com/charmbracelet/huh) | Validated forms, password inputs, selects, confirmation, accessible prompt mode. | Add when connection/profile forms need it; simple inline prompts can use Bubbles. Match major versions. |
| [Log](https://github.com/charmbracelet/log) | Structured logging with text/JSON/logfmt and a `slog` handler; current imports use `charm.land/log/v2`. | Optional presentation handler for stdlib `log/slog`. Keep operation/audit records structured and credentials redacted. |
| [Glamour](https://github.com/charmbracelet/glamour) | Styled Markdown rendering. | Optional for help/runbook content; not required for ordinary tables or remote command output. |
| [Fang](https://github.com/charmbracelet/fang) + [Cobra](https://github.com/spf13/cobra) | Styled help/errors, completion, version and manpage conveniences around Cobra. Fang describes itself as experimental. | Defer until the external CLI command tree is settled. Hovel alignment comes before adding a second command framework. |
| [Wish](https://github.com/charmbracelet/wish) / [Charm SSH](https://github.com/charmbracelet/ssh) | SSH servers; Wish middleware can serve a Tea application to incoming SSH clients. | Not the outbound client stack. Revisit only if users should SSH *into Burrow* as a hosted service. |
| [Wishlist](https://github.com/charmbracelet/wishlist) | SSH directory, config-driven server picker, and bastion application. | Study host-selection and connection UX as a close reference. It does not establish Burrow's Hovel or LazySSH parity contracts. |
| [Harmonica](https://github.com/charmbracelet/harmonica) | Spring/physics animation. | Let Bubbles use it transitively where appropriate; no bespoke animation engine. |
| [Ultraviolet](https://github.com/charmbracelet/ultraviolet) and [Charm x](https://github.com/charmbracelet/x) | Lower-level terminal primitives and utility packages. | Prefer Tea's higher-level API. Add direct use only for a demonstrated terminal gap. |
| [Gum](https://github.com/charmbracelet/gum) | Attractive prompts and progress in shell scripts. | Useful for developer scripts, not an embedded Go UI dependency. |
| [VHS](https://github.com/charmbracelet/vhs) | Scripted terminal recordings. | Later development-only dependency for demos and visual smoke checks; use synthetic hosts/data. |
| [Glow](https://github.com/charmbracelet/glow) | Markdown reader application. | Reference UX; use Glamour if Burrow needs rendering. |
| [Crush](https://github.com/charmbracelet/crush) | Agentic coding application with a polished terminal UI. | Visual reference only. Its agent/tool architecture is outside SSH management. |
| [Mods](https://github.com/charmbracelet/mods), [Skate](https://github.com/charmbracelet/skate), [Soft Serve](https://github.com/charmbracelet/soft-serve) | LLM CLI, key/value application, and Git-over-SSH service. | No initial runtime role; avoid adding unrelated AI, storage, or hosting systems. |

UI proposal: host/session list with clear active context, a details area for identity and tunnel state, and a transfer queue with byte counts and cancellable progress. Include keyboard help, light/dark terminal support, narrow-window behavior, readable text status in addition to color, and a plain non-TTY output path. These are design requirements, not claims of an existing screen. A Bubble Tea viewport displays text; it is not by itself a terminal emulator for arbitrary interactive programs. Hand the terminal to an SSH subprocess for the first interactive-shell slice and restore the TUI on exit.

Avoid writing concurrent progress/log output straight to the TUI's stdout. Tea owns that terminal; route events through the model and diagnostics to a file or other dedicated sink. [Bubble Tea logging guidance](https://github.com/charmbracelet/bubbletea#logging-stuff).

## Go SSH versus Paramiko and OpenSSH

Yes: `golang.org/x/crypto/ssh` supplies native Go SSH clients, authenticated connections, shell/exec sessions, PTY/window handling, channel streams, public-key/password/keyboard-interactive authentication, and forwarding primitives. It is a Go project supplementary module, not part of the standard library. SFTP is supplied separately by `github.com/pkg/sftp`. [Go SSH API](https://pkg.go.dev/golang.org/x/crypto/ssh), [SFTP project](https://github.com/pkg/sftp).

| Requirement | Starting point | Work Burrow still owns |
| --- | --- | --- |
| Paramiko-style command execution | `ssh.Client.NewSession`, `Session.Run`, streams and exit status | Timeouts/cancellation, bounded output, lifecycle and reporting. |
| Interactive shell | OpenSSH process initially; native `RequestPty`, `Shell`, `WindowChange` if needed | Terminal handoff/restoration; a text viewport cannot replace full terminal semantics. |
| Authentication | Password, key and keyboard-interactive APIs; [`ssh/agent`](https://pkg.go.dev/golang.org/x/crypto/ssh/agent) | Credential prompts and storage policy; do not enable agent forwarding implicitly. |
| Host identity | [`ssh/knownhosts`](https://pkg.go.dev/golang.org/x/crypto/ssh/knownhosts) | Verify existing entries, deliberate first-use trust and explicit changed-key errors. Never default to `InsecureIgnoreHostKey`. |
| Upload/download | [`pkg/sftp`](https://github.com/pkg/sftp) | Byte progress, cancellation, incomplete-file policy, permissions, overwrite behavior, large files. |
| Local/remote forwarding | SSH dial/listen primitives or OpenSSH forwarding options | Listener ownership, bind addresses, duplex copying, errors and cleanup. |
| Dynamic SOCKS forwarding | OpenSSH `-D`, or a separately selected SOCKS implementation | SSH channels alone do not parse SOCKS requests. |
| SSH configuration and session reuse | OpenSSH `Host`/`Include`/`Match`, `ProxyJump`, `ControlMaster`/`ControlPersist` | Profile precedence, socket lifecycle, ownership and scope isolation. |

The SSH session APIs above are documented by [Go SSH](https://pkg.go.dev/golang.org/x/crypto/ssh); configuration, agent/provider options and multiplexing behavior are defined by [OpenSSH ssh_config](https://man.openbsd.org/ssh_config). A native SSH protocol library does not automatically implement the OpenSSH configuration language or launch/manage its master processes. Local raw terminal handling is available through [`golang.org/x/term`](https://pkg.go.dev/golang.org/x/term).

### A promising bridge already exists

`ssh.NewControlClientConn`, added in `x/crypto` v0.53.0, attaches to an existing OpenSSH ControlMaster connection using proxy mode. This is especially relevant because Hovel's inspected dependency manifests already reach that version. It gives Burrow a candidate path from OpenSSH-owned authentication/configuration to native Go channels and SFTP without an independent second login. [Go API](https://pkg.go.dev/golang.org/x/crypto/ssh#NewControlClientConn), [implementation](https://github.com/golang/crypto/blob/master/ssh/control.go).

The connection must be a secure local socket to an already-running master: this mode bypasses the cryptographic handshake, and using an ordinary TCP connection leaks plaintext. Burrow would still own secure socket placement, target/account matching, cleanup and behavior when the master exits. This is a research candidate, not a proven integration. [Control connection implementation](https://github.com/golang/crypto/blob/master/ssh/control.go).

Compare three options in the first spike: OpenSSH alone, OpenSSH plus native SFTP over its control socket, and entirely native SSH. Prefer whichever preserves LazySSH's actual behaviors with the smallest reliable implementation. Test encrypted keys/agent authentication, an unknown and changed host key, a jump host, shell resize/exit, transfer cancellation, and master termination. Do not silently substitute a fresh native login if reuse fails.

## Future SMB placeholder — research only

Slinger is an application built around Python Impacket sessions. Its scope includes SMB file operations and RPC-backed Windows administration, so a generic SMB file client is not equivalent. Password, NTLM-hash and Kerberos authentication are documented. Its repository identifies Apache-2.0 licensing. Treat its session/workflow UX as a reference, not as evidence that one Go SMB dependency replaces its full feature set. [Slinger](https://github.com/ghost-ng/slinger).

| Candidate | Verified scope and authentication | License / observed activity | Assessment |
| --- | --- | --- | --- |
| [hirochachacha/go-smb2](https://github.com/hirochachacha/go-smb2) | SMB2/3 share enumeration and filesystem operations; NTLM examples. | BSD-2-Clause; not archived; repository push 2026-08-30. | Small file-management candidate. Do not label it abandoned based on old release dates alone. |
| [CloudSoda/go-smb2](https://github.com/CloudSoda/go-smb2) | Fork of the above; filesystem/context APIs, NTLM and explicit Kerberos `Krb5Initiator` example. | BSD-2-Clause; not archived; push 2026-08-04; README warns releases remain pre-1.0 during development. | Compare for file browsing/transfer; pin and test API differences. |
| [jfjallid/go-smb](https://github.com/jfjallid/go-smb) | SMB2/3 plus named-pipe/TCP DCE/RPC, service/registry and other Windows RPC support; password/hash NTLM and Kerberos via a modified gokrb5 fork. | MIT; not archived; push 2026-08-22; described as work in progress. | Closer to Slinger's administrative scope; assess only the required RPC subset. |
| [oiweiwei/go-msrpc](https://github.com/oiweiwei/go-msrpc) | DCE/RPC/DCOM, generated protocol stubs, SMB transport, Kerberos/NTLM/SPNEGO integration. | MIT; not archived; push 2026-09-10. | Broader Windows protocol option when concrete RPC requirements justify it. |
| [RedTeamPentesting/adauth](https://github.com/RedTeamPentesting/adauth) | AD authentication helper, including Kerberos/NTLM credential handling. | MIT; not archived; push 2026-05-12. | Potential shared authentication helper later, not an SMB transport. |

Activity dates come from each repository's GitHub metadata as observed on the research date; a push is not proof of release quality, security review, or long-term maintenance. No candidate was installed or tested. Before choosing: check dialect negotiation, required signing/encryption, domain/local account distinction, Kerberos SPNs/tickets, cancellation, reconnect and transport routing. Start future SMB work with share browsing and transfer; define additional RPC operations explicitly.

## Future WinRM placeholder — research only

Evil-WinRM is Ruby and uses the WinRM ecosystem's PSRP support for runspace pools/pipelines. Its UX includes transfer progress, completion and session history; it documents NTLM-hash, Kerberos and certificate options. Its license is LGPLv3+. Reproducing that interactive experience requires more than launching a PowerShell command through WinRS. [Evil-WinRM](https://github.com/hackplayers/evil-winrm).

| Candidate | Evidence | Assessment |
| --- | --- | --- |
| [masterzen/winrm](https://github.com/masterzen/winrm) | Apache-2.0; WinRM/WinRS command streaming and cancellation; NTLM/Kerberos transport examples. Not archived; push 2026-04-07. | Strong initial candidate for simple execution. README has contradictory old domain-auth statements; inspect current transports and test domain auth instead of repeating them. `RunPSWithString` wraps PowerShell execution, not proof of a persistent PSRP runspace. |
| [investigato/go-psrp](https://github.com/investigato/go-psrp) + [go-psrpcore](https://github.com/investigato/go-psrpcore) | MIT; Go PSRP/WSMan client and protocol core; documented Basic/NTLM, runspaces, pipelines and serialization. Not archived; pushes 2026-04-03 and 2026-04-02 respectively. | More direct protocol fit, but early candidate requiring an interoperability spike. Do not assume complete Evil-WinRM parity, Kerberos, certificate auth, pass-the-hash or production maturity. |
| [pypsrp](https://github.com/jborean93/pypsrp) / existing Evil-WinRM executable | Existing Python/Ruby PSRP implementations. | Optional external process fallback if Go candidates fail required cases; incurs an additional runtime and a distinct credential/lifecycle boundary. |

The current masterzen repository includes an [encryption implementation](https://github.com/masterzen/winrm/blob/master/encryption.go). Do not infer that all auth/transport combinations are protected merely because that file exists. A future spike must verify TLS certificate checks, HTTP message encryption where required, actual auth mechanisms, persistent runspace behavior, transfer, remote path completion and cancellation. Do not copy old README setup steps that enable unencrypted traffic as Burrow defaults.

## Dependency and licensing decisions left open

Selected Charm UI libraries expose MIT licenses in their primary repositories; Go `x/crypto` is BSD-3-Clause, and `pkg/sftp` is BSD-2-Clause. Verify exact selected versions and transitive dependencies before redistribution. Slinger and Evil-WinRM are research references here; copying their implementation or bundling either executable would require preserving and assessing its own license terms. No protocol dependencies, SMB/WinRM adapter interfaces, credential store, or runtime plugin framework have been added by this research.

Wayfinder should settle the Hovel process boundary and SSH parity acceptance criteria first. Then pin one compatible Charm family and build one real SSH slice. SMB and WinRM remain these explicit backlog placeholders until their required workflows are defined.
