# Pre-SSH connection delay profile — issue #77

Implementation evidence for [#77](https://github.com/Bochner/burrow/issues/77),
following the accepted #73 retained-manager checkpoint. The owner accepted this
result and its remaining limits on 2026-09-13; the check-placement decision is
recorded in [the connection-control decision](../adr/0001-manual-connection-approval.md).
Measurements are from the controlled lab on a shared development machine, not a
promise about real-server timing.

## Where the delay was

Phase tracing (`BURROW_PHASE_TRACE`, opt-in, phase names and monotonic
durations only) was added across the frontend, the daemon-launched throw
adapter and the retained manager, then the existing `aspect burrow ssh-latency`
harness recorded per-phase totals for four cold/warm pairs alongside its
untraced samples. Baseline commit `e593bdb` (tracer only, behavior unchanged).

Per warm connect at the baseline, medians across the phase-traced samples
(phases nest, so totals overlap and are not additive):

| Phase | Count | Total | What it is |
| --- | ---: | ---: | --- |
| `hovel-cli:module` | 1 | 0.86 s | Public Hovel CLI `module installed --json` inventory before every throw |
| `hovel-cli:throw` | 1 | 1.30 s | Public Hovel CLI confirmed throw; includes the adapter run |
| `call:GetModuleCatalog` | 1 | 0.32 s | Live catalog revalidation before every throw |
| `register-module` | 1 | 1.23 s | Cache copy, inventory CLI, catalog RPC: the two rows above plus verification |
| `status` | 40 | 1.58 s | Daemon receipt, process, socket, peer and `GetDaemonInfo` verification |
| `hash` | 80 | 1.54 s | SHA-256 of the 45 MB pinned Hovel executable, twice per `status`, plus own build |
| `call:*` (all RPCs) | 15 | 1.54 s | Each RPC verifies the daemon before dialing and again after the peer check |

Cold connects add one activation throw (`manager-throw:activate`) with its own
CLI launch and adapter start. Setup (`burrow status` launching a fresh daemon
and registering the module) is recorded separately and was not changed.

Dominant costs, in order: the Hovel CLI process launches (about 0.8 s each for a
daemon-backed command on this machine; a bare `hovel version` alone takes about
0.26 s), the per-connect module inventory and catalog revalidation, and the
repeated executable hashing inside about forty daemon verifications per
connect. Hovel IPC itself is cheap: `GetDaemonInfo` and session RPCs complete in
well under a millisecond each. Owner dispatch to the private prompt was
about 0.22 s, half of it the manager's own repeated hashing.

## Check placement decision and invalidation rules

Startup means `burrow status` or the terminal frontend opening a workspace,
including New/Open Workspace inside the TUI. An operation means one reviewed
connect, reconnect, list, inspect or close. Startup never connects or
authenticates; it only launches or reuses the verified daemon and registers this
build's module.

| Check | Placement | Reuse | Invalidation |
| --- | --- | --- | --- |
| Pinned Hovel package and executable content | Startup install; every CLI launch re-checks the installed executable's owner, mode, size and digest | The content digest is reused within one process while the opened file's device, inode, size, modification time and change time are unchanged | Any rewrite moves the change time (at the filesystem's timestamp granularity) and any replacement changes the inode, so the file is hashed again; a mismatch refuses as before. A same-user process is not a threat boundary here, as the accepted decision records. Nothing is persisted, so every new process hashes once. |
| Daemon identity: receipt, PID, boot, start ticks, executable pin, workspace/runtime/socket identity, owner-only socket, peer credentials, `GetDaemonInfo` | Fresh before every RPC and CLI launch, again after the peer check, at adapter start and on every reservation | None; each verification now costs about a millisecond | Daemon restart or replacement, a replaced socket or workspace directory, and a changed peer are refused exactly as before |
| Module registration: immutable cache copy, linked install, live catalog | Startup only | Per daemon launch. Every throw carries the frontend's own build digest in the immutable request chain, and the daemon-launched adapter refuses a different build before touching any reservation or manager | A build that was never registered fails at `AddModule` with guidance to run `burrow status`; an adapter from another build refuses with the same guidance and dispatches nothing. The retained manager keeps the build that activated it until it is closed, as before. |
| Reservations, request/preview binding, SSH config resolution, manager identity, authentication | Fresh per operation, unchanged | None | Unchanged: changed settings require a new recap, and unidentified resources are never adopted |

Concurrent frontends and workspace switching need no shared state: reuse is
process-local and keyed by file identity, and every daemon verification remains
fresh. Long-running TUI use benefits most from startup registration; standalone
CLI use still verifies the daemon on every command and hashes each executable
once per process.

This moves the live-catalog revalidation that
[the connection-control decision](../adr/0001-manual-connection-approval.md)
mentions from every throw to workspace open, with the adapter's exact build
check as the fresh per-throw substitute. The owner accepted that
reinterpretation on 2026-09-13.

Rejected for now: a persistent digest cache (no evidence it is needed once
per-process reuse exists), fewer verifications per RPC (each is now cheap), and
replacing the confirmed CLI throw with raw RPC (excluded by
[the connection-control decision](../adr/0001-manual-connection-approval.md)).

## What changed

- `core/launch` reuses verified executable digests per process by file identity
  and applies the same private-file checks when only the digest is needed.
- Module registration stays at startup; `connectManaged` no longer re-runs the
  inventory CLI and catalog RPC per connect. Throws send the frontend build
  digest as chain configuration and `runManager` refuses another build before
  dispatch. `AddModule` failure explains that the build is not registered.
- The SSH gate asserts, from the same trace, that a warm connect still verifies
  the daemon at least ten times and runs exactly one throw, hashes at most four
  executables (frontend and adapter, Hovel and own build) and never re-reads
  the module inventory. A unit test proves the digest reuse and invalidation
  rules directly.

## After

Measured on 2026-09-13 with the same harness, fixture, machine and build mode
before and after, in immediate succession; both runs completed all 40 untraced
samples, four executable-start traces and eight phase-traced samples.
[Baseline samples](connection-latency-baseline-measurements.json) record binary
`00ccf27760246cbafef62c895d1810c1e846f4db445afba788690a80420ad662` at commit `e593bdb`; [result samples](connection-latency-measurements.json)
record binary `568baa0f886b3d23cbb12a9c9e849ac424e0b3e5f4344654ce58e065ed2806bb`. Initial load averages were
0.19, 0.68, 1.39 before and 1.93, 2.13, 1.90 after.
Password samples answer the first private prompt after 20 milliseconds.

| Phase, seconds | Before p50 | Before p95 | After p50 | After p95 |
| --- | ---: | ---: | ---: | ---: |
| Cold: Submission to owner dispatch | 5.179 | 5.223 | 2.285 | 2.403 |
| Cold: Submission to observed connected | 5.476 | 5.557 | 2.476 | 2.580 |
| Cold: Owner dispatch to private password prompt | 0.216 | 0.224 | 0.102 | 0.104 |
| Warm: Submission to owner dispatch | 3.673 | 3.692 | 1.300 | 1.308 |
| Warm: Submission to observed connected | 3.957 | 4.015 | 1.418 | 1.520 |
| Warm: Owner dispatch to private password prompt | 0.216 | 0.219 | 0.101 | 0.102 |

Setup is reported separately so moved work cannot hide there (20 samples; p50
and maximum):

| Startup, seconds | Before p50 | Before max | After p50 | After max |
| --- | ---: | ---: | ---: | ---: |
| Setup: fresh daemon launch and registration (`burrow status`) | 1.954 | 2.006 | 1.495 | 1.549 |

Per-phase medians from the phase-traced samples, count (total), warm and cold:

| Phase | Warm before | Warm after | Cold before | Cold after |
| --- | ---: | ---: | ---: | ---: |
| Executable hashes | 80 (1.54 s) | 4 (0.05 s) | 110 (2.11 s) | 6 (0.07 s) |
| Daemon verifications | 40 (1.58 s) | 42 (0.06 s) | 56 (2.15 s) | 58 (0.09 s) |
| Inventory CLI launches | 1 (0.86 s) | 0 | 1 (0.82 s) | 0 |
| Catalog RPCs | 1 (0.32 s) | 0 | 1 (0.31 s) | 0 |
| Throw CLI launches | 1 (1.30 s) | 1 (1.02 s) | 2 (2.44 s) | 2 (1.98 s) |
| Connect throw, end to end | 1 (2.08 s) | 1 (1.04 s) | 1 (2.10 s) | 1 (1.03 s) |
| Whole CLI connect process | 1 (4.23 s) | 1 (1.23 s) | 1 (5.75 s) | 1 (2.20 s) |

Successful `execve` starts across the frontend and daemon descendants, one
trace per combination:

| Authentication | Cold before | Warm before | Cold after | Warm after |
| --- | ---: | ---: | ---: | ---: |
| Key | 18 | 14 | 14 | 10 |
| Password | 22 | 18 | 18 | 14 |

List and selected close while a private prompt is unanswered remain covered by
the SSH gate's prompt check; the retained-manager and consumer proofs are
unchanged.

## Remaining limits

- Each remaining Hovel CLI launch costs about 0.8 s on this machine for a
  daemon-backed command, and a bare `hovel version` about 0.26 s, before any
  Burrow work. One confirmed throw per connect (two for a cold connect) is
  required by the accepted decision, so this floor is upstream. It relates to
  [#28](https://github.com/Bochner/burrow/issues/28) only insofar as a documented
  daemon compatibility contract would be a prerequisite for any future throw
  coordination that avoids a CLI process; no Hovel change is proposed here.
- The throw adapter is a fresh process per throw and verifies the daemon and
  its own build from scratch; that is deliberate freshness, not waste.
- A standalone CLI connect from a build that was registered by a different
  build spends one real confirmed throw, refused by the adapter before dispatch,
  and leaves that refusal in Hovel history. It is a development scenario; the
  cheap fix is `burrow status`. A local pre-check cannot prove what the daemon
  has registered, so none was added.
- All samples are standalone CLI processes. The terminal frontend shares the
  same connect path and additionally keeps its registration from workspace
  open, but no in-TUI timing was recorded.
- The after run saw higher load averages than the before run; that understates
  rather than inflates the improvement.
- Real-server authentication, network and human review time are outside every
  number above.

## Validation

`aspect test //core/launch:digest_test` passes. The first full `aspect burrow-check`
after the change failed the SSH acceptance lab in the ProxyJump scenario with
OpenSSH reporting `Connection closed by 127.0.0.1` before any banner; the #73
checkpoint passed in the same environment. Bisecting showed neither the
registration change nor tracing caused it: the pinned OpenSSH 10.3 server's
default per-source penalties block a client address after rapid authentication
failures, the lab deliberately fails three passwords immediately before that
jump, and the faster client now crosses the penalty threshold the slower
checkpoint stayed under. Reproduced with plain `ssh` against the container. The
disposable fixture now sets `PerSourcePenalties no` next to its existing
forwarding edit; that server defense is not under test. With that fixture change
the SSH lab passed in 59 seconds, against 255 seconds at the checkpoint, and the
new SSH-gate assertion on hash count, verification count, single throw and
absent inventory passed in every run. One full-gate run in between timed out
once in the lab's SIGTERM-cancellation scenario (the private CLI did not exit
within the lab's 20-second bound); it passed in every other run and did not
reproduce, so it is recorded here as an unexplained single timeout rather than
attributed to this change. The final `aspect burrow-check` result is recorded
in the commit message. Standards and Spec reviews raised no outstanding hard
findings; their judgement calls are reflected above.

## Reproduce

```sh
aspect burrow ssh-latency
aspect burrow ssh-check
aspect test //core/launch:digest_test
aspect burrow-check
```
