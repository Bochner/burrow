# Hidden retained-shell polling (#109)

Implementation and acceptance for [#109](https://github.com/Bochner/burrow/issues/109),
the owner-authorized follow-up to [#98 row 19](https://github.com/Bochner/burrow/issues/98).
Work began at `524c51c2e930b23e7dfa221fa268c4d630aacc34` on
`audit/general-use-architecture`. The completed initial #98 scope remains closed.

## Change and boundaries

Selected retained-shell attachments keep their 50 ms snapshot interval. Hidden
tabs, including tabs in background workspaces, refresh once per second. Every
frame update synchronizes visibility from the selected workspace/tab; selection
wakes the existing serialized attachment worker immediately. The initial attach
still loads a verified snapshot. Shared-shell frontend sampling now waits for
a buffered local update notification instead of repeatedly copying the same
cached observation every 30 ms. Refresh success/failure, input results and
release visibility wake the viewer; waits honor attachment and frontend shutdown.
Local PTYs retain their existing 30 ms sampling. This is an in-process
notification, not a new owner RPC or screen-wait protocol.

Selection and input can arrive together. A pending selection refresh precedes
input encoding and navigation classification. Queued tokens and generations
remain bound to the original request; actual remote input also requires a running,
synchronized observation. Keys accepted before a queued release retain their
place before that release. Visibility does not claim, release, resize or close a
shell. Existing explicit controller actions still refresh their observations.

A hidden observation can lag by one second plus RPC time; slow or failed owner
calls retain their three-second bound and truthful unverified state. Returning
to a tab wakes a verified screen fetch, including current control, modes, geometry,
loss and gap information. The owner continues draining the PTY regardless of
frontend visibility. Each observer keeps its own history position and gap state.
No daemon/owner identity cache, new wait command, shell lifecycle change, runtime
upgrade, public module or dependency was added. Rows 20–21 remain separate.

## Reproduce

```sh
rtk proxy mkdir -p /tmp/burrow-hidden-after
rtk proxy env TEST_UNDECLARED_OUTPUTS_DIR=/tmp/burrow-hidden-after \
  aspect burrow attachment-cost -- --phases
rtk proxy aspect burrow shared-tui-check
rtk proxy aspect test //core/terminal:host_test //core/cmd/burrow:interaction_test
rtk proxy aspect burrow-check preflight
```

The before run used the task-start runtime; the after run uses this change.
Both reuse #107's unchanged measurement branch: three five-second samples per
1/2/4-attachment, idle/output, visible/hidden/background-workspace case (54 each).
New `shell_lab.py` regression scenarios run only in normal acceptance mode;
measurement modes retain their prior workload and tracing policy. Raw reports
record binary/lab/source hashes, exact pins, machine/load, every interval,
frontend and owner CPU ticks, operation counts, payload bytes and failures.
See [#107's methodology](shell-attachments-107.md) for process exclusions and
case definitions. Both runs enable the same metadata tracing; CPU comparisons
include tracing overhead and shared-host variability, not a population guarantee.

## Verification and development failures

The existing real PTY seam first failed the cadence assertion with 24 visible
and 24 hidden snapshots over equal 1.2-second windows. After the change it
observed 24 visible and one hidden snapshot. The regression also holds the
fixture owner for 150 ms while an observer selects a hidden tab and immediately
presses Shift-PgUp after the owner has exited its alternate screen. It requires
normal history navigation, a post-selection snapshot, and completion within
750 ms (a local fixture bound below the hidden interval, not a remote SLA).

That forced mode-change scenario failed before navigation was moved behind the
refresh. A first unforced version passed without forcing the race and was strengthened.
An intermediate readiness guard then rejected keys queued before release during
the existing less/rapid-detach workflow; the final guard preserves queue order
while refusing unverified input. A separate harness mistake used Alt+B (which
releases control) where workspace selection was needed; the harness was corrected.
These development failures are not successful measurement samples.

The first polling-only measurement variant collected 54 windows but timed out
waiting for a prompt after interrupting the final background command. Its
[fault report](hidden-shell-polling-109/polling-only.json) and
[log](hidden-shell-polling-109/polling-only.log) are retained; it is not
a successful acceptance run. No input refusal was visible, but the trace does
not establish whether the interrupt was accepted remotely. This unexplained
intermediate failure is not claimed fixed by the notification change. The
remaining approximately one-core hidden
frontend CPU cost motivated removal of local repeated sampling. A subsequent
shared-TUI attempt failed during fixture setup with its declared wheel path
absent while another Aspect build ran. Further Aspect invocations were serialized.

The focused frame/terminal targets passed. The final real shared-TUI invocation
passed, including the forced navigation scenario at 0.207 s (including the
150 ms pause), takeover, observer dimensions/history, vim/less/top switching,
background output, detach/close/exit/loss, restart and cleanup. The existing
4,000-line switch-and-command scenario passed at 0.683 s; this is not a new
per-key latency measurement. Its complete [log](hidden-shell-polling-109/shared-tui.log)
is retained. Final gate and measurement results follow below.

Required `rtk proxy aspect burrow-check preflight` passed **45/45 targets** in
9m05s, uncached without failed-test retries; setup and terminal checks ran three
times each. This includes all nine SSH partitions, both scoped format checks,
and documentation checks. The collector accepted source tree
`e883108c2f91bcd1c8258cd7b2b5809b8208f031d90879a7e39f06f7aa311751`
at the task-start commit plus the implementation diff. Retained
[suite metadata](hidden-shell-polling-109/preflight.json),
[gate log](hidden-shell-polling-109/preflight.log) and
[full shared-shell partition log](hidden-shell-polling-109/preflight-shared-shell.log)
record the selection, attempts, pins and hashes. Only this research evidence was
updated afterward; runtime, build and test source remained unchanged.

The separate `rtk proxy aspect burrow-check preflight hovel` failed, exit 3,
in 14.3s: **one passed and two failed**. `consumer_check` passed;
`hovel_wal_test` reproduced “lost SQLite WAL lifetime lock: type=2 owner=0”,
and `prototype_manager:check` failed its `check.py:336` assertion that the
`throw_confirmations` count is at least `len(plans)`. The latter failure's
cause was not established by this run; it is not attributed to WAL. Its
[failed suite](hidden-shell-polling-109/hovel.json) and
[log](hidden-shell-polling-109/hovel.log) are retained. The exact three-diagnostic
#86 exception remains unchanged. Strict `ci` and default full mode were not
rerun or claimed green. No containers remained after the gates.

Independent Standards and Spec reviews reported no remaining findings on the
final implementation and evidence; both independently checked the reported
statistics against the raw samples. Earlier review findings were corrected and
covered by the forced selection/navigation regression above. The final evidence
review also corrected an initial misreading of the advisory result from two
passes to one pass and two failures; the retained raw evidence was unchanged.

## Matched results

Both final variants completed all **54 samples** and cleanup with zero failures
or timeouts. These are sequential traced runs on the same shared workstation;
no builds or other acceptance suites ran during their measurement windows.
The [before](hidden-shell-polling-109/before.json) and
[after](hidden-shell-polling-109/after.json) reports retain raw per-case counts,
bytes, CPU ticks, pins and workload conditions. The
[summary](hidden-shell-polling-109/summary.json) includes every idle/output and
placement case, median and nearest-rank p95. With three samples p95 is the
maximum, not a population-tail estimate.

The table reports idle-case medians as before → after. One logical CPU = 100%.
RPC counts include identity and ordinary inventory calls; snapshot payload bytes
exclude transport/envelope overhead. Shell-owner CPU excludes Hovel daemons,
SSH children, the remote workload and the lab decoder.

| Tabs | Placement | Snapshots/s | RPC/s | Payload KiB/s | Frontend CPU % | Shell-owner CPU % |
| ---: | --- | ---: | ---: | ---: | ---: | ---: |
| 1 | visible | 20.0 → 20.0 | 213.6 → 213.6 | 91.1 → 91.1 | 48.5 → 38.1 | 3.0 → 2.6 |
| 1 | hidden | 20.0 → 1.0 | 212.6 → 40.3 | 91.1 → 4.5 | 43.6 → 12.0 | 2.8 → 0.6 |
| 1 | background-workspace | 20.0 → 1.0 | 212.4 → 41.3 | 91.1 → 4.5 | 42.2 → 11.5 | 2.8 → 0.6 |
| 2 | visible | 39.9 → 21.0 | 394.2 → 225.4 | 181.9 → 95.6 | 85.0 → 38.9 | 5.4 → 3.2 |
| 2 | hidden | 39.9 → 2.0 | 394.1 → 50.6 | 181.8 → 9.0 | 75.0 → 13.6 | 5.6 → 1.0 |
| 2 | background-workspace | 40.0 → 2.0 | 393.5 → 51.7 | 182.1 → 9.1 | 72.6 → 13.0 | 5.8 → 1.2 |
| 4 | visible | 79.9 → 23.0 | 749.0 → 235.6 | 364.1 → 104.7 | 153.9 → 40.0 | 11.2 → 4.4 |
| 4 | hidden | 79.9 → 4.0 | 747.4 → 63.8 | 364.3 → 18.1 | 140.2 → 14.4 | 11.1 → 2.2 |
| 4 | background-workspace | 80.1 → 4.0 | 749.5 → 64.6 | 365.2 → 18.1 | 137.5 → 14.9 | 11.3 → 1.8 |

Hidden and background-workspace snapshot rates fall by about 95%; selected-tab
refresh stays near 20/s. Four background-workspace attachments now generate
approximately four snapshots/s. CPU savings include reduced RPC work and fewer
repeated frontend updates; the experiment does not isolate an exact cost for
each mechanism or establish a product CPU/latency guarantee.

The intermediate polling-only report is listed separately with its failure. Its
samples are not pooled with the successful before/after comparison. No new
live-echo or shell-startup speedup is claimed.

## Next decision

Row 19 is complete. Row 20 (discovery/inspection consolidation) remains the next
ranked candidate, but its scope must account for these lower costs: four hidden
idle attachments now produce about 64 total RPC/s and 14–15% frontend CPU,
including ordinary inventory work. A bounded proposal must preserve fresh exact
owner identity, replacement/loss detection and generation fencing; the remaining
traffic does not itself justify an identity cache. No next implementation ticket
was created or started. The deferred #65 owner walkthrough remains open.
