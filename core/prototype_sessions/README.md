# Shared interactive sessions — disposable #87 proof

Status: **bounded proof, awaiting owner review**. This is not production session
parity. Work stays on `mvp5`; the production frontend and module behavior are
unchanged. [Issue #87](https://github.com/Bochner/burrow/issues/87) requires owner
acceptance and published migration dependencies before resolution.

Run the real SSH/daemon/terminal check:

```sh
aspect burrow-prototype sessions-check
```

Requires the existing Docker/OpenSSH prerequisites. The declared test creates
one digest-pinned disposable SSH server, synthetic key, private scratch workspace
and pinned Hovel daemon, then cleans them up. It never uses the operator's live
workspace. If `/tmp` is full, use
`aspect test --bazel-flag=--test_env=TMPDIR=/var/tmp //core/prototype_sessions:check`.
The target enters the full and shell acceptance gates automatically.

`test.log` and `test.outputs/outputs.zip` under
`bazel-testlogs/core/prototype_sessions/check/` preserve results and three terminal
captures: ordinary attach, alternate-screen detach and the actual Burrow observer.
Run the check again to reproduce them. Passing a GAP assertion means the
limitation was reproduced, not that the capability passed.

## What is exercised

The proof installs a scratch-only extension of the **same `burrow@0.1.0` module**.
It reuses production `connection.ShellCommand`, its verified existing manager
and its OpenSSH master. No second registry, daemon, private import, new login or
production routing exception is introduced. Two native PTY sessions and one
typed candidate share that connection. Session creation uses an ordinary
confirmed Hovel throw; controls use public daemon/session-command contracts.

Production currently rejects unfamiliar Burrow session kinds as incompatible
owners. The proof therefore prepares all three verified SSH commands before
registering the sessions. That is a bounded lab arrangement, not a production
discovery policy. The existing Hovel tab can still observe a native session;
the management inventory truthfully becomes unverified.

## Recorded behavior, 2026-09-20

The focused real-fixture check passed. The initial red check failed because the
module did not open a shell; the additional typed-candidate check failed before
that session existed. Go compilation is part of every relevant Aspect check.
The [source investigation](../../docs/research/shared-session-contract.md)
records exact SDK/runtime/source provenance and the #29/#34 follow-ups.

The full `aspect burrow-check` run passed 36 of 37 targets; only the existing
`//core/launch:hovel_wal_test` failed with the known [#86 WAL lifetime-lock
defect](https://github.com/Bochner/burrow/issues/86). The focused proof was rerun
after review strengthened the remote-close and terminal-restoration assertions.

| Check | Observed outcome |
| --- | --- |
| Native headless commands | Real `hovel run … -- session send/read/tail/call … --json` works without a controlling TTY. Registration alone was not counted. |
| Same SSH transport | Three shells reuse the verified master's socket; explicit shell close preserves the sibling and original master. |
| Human observation | A separate native terminal and the existing Burrow Hovel tab display input/output submitted by the headless agent. |
| Native readers | Reader A consumes the output that reader B expected. Non-consuming tails repeat history and lack positions. |
| Bounded tail workaround | Separate client positions work below an explicit 64 KiB ceiling; the candidate refuses to continue at that ceiling. |
| History rollover | After 11,000,000 output bytes, native history is the final 10 MiB, without offset/epoch/gap metadata. Older output is gone. |
| Geometry | Native SDK PTY starts at `0 0`. The second/third proof shells explicitly start at 80×24. A public `resize` command changes the real remote `stty size`; invalid geometry is refused. Native attach does not forward window changes. Fixed proof sizing is not native attach negotiation. |
| Concurrent input | Native inputs from two clients can splice into one shell command; there is no controller arbitration. |
| Terminal use | `top`, remote Ctrl+C, independent shells, detach/reattach and same remote PID/shell-variable retention pass. |
| Frontend exit | Ending the Burrow frontend restores its terminal and leaves the retained shell alive. This is the embedded-Hovel path, not a production SSH-tab migration or quit-review implementation. |
| Native restoration | Clean native detach restores termios but leaves an explicitly enabled alternate screen and hidden cursor active. |
| Closed input | The SDK still acknowledges a write to a closed PTY. An input acknowledgement is not execution success. |
| Loss | Killing the master closes the retained shells; the typed candidate rejects further input. No fallback login occurs. Explicit cleanup removes shell records and production inspection reports the original connection lost. |

The stronger close check initially found a live remote bash 15 seconds after
SDK-only close removed the Hovel record. Further remote PTY output allowed it
to exit, consistent with the SDK's blocking master read retaining its descriptor.
The module now explicitly kills and waits for **its own SSH subprocess** before
closing SDK resources, as production Burrow already does for local terminals.
The final check requires the remote PID to disappear while its sibling and
master remain. This is a Burrow subprocess-ownership fix, not a request that
Hovel infer ownership of arbitrary children started by a frontend callback.

## Typed candidate that passed

The third session implements a bounded protocol through the already-public
`RunSessionCommand` / `PayloadCommandProvider` extension. Its sole terminal
history lives in that session; native reads return no bytes and native writes
are refused. Hovel still owns the session registry, process retention and close.
This avoids both competing native reads and a second daemon-owned transcript.

| Command | Contract demonstrated |
| --- | --- |
| `observe CURSOR` | Independent monotonic byte positions; data, oldest position, next position, closed state, controller label and generation. A stale cursor returns an explicit gap and no pretend screen. Reads never advance another viewer. |
| `control EXPECTED_GENERATION LABEL COLS ROWS` | Explicit compare-and-change takeover with validated geometry and a new opaque input token. Stale takeover requests fail. |
| `input TOKEN BASE64` | Only the current token can submit 1–4096 raw bytes, including Ctrl+C. Returns accepted byte count, not command result. |
| `resize TOKEN COLS ROWS` | Only the current controller may resize; each dimension must be 1–1000. |
| `release TOKEN` | Leaves the shell running, clears control and invalidates the old token. A later explicit claim can resume it. |

The check proves separate reader positions, actual geometry, takeover/release
fencing, raw-route bypass refusal, Ctrl+C, reattachment and an explicit overflow
gap after 70,000 bytes. It operates these commands from separate no-TTY CLI
processes using JSON stdin; control tokens are not put into command arguments
or logs. Same-user workspace access is not a new security boundary.

Limits remain explicit: 64 KiB byte history is not a VT snapshot. A new/slow
viewer after overflow needs screen resynchronization; this proof reports a gap
instead of fabricating a usable screen. The SDK's intermediate output queue is
unbounded and writes can block; the candidate is not a production memory or
backpressure solution. Its control survives a client disappearing until an
explicit takeover; no lease or automatic controller recovery was proven.
The native TUI-observation test and the typed-control test are distinct paths;
a typed session is not yet rendered by Burrow's SSH tabs. Daemon/module restart
recovery, sustained-load behavior and a normal quit-review UI are not proven.

## Concrete owner review

Recommended direction: adopt the **typed public-session-command approach** for
production planning, keeping native raw attach as negative compatibility evidence.
Use one explicit controller; observers never resize or send input. Takeover
invalidates old input tokens. Normal detach releases control and retains the
shell. If a controller disappears unexpectedly, its claim remains until an
explicit takeover; the displayed claim is not a liveness assertion. Do not
invent automatic recovery. Keep running retains
shells; Close shell preserves its connection/siblings; Close connection ends
all dependent resources; Cancel changes nothing. Output gaps must be visible.
Master/module/daemon loss never silently reconnects or claims remote command
completion.

Accepting retained shells changes [#24's frontend-local lifetime decision](https://github.com/Bochner/burrow/issues/24):
today SSH shells end when Burrow exits. It does not promise survival after the
daemon or owning module is lost. The wider policy examples are in the
[source note](../../docs/research/shared-session-contract.md#concrete-behavior-to-review-with-the-owner).

## Draft migration slices — not approved or published

1. **Retained shell ownership and public controls.** Register a recognized shell
   kind in the single base module, bind it to exact manager/connection creation,
   and expose CLI create/inspect/close. Admit subsequent shells normally; enforce
   connection-wide teardown and sibling preservation. Include bounded PTY input,
   output backpressure, process cleanup and truthful loss states. Blocked by
   accepted #87 contract, not merely a passing proof.
2. **Shared control and independent terminal observation.** Build the approved
   token/generation/cursor contract at the real shell owner. Reject stale control,
   validate initial/live geometry, surface gaps, and prove a VT snapshot/replay or
   explicit redraw/resynchronization contract after truncation. Prove close/input
   races, slow readers and controller disappearance. Depends on slice 1.
3. **Burrow TUI attachment and quit review.** Use slice 2 in existing SSH tabs,
   show controller/synchronization state, provide observe/takeover/detach, preserve
   Catppuccin/NO_COLOR and isolate terminal controls. Check two full-screen shells,
   initial/live resize, all exits/restoration, keep/close/cancel and loss. Depends
   on slices 1–2. No production terminal migration is included in this proof.
4. **Shared-session activity and agent workflow acceptance.** Integrate actual
   session routes with #89's CLI lifecycle, #90's follower and #64's skill/external
   agent exercise. Distinguish submitted input, provider result and terminal I/O;
   do not claim a raw shell command has a structured exit result. Depends on
   slices 1–3 and the relevant CLI/follower tickets.

After owner review, publish accepted slices with native dependencies and make
them native blockers of #64 before resolving #87. Keep #64 `needs-info` until
then. The proven typed route does not establish that upstream changes are
mandatory; remaining native gaps stay in Burrow for the owner, with #29/#34 as
existing handoff context. Confirmed native defects remain in Burrow's Hovel
handoff tickets; reader, controller, resize and subprocess-ownership work that
Burrow can implement is not filed as an upstream bug. No unapproved production
tickets were published.
