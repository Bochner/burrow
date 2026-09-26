# Idle and background retained-shell costs (#107)

Measurement for [#107](https://github.com/Bochner/burrow/issues/107), row 11 of
[#98](https://github.com/Bochner/burrow/issues/98). This adds an opt-in diagnostic
to the existing declared shared-shell/TUI lab. Polling, identity checks, input,
control, terminal rendering and retained lifetimes are unchanged.

## Reproduce and interpret

```sh
rtk proxy mkdir -p /tmp/burrow-attachments
rtk proxy env TEST_UNDECLARED_OUTPUTS_DIR=/tmp/burrow-attachments \
  aspect burrow attachment-cost -- --phases
```

Omit `--phases` for a CPU run with tracing disabled. Use a separate output
directory for each run. The default is three five-second samples per case;
`--samples` permits one through five repeats. The existing outer lab deadline
is 1,800 seconds; terminal waits retain their 15-second deadline. Without an
output directory, the report is printed. With one, `shell-attachment-cost.json`
and the opt-in `attachments.jsonl` trace are retained.

The workload uses one approved OpenSSH master, one frontend and one, two, then
four retained shells. Every shell has its own owner process and one controller
attachment in the frontend. A second, empty workspace remains open throughout.
Each attachment count is measured idle, then with each shell printing one
short ASCII counter line per `sleep 0.1` (nominally ten lines/s, not a measured
exact output rate). Each workload has three placements:

- **Visible:** the last shell is selected; any other shells are hidden tabs in
  the same workspace.
- **Hidden:** management is selected in that workspace; all shell tabs are hidden.
- **Background workspace:** the empty workspace's management view is selected;
  all retained shells belong to the other, background workspace.

The real frontend PTY is drained and decoded throughout each interval. Selecting
views uses existing navigation and preserves each controller. One second of
settling precedes each placement. No CLI inspection runs inside a sample;
state/geometry/generation inspections, output setup, navigation and cleanup are
outside the windows. CPU readings include normal frontend inventory work.
Output is stopped afterward, and a split marker proves that returning to each
tab still accepts and displays a new command. Keep running releases all claims;
the lab verifies terminal restoration, explicitly closes retained shells, checks
the original master identity, then closes the connection. Disposable daemons
and the container are cleaned up by the existing lab.

## Measurement boundaries

`BURROW_ATTACHMENT_TRACE` uses the existing private-file validation and append
mechanism, with PID, monotonic completion time, fixed operation name and byte
length only. It records no input, output content, tokens, credentials or workspace
paths. Module stdout remains framed JSON-RPC. The existing phase trace is disabled
in these runs, and attachment tracing is off by default.

- `rpc:*` counts completed attempts at `rpcBody`, including `GetDaemonInfo`
  identity checks that occur beneath public `launch.Call` operations. Attempts
  are not an RPC-success guarantee and do not expose individual failure codes.
- `command:*` counts owner-command returns by verb, including `inspect` and
  `snapshot`. These are subcategories of `RunSessionCommand`, not extra RPCs;
  never add the command counts to RPC totals. Private input/control operations
  are outside these observation windows.
- Snapshot bytes are `len(result.Stdout)`: the UTF-8 byte length of the rendered
  snapshot JSON payload. They exclude the outer RPC JSON envelope/escaping,
  HTTP headers and Unix transport. They are **payload bytes, not wire traffic**.
- Only records from the measured frontend PID and monotonic window are counted.
  Daemon/owner background work, lab CLI probes and setup/cleanup trace records
  are excluded. A request crossing a boundary belongs to its completion window.
- CPU is the change in process `utime + stime` from Linux `/proc/PID/stat`, divided
  by `SC_CLK_TCK` and the measured interval. Raw start/end ticks and process
  start times are preserved; a replaced process fails the sample. Frontend CPU
  is separate from the sum of all retained-shell owner processes. Hovel daemons,
  the connection manager, SSH children, the remote workload and VT decoder are
  excluded. These are process CPU costs, not total system or transport costs.
  Percentages use **one logical CPU = 100%**; multicore work may exceed 100%.

Timers are unchanged (50 ms attachment refresh and 30 ms frontend sampling),
but rates and CPU below come from observations rather than those constants.
Tick quantization and sequential boundary reads introduce small skew. Each
report retains actual interval boundaries, counts by operation, snapshot bytes,
process CPU readings, load, memory, kernel, CPU model and all source/runtime/
toolchain pins. Cases run in fixed ascending tab-count and placement order;
retained output/history and workload age differ. This is a local warmed fixture,
not a randomized experiment or a product tab-count/CPU guarantee. Multiple
observers of the same shell and many populated workspaces are not measured.

## Results

The traced run completed **54 samples**, three per case, with zero failures or
timeouts. Rates below are medians of each sample count divided by its actual
elapsed interval. CPU columns are medians of measured process CPU percentages.
The [per-case summary](shell-attachments-107/summary.json) also retains median
and p95 for CPU, payload rates and every operation rate in both applicable runs.
P95 uses nearest rank, `sorted[ceil(0.95*n)-1]`: with only three repeats it is
the **maximum**, a descriptive observation rather than a reliable tail estimate.
The [raw report](shell-attachments-107/traced.json) preserves every window,
operation count, payload-byte total and process tick reading.

| Tabs | Workload | Placement | Snapshots/s | RPC/s | Payload KiB/s | Frontend CPU % | Shell-owner CPU % |
| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: |
| 1 | idle | visible | 20.0 | 213.4 | 90.8 | 49.3 | 2.8 |
| 1 | idle | hidden | 19.9 | 211.6 | 90.6 | 45.0 | 2.8 |
| 1 | idle | background-workspace | 19.9 | 214.4 | 90.8 | 43.7 | 2.8 |
| 1 | output | visible | 20.0 | 213.1 | 91.1 | 51.7 | 3.0 |
| 1 | output | hidden | 20.0 | 211.3 | 90.9 | 44.5 | 3.0 |
| 1 | output | background-workspace | 20.0 | 218.6 | 91.1 | 50.0 | 3.2 |
| 2 | idle | visible | 40.0 | 393.3 | 182.1 | 98.1 | 6.0 |
| 2 | idle | hidden | 39.9 | 391.9 | 181.8 | 80.0 | 5.8 |
| 2 | idle | background-workspace | 40.0 | 388.0 | 182.4 | 77.2 | 5.4 |
| 2 | output | visible | 40.0 | 397.6 | 182.3 | 85.2 | 5.8 |
| 2 | output | hidden | 39.9 | 396.6 | 181.9 | 78.5 | 5.6 |
| 2 | output | background-workspace | 40.0 | 398.7 | 182.4 | 75.9 | 5.6 |
| 4 | idle | visible | 80.0 | 753.1 | 364.7 | 164.8 | 11.9 |
| 4 | idle | hidden | 80.1 | 758.0 | 365.0 | 148.1 | 11.5 |
| 4 | idle | background-workspace | 79.9 | 756.9 | 364.1 | 150.1 | 11.8 |
| 4 | output | visible | 79.9 | 753.6 | 364.0 | 175.5 | 13.2 |
| 4 | output | hidden | 80.1 | 751.7 | 364.8 | 170.3 | 13.9 |
| 4 | output | background-workspace | 80.0 | 749.0 | 364.4 | 167.3 | 13.8 |

Snapshot rate and payload scale approximately with attachment count. Hiding
tabs or selecting another workspace does not stop that work. Four idle
background attachments consumed about 1.5 logical CPUs in the frontend and
0.12 logical CPUs across shell owners in this traced run. This excludes the
other processes listed above; it is not a whole-application CPU budget.

Representative **visible idle** median RPC rates by public operation:

| Operation | 1 tab /s | 2 tabs /s | 4 tabs /s |
| --- | ---: | ---: | ---: |
| ActiveLogs | 1.6 | 1.6 | 1.6 |
| GetDaemonInfo | 142.3 | 262.3 | 502.2 |
| ListSessions | 25.3 | 45.6 | 85.1 |
| RunSessionCommand | 42.3 | 83.1 | 162.4 |
| Snapshot | 1.0 | 1.0 | 1.0 |

The raw report has these counts for **every** case, plus owner-command verbs.
`GetDaemonInfo` dominates request count. The measured snapshot and inspect
commands each track about 20 returns/s/attachment. Counts include the normal
management refreshes for both opened workspaces, so the entire RPC rate must
not be attributed exclusively to snapshots. No span timings or CPU profile
separate identity verification, JSON decode and frontend rendering here.

### Tracing comparison

The [untraced run](shell-attachments-107/untraced.json) also completed all 54
samples with zero failures/timeouts and the same executable. Three-sample
median CPU percentages below compare the matching cases. No RPC/byte counts
are invented for the disabled run.

| Tabs | Workload | Placement | Frontend traced / untraced % | Owners traced / untraced % |
| ---: | --- | --- | ---: | ---: |
| 1 | idle | visible | 49.3 / 70.7 | 2.8 / 2.6 |
| 1 | idle | hidden | 45.0 / 68.1 | 2.8 / 2.4 |
| 1 | idle | background-workspace | 43.7 / 63.6 | 2.8 / 2.4 |
| 1 | output | visible | 51.7 / 71.4 | 3.0 / 2.6 |
| 1 | output | hidden | 44.5 / 67.3 | 3.0 / 2.6 |
| 1 | output | background-workspace | 50.0 / 59.2 | 3.2 / 3.4 |
| 2 | idle | visible | 98.1 / 101.8 | 6.0 / 6.6 |
| 2 | idle | hidden | 80.0 / 91.1 | 5.8 / 6.6 |
| 2 | idle | background-workspace | 77.2 / 89.5 | 5.4 / 6.7 |
| 2 | output | visible | 85.2 / 100.7 | 5.8 / 6.7 |
| 2 | output | hidden | 78.5 / 88.6 | 5.6 / 6.6 |
| 2 | output | background-workspace | 75.9 / 88.0 | 5.6 / 6.6 |
| 4 | idle | visible | 164.8 / 174.0 | 11.9 / 13.3 |
| 4 | idle | hidden | 148.1 / 162.9 | 11.5 / 12.9 |
| 4 | idle | background-workspace | 150.1 / 154.9 | 11.8 / 12.3 |
| 4 | output | visible | 175.5 / 162.2 | 13.2 / 12.6 |
| 4 | output | hidden | 170.3 / 145.3 | 13.9 / 12.3 |
| 4 | output | background-workspace | 167.3 / 139.0 | 13.8 / 12.3 |

Differences change sign and are larger than a reliable isolated tracing effect.
Untraced results confirm material attachment-count scaling, but these two
sequential shared-workstation runs do **not** identify an exact append overhead
or a causal CPU saving. There is no optimization in either run. Untraced
one-minute load was 27.92 at start and 20.55 at end.

## Follow-up decisions

1. **Hidden-tab scheduling (#98 row 19): merits a bounded proof.** Idle hidden
   and background tabs retain nearly the same snapshot/RPC/byte work as visible
   tabs, with substantial frontend CPU. Investigate reducing hidden sampling
   first. Focus, input and control changes must obtain a fresh verified screen;
   independent observer positions, current geometry, lost/out-of-sync reporting,
   owner PTY drain and retained lifetimes must remain correct.
2. **Discovery consolidation (row 20): merits a bounded proof.** Repeated
   inspection and daemon identity RPCs are a large measured request multiplier.
   This does not quantify the CPU share of those operations. Any fewer-call path
   must retain exact session/module/kind/name matching, fresh daemon/peer checks,
   generation fencing and truthful replacement/loss detection. This ticket
   neither caches identity results nor establishes a safe atomic replacement API.
3. **Screen-change waiting (row 21): merits a separate proof, after the smaller
   scheduling experiment.** Unchanging screens still transfer full snapshots;
   an owner-authored bounded wait could reduce work even for visible idle tabs.
   Combine this evidence with [#106's observation-delay measurements](shell-interaction-106.md).
   Revision/wakeup races, cancellation, authority changes, independent observers
   and locks needed by input/output require proof. A timer rate is not a predicted
   improvement and raw Hovel reads are not a rendered-screen subscription.

These are evidence-supported recommendations, **not implemented optimizations**
or approval of an unbounded rewrite. No tab-count ceiling, CPU SLA or speedup is
claimed.

## Provenance and collection overhead

Measured 2026-09-26 on `audit/general-use-architecture`, source base
`fca27eb0f8f0c3142298255751eb24feec14474c` with this issue's lab/instrumentation.
Every ordinary run records the tracked source-diff hash and the new lab source
hashes, plus complete pin/build files. The traced lab hash precedes a Python
formatter-only change to its final print conditional; workload and executable
are identical for the untraced run. Both use Aspect fastbuild, Bazel 9.1.1,
Aspect 2026.33.3 and Go 1.26.5. Hovel runtime v0.4.2 is pinned at
`c461ba282a8aecc7aa3a079a4613bf5e2640c388`; its public SDK is
`a4cbfdf7769a9551695088c11061e3cabc368e07`. The Docker OpenSSH fixture digest and
wheel SHA-256 are preserved in each report.

The measured executable SHA-256 is
`09e727b143f639a8acc28b646171e80fac96d437d5aef335c3ee04273fa80469`.
The machine is Linux WSL2, AMD Ryzen 9 9950X3D with 32 logical CPUs, shared with
other workstation work, without affinity or synthetic network delay. Traced
one-minute load rose from 14.13 to 21.29. The normal test suite was not running
concurrently with measurement windows; the initial measurement launch waited
for the focused test command to finish before building/running.

Tracing adds synchronous private-file appends for every counted RPC/command.
The disabled run uses the same executable with the trace descriptor absent;
it still has dormant hook calls/name construction, so it does not measure all
instrumentation cost against an unmodified binary. The PTY decoder and Python
collector also consume unreported CPU and can affect scheduling. Trace aggregation
runs outside each window. No production monitoring service or cadence change
was introduced. Timing/order and shared-host load limit causal overhead claims.

Raw per-window samples are committed, rather than only medians. The complete
traced event file is retained locally at
`/tmp/burrow-107-traced/attachments.jsonl`; its size/count/hash are in the
[trace manifest](shell-attachments-107/trace-manifest.json). The file contains
setup/cleanup and other-process events excluded from reported frontend windows.

## Validation and development failures

The focused `aspect test //core/launch:digest_test //core/terminal:host_test
//core/cmd/burrow:interaction_test` passed. An earlier invocation named the
nonexistent `frame_test` target and failed during target selection; it ran no
tests. The corrected invocation uses the declared frontend interaction target.

Three development probes stopped honestly: one used a workspace-list row where
a shell-list row was needed (terminal wait timeout), one expected `GetInfo`
instead of the actual `GetDaemonInfo` method (counter assertion), and the retained
[owner-probe report](shell-attachments-107/owner-probe.json) assumed
a shared shell-owner PID (assertion after the one-attachment cases). The harness
now selects shell rows from decoded sidebar cells, counts the real method, and
sums every retained-shell owner separately. Those probes are excluded from the
ordinary scaling results. Their failures caused no production behavior changes.

Final `aspect burrow-check preflight` passed **all 45 targets** in 9m 19s,
uncached without failed-test retries. It built the production/proof packages
and ran portable, both scoped formatting, documentation, all nine SSH acceptance
partitions, and three executions each of setup/terminal checks. Its shell
partition invokes the existing session and shared-TUI checks: independent
readers, concurrent takeover, stale-authority refusal, observer dimensions,
visible gaps/snapshot recovery, retained reattachment, owner/daemon loss,
terminal restoration and cleanup all passed. The existing composite switch and
command during 4,000 background lines took **0.700 s**; it is not a per-key or
new scaling sample.

The report collector accepted the unchanged source. Gate log:
`/tmp/burrow-107-preflight.log`; structured evidence: `.report-input/all/`.
No disposable connection-lab containers remained. Only this completion prose
was added after collection. The three advisory #86 diagnostics and deferred
#65 owner walkthrough remain outside this completion claim.

## Standards review

Zero findings. The diff follows declared Aspect workflows, existing SSH/TUI
fixture and cleanup, public Hovel calls, unchanged approval/control contracts,
and opt-in tracing without terminal contents or credentials. No actionable
baseline smells were identified.

## Spec review

Zero remaining findings. Review caught the missing p95-method disclosure; the
nearest-rank convention and its three-sample limitation are now explicit.
Independent recomputation matched every median and p95 across all 36 summary
cases. Required workloads, placements, operation counts, payload bytes and CPU
costs are covered, with limits and overhead disclosed. Conditional changes
remain recommendations. The subsequently passed required gate supplies the
preservation evidence that was pending during review.

Review totals: Standards 0; Spec 0 remaining (one reporting finding corrected).
