# Disposable connection ownership proof

For [Can connection ownership and complete teardown be proven through Hovel?](https://github.com/Bochner/burrow/issues/25).
Run `aspect burrow-prototype transport` or `aspect burrow-check`.
Pins and host requirements are inherited from [the transport proof](README.md).

## Observed ownership and logging boundary

The packaged module starts its own foreground OpenSSH master and returns an SDK
session of kind `connection`. This exposes a small inert status control channel;
it is not an interactive SSH shell. Hovel retains the module after Run returns.
Two ordinary SSH clients and local, reverse and SOCKS listeners reuse its master.

- Explicit Hovel CloseSession ends the master, both clients, all three listeners,
  and master socket. The check tests processes and ports as well as the pathname.
- Killing the module initially left an orphan master. The preserved version uses
  Linux `Pdeathsig=SIGTERM` and holds the creating Go OS thread until child exit.
  SIGKILL of the module now ends the master, clients and listeners.
- Killing the daemon yields SDK input EOF. An explicit ServeIO-return cleanup
  wrapper closes the master; plain SDK EOF does not call session close itself.
- External master exit refuses subsequent reuse with `ProxyCommand=/bin/false`.
  No automatic authentication or tunnel recreation occurs.
- Unexpected client EOF preserves the master and sibling client. Two actual Hovel
  workspaces hold `gateway` concurrently; closing one leaves the other live.
  An explicit daemon restart exposes no live sessions or recreated tunnels; a new
  explicit operator throw reconnects and can then close normally.
- Atomic creation of an owner-only fixture directory rejects a same-name
  collision. Empty stale reservations are deliberately not automatically adopted
  or removed by a new invocation. Cleanup removes only the owned empty directory.
- Dangerous execution without explicit allowance refuses before master creation.
  Hovel stores confirmation IDs and timestamped diagnostics after Run returns.
  Artifact paths are workspace-relative, content-addressed, and hash checked;
  evidence remains intact across close/loss.

## Reproduced upstream failure and bounded workaround

Send 257 additional raw SDK log notifications after two initial milestones.
Exactly 256 total module logs are persisted. The next notification ends Hovel's
RPC reader with `module log notification count exceeds maximum 256`; the broker
reports the session closed even though the SSH master and listeners remain live.
CloseSession returns that protocol error. Its request can still reach the module,
so cleanup may execute despite the missing successful acknowledgment.

The production recommendation is to fix the upstream lifetime buffer without
making it unbounded. The disposable workaround allows 200 diagnostic milestones
per module lifetime, then emits one warning and suppresses subsequent milestones.
The gate sends 600 attempts, checks the warning, and verifies explicit close still
cleans up all resources. This sacrifices later diagnostics and is not a durable
audit guarantee. The raw `log-ceiling` command deliberately bypasses the budget to
preserve the upstream failure reproduction; it is a fixture-only command.

## Local terminal handoff

`local_terminal.py` is a disposable standard-library frontend driven by the gate
through a real controlling PTY. It uses ordinary OpenSSH clients over the retained
master. Management keys `1`/`2` open/select local shells, Ctrl-] returns to management,
and `q` quits. The check exercises:

- Initial 80×24 and live 120×40 dimensions observed through remote `stty size`.
- Independent PTYs: a background shell still reports 80×24 while the active one
  reports 120×40; selecting the background shell applies current dimensions.
- Continued execution while another shell/management is displayed, and bounded
  output draining: a 100,000-byte burst retains at most 65,536 bytes per shell.
- Return clears the display and replays the retained tail, with an explicit
  discarded-output notice. This proves plain-output replay only.
- A raw byte without Enter reaches remote `dd`; Ctrl-C interrupts remote sleep.
- Shell exit/reopen reuses the same master. Normal frontend quit reaps its local
  clients, restores exact termios and emits cursor/alternate-screen reset, while
  the daemon-owned master and forwarding listeners remain usable.
- Zero geometry reports an error without presenting a successful resize.
- Master loss during handoff returns to management; shell reopen cannot fall back
  to fresh authentication, and frontend exit still restores the terminal.

## Path and recovery contract exercised

The fixture layout is `<private workspace runtime root>/<name>/master`. The root
is supplied by its controlled daemon launcher, must be absolute, owned by the
current UID, mode 0700, and resolve without symlinks. Reservation uses atomic mkdir.
Names are 1–24 ASCII letters/digits/underscores/hyphens, starting with a letter or
digit. A socket pathname over 90 bytes refuses to leave space for OpenSSH's
temporary suffix. This is a conservative fixture limit, not a universal OS limit.

Two separate runtime roots support the same name; within-root collisions refuse
while the established master continues working. Invalid names, permissive roots,
symlink roots and stale socket reservations refuse. The stale-socket check verifies
the inode remains unchanged. The new invocation never auto-adopts or deletes an
unknown reservation. Harness teardown removes only fixture-owned files after
checking master termination; it is not an automatic stale-cleanup algorithm.

## Explicit acceptance limits

The outstanding local terminal limitation is arbitrary full-screen redraw:
replaying a truncated byte tail is not a VT screen model and can start inside an
escape sequence or UTF-8 character. The fixture demonstrates plain shell output;
production foreground/background use of full-screen applications must add a
supported terminal screen model and test its rendered state. Do not call this
full terminal parity or infer pixel-perfect restoration from emitted sequences.

The per-workspace root's final launcher/receipt placement and recovery UI remain
implementation choices; the proof requires a verified private root rather than
inventing another daemon registry. It verifies controlled SSH clients/listeners,
not cancellation of arbitrary remote jobs that deliberately daemonize or escape
the SSH channel. Script-run cancellation belongs to its separate proof.

The ownership ticket remains open for owner review of these concrete results and
limits. The offered Ubuntu host was not required: the daemon/logging fault is
local, and the loopback fixture provides repeatable remote geometry observations.
