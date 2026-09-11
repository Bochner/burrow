# Hovel lifecycle and terminal contracts

Research baseline: 2026-09-11, Hovel `c461ba282a8aecc7aa3a079a4613bf5e2640c388`.
The GitHub API reported the same `main` head during this investigation. This is
source research for [Which Hovel lifecycle and terminal contracts can Burrow rely on?](https://github.com/Bochner/burrow/issues/3), not a live integration proof or a UI decision.

Subsequent evidence: the [terminal and setup proofs](prototype-evidence.md)
demonstrated local frontend placement and inert detach/reattach behavior. The
alternatives below are historical research; the linked decisions record the
selected direction, and the proofs retain their untested boundaries.

## Findings

**Hovel supports a module-owned session surviving operator detach. Its current
public session contract does not carry terminal dimensions or resize events.**
Keep the full-screen UI placement decision open until the terminal proof.

| Boundary | Documented/public contract | Observed implementation and limit |
| --- | --- | --- |
| Module process | Handshake, schema, execute, shutdown over framed JSON-RPC; sessions keep the process alive. | `Runner.Run` launches a process independently of its bounded execute context, adopts returned sessions, otherwise requests shutdown. Failure before adoption kills the process. |
| SSH channel/session | SDK `Session` exposes Open, Write, Read(wait), Close(reason), Closed; register through `Context.OpenSession`. | Registry holds session objects after Run returns. A returned session must own any SSH resources it needs; Run-scoped deferred cleanup would end them prematurely. |
| Detach | Session bytes are brokered independently of an operator frontend. | CLI Ctrl-] and input EOF stop attachment without calling CloseSession. Attach resumes reading and can replay daemon history. Detach does not mean daemon restart recovery. |
| Close | Explicit CloseSession delegates session/close. | Broker removes the closed session; when no other tracked session uses its process, it requests shutdown with a five-second bound, then kills/waits if needed. A close RPC error leaves the session tracked. |
| Cancellation | SDK Context is a module input/service container, not Go context.Context. | SDK ignores notifications including cancel; RPC caller context cancellation stops waiting, not the SDK handler. Shutdown waits for outstanding handlers before closing registered sessions. |
| Terminal bytes | Session Read/Write transport bytes; optional terminal.pty capability identifies local PTYs. | Existing CLI converts carriage returns for non-PTY sessions; PTY sessions preserve input bytes except the reserved detach byte. CLI output can normalize LF to CRLF. This is not a proven transparent terminal transport. |
| Terminal geometry | Session interface has no size/resize member. | Dispatcher and daemon OpenAPI have no session resize method. Linux PTY allocation does not initialize window dimensions. |
| Platforms | Package/release operator targets are separate from remote target platforms. | Hovel release task targets Linux amd64/arm64, macOS arm64, Windows amd64/arm64; SDK local PTY helper is Linux-only. |

Sources: [module lifecycle documentation][module-doc], [SDK Session][session],
[SDK Context][context], [SDK server][server], [runner and session broker][runner],
[CLI attachment][cli], [daemon OpenAPI][openapi], [release task][release],
[Linux PTY allocator][pty-linux], [unsupported platforms][pty-other].

## Lifetime details that affect the specification

The documented successful path is `Run → returned SessionRef → retained module
process → session I/O → explicit close`. Merely opening a socket or a goroutine
without a returned adopted session does not establish retention. The broker
continuously pumps session output even with no attached CLI; its session map and
history are in memory. Existing tests establish bounded daemon history, tail
consumption, and local pump-context cancellation. They do not demonstrate Burrow
SSH cleanup or restart recovery. [Runner][runner], [history tests][history-tests],
[module guidance][module-doc].

A session becoming closed during a read follows a different source path from
explicit close: `pumpSession` calls `markClosed`, which marks local state and stops
the pump; it does not remove the session or invoke last-session process shutdown.
The later ownership decision must account for this distinction rather than
assuming remote EOF has identical cleanup semantics. [Broker][runner].

The SDK waits for outstanding requests on stdin EOF, but that EOF path does not
call `closeAll`; an explicit shutdown does. Consequently, neither pipe loss nor
an ignored cancel notification proves cooperative resource cleanup. A stuck
handler can delay SDK shutdown; the runtime's bounded kill fallback does not
run Go defers. SSH operations need their own deadlines and close semantics.
These are implications of the inspected code, not newly promised upstream
behavior. [SDK serve loop][server], [process shutdown][runner].

The SDK PTY adapter starts a frontend with explicit reader/writer streams on a
local PTY and queues its output. That queue has no configured bound in the
adapter; daemon history bounds do not bound this upstream queue. PTY close
closes file descriptors but supplies no cancellation context to the frontend
callback. A frontend that continues unrelated work after descriptor closure
needs its own lifecycle handling. A local PTY is also distinct from an SSH
server-side PTY request. [PTY adapter][pty].

## Viable UI placements, pending proof

1. **Hovel CLI plus structured Burrow module operations and SSH sessions.** This
   uses the existing session path and postpones full-screen terminal requirements;
   it does not by itself satisfy the requested Charm management interface.
2. **Charm frontend inside SDK PTYSession.** Public adapter exists on Linux and
   separates UI bytes from module stdout. Initial size, resize, input handling,
   redraw on reattach, output bounds, and callback termination remain proof gaps.
   A fixed-size fallback would be a limitation requiring an explicit decision.
3. **Separate Charm frontend using Hovel daemon HTTP/JSON contracts.** Frontend
   owns the real terminal and its resize events while Hovel retains operational
   state. Public daemon contract exists, but the current Go client is internal;
   Burrow must use the public wire contract rather than importing it. This removes
   the local management UI's dependency on SDK PTY resizing, but does not invent
   remote SSH session resize support.

These alternatives follow [frontend documentation][frontends], [daemon RPC
documentation][daemon-doc], [SDK PTY adapter][pty], and [daemon OpenAPI][openapi].
Hovel's own `tui` role is explicitly unimplemented; this research does not choose
between embedded and separate Burrow UI or approve changes to Hovel core.

## Smallest follow-up proof

The existing package-lifecycle and terminal-proof decisions should exercise one
retained session with two attachments: detach preserves it, explicit close
releases its resources, and closing one of two sessions preserves the other.
Include remote EOF, failed close, a bounded stuck operation, module pipe loss,
and daemon exit observations. Record daemon-restart behavior separately without
promising recovery. For the UI path, verify real initial geometry and resize,
raw input/Ctrl-C/Ctrl-], terminal restoration, redraw after detach, and output
behavior under a slow reader. These are proposed acceptance checks, not results.

Validation for this research: source and existing test inspection, upstream head
verification, and `aspect build //:research` metadata gate. No Hovel/SSH runtime
or terminal integration test was executed; the metadata gate does not validate
these behavioral claims or prose.

[module-doc]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/module-development.html#L97-L115
[session]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/session.go#L80-L95
[context]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/context.go
[server]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/server.go#L53-L107
[runner]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/moduleruntime/pythonrpc/runner.go
[cli]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/cli/session_connect.go
[openapi]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/spec/reference/daemon-rpc.openapi.json
[release]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/.aspect/release.axl#L1-L12
[pty-linux]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/pty_linux.go
[pty-other]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/pty_unsupported.go
[history-tests]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/moduleruntime/pythonrpc/session_history_test.go
[pty]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/pty_session.go
[frontends]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/front-ends.html
[daemon-doc]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/daemon-rpc.html
