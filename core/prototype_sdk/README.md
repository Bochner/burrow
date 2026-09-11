# Disposable terminal placement proof

This branch also contains the [setup and attachment proof](SETUP.md), which
reuses this fixture against the verified published Hovel binary.

For [Where should the terminal UI run?](https://github.com/Bochner/burrow/issues/8).
This branch extends the accepted external SDK proof with an inert Linux PTY
frontend. It is a boundary probe, not a Charm UI or SSH implementation. No owner
placement decision has been made.

## Observed on Linux amd64, 2026-09-11

Hovel pin: `c461ba282a8aecc7aa3a079a4613bf5e2640c388`.
Unchanged SDK source, consumer-owned BUILD overlay; original archive SHA-256
`c2233e71fa79473555863b5d0b8381ccf5902877fb6d341825f60a6e12a1e722`.
Inherited pins: Go 1.26.5, rules_go 0.61.1, Gazelle 0.51.3, x/sys v0.47.0,
rules_pkg 1.2.0, Aspect 2026.33.3, Bazel 9.1.1.

Both linked and archive packages pass discovery, schema, confirmed daemon
execution, explicit-close process cleanup, and daemon-SIGTERM process cleanup.
The terminal integration uses a real controlling PTY and the public Hovel CLI.
Its assertions deliberately document missing functionality at this pin; a green
probe does not mean the embedded UI route is production-ready.

| Probe | Observation |
| --- | --- |
| Module stdout | Framed JSON-RPC stays valid; terminal bytes use SDK PTYSession. |
| Initial geometry | Operator terminal is 80 columns by 24 rows; embedded PTY reports 0 by 0. |
| Resize | Operator terminal becomes 120 by 40 and receives SIGWINCH; embedded PTY still reports 0 by 0. |
| Raw input | Probe receives individual x/y bytes without Enter. The probe explicitly configures its slave raw mode. |
| Ctrl-C | Reaches probe as byte 03; probe remains alive. This proves byte delivery, not remote-command cancellation. |
| Ctrl-] | CLI intercepts it and detaches; probe does not receive byte 1d. |
| Reattach | A second attachment exchanges fresh bytes with the same retained process, with history disabled to prevent replay from satisfying assertions. |
| Terminal settings | Operator termios exactly matches pre-attachment state after each detach. |
| Screen/cursor restoration | After probe enters alternate screen and hides cursor, detach emits neither corresponding reset. Second attachment explicitly resets both. |

The restoration observation checks emitted control sequences, not pixels in a
terminal emulator. The integration drives only a disposable PTY; it cannot leave
the user's terminal in alternate-screen mode. The harness drains output after
CLI exit before checking missing resets.

## Repeat

```sh
aspect burrow-prototype check
aspect burrow-prototype integration -- /absolute/pinned-hovel-executable
aspect burrow-check
```

Build the host executable from the exact Hovel pin in a separate checkout with
`aspect build @hovel_core//cmd/hovel`; pass the resulting executable above.
The integration uses temporary workspaces and XDG directories and cleans up its
processes. The module performs no SSH or external commands and uses no secrets.
The full branch gate checks package/protocol and generated docs; integration is
an explicit gate requiring the pinned host executable.

## Separate local frontend proof

`frontend.py` is a throwaway Python standard-library terminal probe, not a
production Python frontend or final UI design. It uses only public HTTP/JSON
ListSessions, ReadSession, WriteSession, and CloseSession over the Hovel Unix
socket. It imports no Hovel internals. The Go module remains inert.

The same real-daemon integration gate additionally runs this frontend in a real
controlling PTY for each linked/archive install and observes:

- Initial local display at 80×24; SIGWINCH redraw at 120×40.
- Raw operator input sent to the retained module session through the public API;
  returned bytes displayed as escaped text, so daemon output cannot emit terminal
  control sequences into the management screen.
- Ctrl-] detach and Ctrl-C frontend interruption both restore cursor, alternate
  screen, and exact termios; the Hovel session/process remains available.
- Reattachment reads the existing session and exchanges fresh input/output.
- Explicit `q` closes the session through the public API, restores terminal state,
  and removes the module process. Daemon shutdown cleanup is also checked.

The observer checks emitted control sequences rather than rendered pixels. Ctrl-C
here interrupts the local UI, not a remote command. Remote PTY geometry still
reports 0×0 and is deliberately not disguised by the local display size.

For a manual probe against an already running disposable session:

```sh
aspect burrow-prototype frontend -- /absolute/workspace/hoveld.sock SESSION_ID
```

The module/session setup is automated by the integration gate; this manual entry
point expects the session already to exist. The gate runs the same declared
frontend source with the pinned Python runtime. The frontend intentionally polls
and only displays the last 256 received bytes; no transcript policy is selected.

## Owner-selected direction

The owner selected a separate Burrow terminal app backed by Hovel's daemon and
clarified that "standalone" means no need to manage Hovel separately. Transparently
installing/managing Hovel as a dependency is acceptable; avoid separate SSH engines
or build variants. Exact dependency installation, default workspace, startup,
and quit behavior remain the setup/workspace decision.

The local management screen owns its terminal; Hovel owns operational state,
confirmation/planning and retained sessions. Burrow's SSH capabilities remain
available to Hovel chains independently of starting the UI. Squatter illustrates
shared client/provider behavior and public step/session contracts; its embedded
prompt's 80×24 fallback is not a full-screen resize proof, and installed-payload
records must not be copied for ordinary SSH login.

The embedded route's geometry and presentation-restoration gaps remain recorded
above. The public session/daemon contract still needs a solution for remote SSH
PTY initial size/resize; a separate management frontend does not solve it.

Sources at the pin: SDK `session.go`, `step.go`, `pty_session.go`, `pty_linux.go`;
`core/internal/adapters/cli/session_connect.go`; public daemon OpenAPI and
frontend documentation; Squatter `provider/main.go`, standalone
`client/cmd/squatterctl/main.go`, and `client/shell/prompt_pty_posix.go`.
No internal Hovel package is imported or changed.

This probe does not test SSH, Charm rendering, remote PTY resizing, remote
cancellation, visual history/redraw, slow-reader buffering, forced-signal terminal
restoration, daemon restart recovery, or simultaneous attachments. Original
package/lifecycle evidence remains preserved on `prototype/external-sdk-lifecycle`.
