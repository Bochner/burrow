# General-use Track C: measured performance changes

Issue [#98](https://github.com/Bochner/burrow/issues/98), rows 21–25, on
`audit/general-use-architecture`. Baseline:
`f714ea5990a19c2f4d7627893776330b22349cc8`, after rows 19/20 and Tracks A/B.
The owner requested only checks required by this change. These are bounded local
measurements, not network-wide latency or CPU guarantees.

## Row decisions

**21 — implemented.** The existing public owner `snapshot` command accepts an
optional screen revision after the history offset. A matching revision waits
at most one second; an older revision returns immediately. Output, control,
geometry, audit errors and lifecycle changes broadcast to independent waiters.
Registration and revision comparison share the mutation lock; waiting releases
that lock. Snapshot rendering, output/history limits and independent positions
are unchanged. No new public module, input route or authority is introduced.

The TUI allows one observation in flight per attachment. Input, focus changes
and detach cancel and join it before continuing; focus and control operations
still obtain immediate fresh observations. Canceled observations cannot clear
the pending focus refresh or present cancellation as owner loss. Exact session,
module/kind/name, workspace and fresh daemon/Unix-peer checks remain on each
observation. The 50 ms visible scheduling ceiling and one-second hidden cadence
remain; an older owner omitting revisions uses the former polling behavior.

The pinned Hovel SDK dispatches commands concurrently but does not pass a
cancellation context into `RunPayloadCommand`. Local cancellation closes the
RPC connection immediately; abandoned provider waits expire within one second.
At most 32 waits per shell sleep concurrently; excess readers receive an
immediate snapshot. This is bounded cancellation, not a claim that the SDK now
propagates cancellation. See the pinned public SDK `server.go`/`session.go` and
the [current dispatch documentation](https://github.com/vibepwners/hovel/blob/main/sdk/go/hovel/server.go).

**22 — deferred.** Fresh startup samples retain 19 setup RPCs per shell. Their
median cumulative cost is 36.24 ms for three CLI samples, 40.07 ms for the first
TUI shell (one sample), and 43.26 ms for two additional TUI shells. Corresponding
system startup observations are 551.80, 524.29 and 550.76 ms. Removing some setup
calls cannot eliminate the rest of startup. Keep prepare/adopt/start and the
shared `managerThrow` unchanged; an immutable-chain experiment needs a larger
representative setup cost and matched improvement before adding configuration,
concurrent-request, confirmation/audit and failed-adoption lifecycle changes.

**23 — no change.** Ordinary warm CLI SFTP setup is 6.47 ms within 88.24 ms total
before this track's file change. Retaining a client would add cancellation,
exact-owner replacement, close/reconnect and sibling-isolation responsibilities
for a small local saving. Revisit with representative high setup costs. Each
browse continues to own and close its own SFTP subsystem.

**24 — implemented.** The link-heavy fixture spends 41.40 ms in 32 serial link
metadata lookups, a material portion of its 133.93 ms CLI listing. Four workers
now share that request's existing SFTP client. Entries are validated first;
each worker writes only its own entry; all workers finish before enrichment or
sorting. Cancellation stops new lookups and the existing request-owned SSH
cleanup unblocks outstanding reads. Errors remain visible, ordering and metadata
are preserved, and tree traversal still refuses to descend through links.
The pinned `pkg/sftp` v1.13.11 explicitly permits concurrent client calls;
its synchronized packet routing and error broadcast handle the shared client.

**25 — no change.** The one ordinary cold account lookup takes 10.29 ms; warmed
lookups are effectively zero. Three unavailable-account samples have 6.76 ms
median enrichment and numeric fallback. The injected slow case still spends
2001.51 ms at the existing two-second bound. That is a controlled fault, not
evidence of slow NSS in ordinary use. Keep the bounded lookup, five-minute
freshness and stable numeric/name presentation until real slow lookups justify
a different presentation or timeout contract.

## Measurements

All runs used the existing declared Aspect labs, the pinned Docker/OpenSSH
fixture and one approved master. No competing benchmark, build or test workload
ran during measurement windows. Raw reports contain workload/pins, source and
binary hashes, individual samples, phase boundaries, errors and cleanup results.

Attachment counts below are completed snapshots in each five-second visible
window. Two/four attachments have one visible shell and the remaining shells
hidden. CPU is percent of one logical CPU, frontend only; it excludes Hovel,
SSH and the decoder. There is one sample per combination, so the CPU observations
are descriptive, not a stable performance guarantee.

| Attachments | Workload | Snapshots before → after | Frontend CPU before → after |
| --- | --- | --- | --- |
| 1 | idle | 101 → 5 | 36.6% → 15.7% |
| 2 | idle | 105 → 10 | 36.6% → 16.3% |
| 4 | idle | 115 → 20 | 41.9% → 19.0% |
| 1 | 10 lines/s | 100 → 49 | 37.4% → 26.2% |
| 2 | 10 lines/s | 106 → 54 | 36.6% → 30.5% |
| 4 | 10 lines/s | 115 → 64 | 39.6% → 30.9% |

The one-shell idle snapshot payload falls 470,660 → 23,365 bytes per window.
All 18 before and 18 after cases passed, including hidden/background workspaces,
return-to-tab input/output, unchanged controller generations, release and final
cleanup. Hidden/background observations retain approximately one snapshot per
second per attachment. These are snapshot JSON payload bytes, not wire bytes.
Raw evidence: [before](performance-track-c/attachments-before.json),
[after](performance-track-c/attachments-after.json).

File measurements include 22 samples per variant: three each of CLI cold/warm,
32 links, injected slow/missing accounts and TUI cold/warm, plus one initial cold
account/history browse. CLI median values:

| Workload/phase | Before | After |
| --- | --- | --- |
| 32-link listing, total | 133.93 ms | 101.94 ms |
| 32-link listing, owner | 56.94 ms | 26.26 ms |
| Ordinary warmed listing, total | 88.24 ms | 84.86 ms |

Every link listing still performs one SFTP setup, one directory open, 32
`readlink` requests and 32 `stat` requests, with all 33 entries returned. Per-link
phase sums overlap after parallelization (41.40 → 44.60 ms); they are not elapsed
listing time. The ordinary difference is not attributed to parallel links.
Both variants have zero failures/timeouts. The file baseline already contained
the screen-wait implementation but its file path was unchanged from `f714ea5`;
this workload opens no retained shells. Evidence:
[before](performance-track-c/files-before.json),
[after](performance-track-c/files-after.json),
[current startup](performance-track-c/startup.json).

Reproduce separately, setting `TEST_UNDECLARED_OUTPUTS_DIR` to an existing
artifact directory for each invocation:

```sh
aspect burrow attachment-cost -- --phases --samples 1
aspect burrow file-latency -- --samples 3
aspect burrow shell-latency -- --phases --samples 3
aspect burrow interaction-latency -- --samples 10
```

A separate 20-interaction check (ten controller keys and ten CLI inputs observed
in the TUI) completed without failures. Input-to-visible medians were 38.29 ms
and 100.97 ms; maxima were 67.14 ms and 117.04 ms, respectively. This checks that
waiting does not impose a one-second input stall; there is no matched latency
speedup claim. [Raw interaction samples](performance-track-c/interaction.json).

## Verification

The existing real CLI/daemon/provider seam failed on the baseline's missing
revision, then passed bounded timeout, independent broadcast readers, output
before wait registration, invalid revisions/offsets, takeover/geometry and
stale authority. The lab also waits during close racing with backpressured
input. Existing shell checks cover history gaps, loss, retained lifetime and
cleanup. The file lab adds 32 directory symlinks, broken-link errors, stable
ordering, no traversal through links, and cancellation during throttled link
metadata followed by successful browsing without a canceled history entry.

The first combined affected gate ran Go/Python format, Python lint, interaction,
SSH shell and SSH file targets. Five passed; the shell target stopped at an old
assertion requiring fast visible-idle polling. Updating it to require 1–3
observations over 1.2 seconds retained a regression check for excess idle work.
The next shell run exposed a real selection race: clearing `refreshPending` at
refresh start refused navigation against cached alternate-screen modes while
the selection RPC was still in flight. The fix clears it only when a fresh
response is applied, fenced to that selection's visibility revision. The existing
stopped-owner/queued-navigation scenario catches this regression.

Performance samples above precede this final readiness correction; steady-state
wait cadence and file workers are unchanged. They are measurements of that
candidate, not newly measured timings for the corrected binary. The subsequent
behavior checks verify the correction separately. Earlier failed logs remain
in the artifact directory; passing later checks do not erase them.

Final affected verification is complete:

- `go_format_test`, `python_format_test`, `python_lint_test`, `interaction_test`
  and `ssh_files_test` passed. The initial combined invocation took 2m 0s;
  interaction took 55.8s and files 73.1s. Python checks passed again after the
  polling assertion update; Go format passed after the final readiness repair.
- The owner/session portion of `ssh_shell_test` passed, including the added
  timeout, independent-wakeup, control/geometry and close-wait checks, before
  its later TUI failure. The owner implementation did not change afterward.
- `aspect burrow shared-tui-check` passed on the corrected source in 1m 9s.
  Fresh hidden-mode navigation took 0.182s; idle observations were one visible
  and one hidden over each 1.2s window. Full takeover, observer isolation,
  scrollback/gap recovery, two-shell full-screen applications, resize,
  input/cancellation, detach/Keep running, owner loss/replacement, teardown,
  terminal restoration and database integrity passed.

The failed shell target was followed by its focused shared-TUI workflow, rather
than repeating already-passed owner/forward checks. This is combined affected
behavior evidence, not a claim of a final green full `ssh_shell_test` invocation.
See [initial gate](performance-track-c/focused-initial.log),
[focus regression](performance-track-c/shell-focus-failure.log),
[corrected TUI](performance-track-c/shared-tui-final.log),
[file behavior](performance-track-c/files-check.log) and
[final format](performance-track-c/final-format.log).

Independent reviews, including a second review of the readiness correction:
**Standards: 0 findings; Spec: 0 findings.** No further runtime change followed
the final checks. Full release preflight, unrelated SSH partitions and advisory
#86 diagnostics were not rerun. The exact three-diagnostic #86 exception and
deferred #65 owner walkthrough are unchanged. No ready implementation child
remains under #98; #65 remains owner-deferred, not newly started by this track.
