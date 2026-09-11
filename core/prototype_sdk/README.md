# Disposable terminal placement proof

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

## Placement implication, pending owner review

A full-screen management UI inside the current SDK PTY session is not proven
viable: public initial-size/resize propagation and detach presentation cleanup
are missing. Neither a hard-coded size nor an undocumented in-band escape
protocol is accepted as a workaround.

A separate local Burrow frontend using Hovel's documented daemon HTTP/JSON API
over its owner-protected Unix socket is the candidate that avoids those local
management-screen gaps. That candidate has not been implemented or runtime
proven here. It still requires a bounded frontend proof if selected. It also does
not solve remote SSH PTY resize, which remains a public API gap for interactive
remote shells.

Sources at the pin: SDK `session.go`, `pty_session.go`, `pty_linux.go`;
`core/internal/adapters/cli/session_connect.go`; public daemon OpenAPI and
frontend documentation. No internal Hovel package is imported or changed.

This probe does not test SSH, Charm rendering, remote PTY resizing, remote
cancellation, visual history/redraw, slow-reader buffering, forced-signal terminal
restoration, daemon restart recovery, or simultaneous attachments. Original
package/lifecycle evidence remains preserved on `prototype/external-sdk-lifecycle`.
