# Retained-shell startup measurements (#105)

Measured 2026-09-25, America/New_York, for
[#105](https://github.com/Bochner/burrow/issues/105) and measurement row 9 of
[#98](https://github.com/Bochner/burrow/issues/98). This implements measurement
support, not the conditional immutable chain-file experiment. Prepare, adoption,
start, request/geometry binding, exact-owner validation, confirmed throws,
private input, audit and resource cleanup remain in place.

## Findings

The delay is reproducible on an already approved SSH master. Most of it occurs
inside the two Hovel CLI processes, before the shell adapters run. On this WSL
machine, eager clipboard backend detection scans 33 inherited Windows PATH
entries on **every** Burrow and Hovel process startup. Go initialization tracing
measured roughly 280–292 ms in `github.com/atotto/clipboard` alone. The same
binaries, with only those Windows entries omitted, initialized in approximately
22 ms (Hovel) and 36 ms (Burrow), instead of 310–326 ms. These are standalone
initialization probes, not shell startup timings or CPU measurements.

Each shell invokes two Hovel CLIs and starts six Burrow module processes:
catalog inspection by the daemon, installed-package inspection by the CLI, and
the actual adapter for **each** throw. Thus the environment penalty repeats
eight times after the frontend has already started. The retained prepare
adapter later owns the SSH child; the start adapter does not become a second
shell owner. None of these observations justify skipping inspection or approval.

The adjacent growing-tab workload also exposes delay before the shell-create
handler runs. PATH repair does not eliminate that delay. Keep its results
separate from the fixed two-attachment comparison below.

## Reproduce

Use the existing declared disposable fixture through Aspect. The optional
output directory must exist; otherwise evidence writing deliberately fails.

```sh
rtk proxy mkdir -p /tmp/burrow-shell-startup
rtk proxy env TEST_UNDECLARED_OUTPUTS_DIR=/tmp/burrow-shell-startup \
  aspect burrow shell-latency -- --samples 10 --phases
```

Omit `--phases` for normal timings with no phase instrumentation. `--processes`
adds a separate CLI process/syscall diagnostic and Go `inittrace` probes; it
requires the host's `strace` and changes timings substantially. `--linux-path`
omits `/mnt/<drive>/` PATH entries in this disposable lab only. `--growing-tabs`
retains each previous TUI shell as a background observer; by default the first
shell remains and each later shell is closed, holding two attachments.
`--samples` is bounded to 1–30 per frontend (one first TUI shell, then additional
shells); it is a sample count, not a performance threshold.

The lab uses a fresh workspace/daemon with packages already available. Its
first connection includes manager activation, then it closes that connection
and creates `gateway` on the already active manager. Shell measurements reuse
that exact master. Ten CLI creates each close their channel; the TUI then opens
one first shell and nine additional shells. CLI geometry is 80×24; outer TUI
geometry is 160×40 with a 98×35 shell. Authentication uses a synthetic key.
No directory browsing, remote workload, private keyboard capture or human
authentication delay is part of the normal startup workload.

The CLI obtains a real review, waits a synthetic 200 ms, and submits that review
digest. Review construction, review wait and post-approval system work are
separate timestamps. The TUI's explicit shell command has no additional review
dialog; its human wait is not applicable. Connection approval is separate.

## Provenance and raw evidence

Source base: `1ac01c09de6aa4f9305b59da93306adf395ce533`, with this ticket's opt-in
instrumentation/lab changes. Reports record the actual binary SHA-256 and
tracked working-diff SHA-256 at the beginning of each run. Main comparisons use
the identical binary `78ed71b65eb952767918b4c08f9182ed1a98c69c67c910208d5560e61f6625d6`.
Earlier initialization/process probes used `88c67f76…`, before the `module-ready`
timestamp was added. No production optimization separates these binaries.

- Hovel v0.4.2 runtime: `c461ba282a8aecc7aa3a079a4613bf5e2640c388`; wheel SHA-256
  `7edccdabe04e30098e417d27ab48064c11a8612149124b7df0797251e88a0933`.
- Public SDK: `a4cbfdf7769a9551695088c11061e3cabc368e07`.
- Aspect `2026.33.3`, Bazel `9.1.1`, declared Go `1.26.5`; Aspect fastbuild.
- OpenSSH fixture:
  `linuxserver/openssh-server@sha256:47f82202bcbffe214cea77dc7f5ed24b875e81ce6ebf0e0368e2954727bf5116`.
- Linux WSL2 `6.18.33.2`, AMD Ryzen 9 9950X3D, 32 logical CPUs, approximately
  31 GiB guest RAM. Shared workstation; CPU/load and memory availability vary.
  Reports preserve load at both ends and initial memory availability. No CPU
  affinity, cold filesystem cache, network latency emulator or idle-host claim.
- Host `strace` 6.19 is a diagnostic, outside the normal timing workload. Its raw
  [process trace](shell-startup-105/processes.trace) replaces the temporary
  workspace prefix with `<LAB>`; arguments contain no credentials/private input.

Raw reports retain monotonic timestamps, phase durations, counts, failures and
timeouts. Boundary/setup spans are kept individually; unrelated background
caller RPCs are aggregated in the published older reports. Complete
private phase traces remain under `/tmp/burrow-105-*`, with their hashes in the
reports. All medians are sample medians; p95 uses nearest rank
`sorted[ceil(0.95*n)-1]`, so p95 is the maximum for these small samples.

| Evidence | Workload |
| --- | --- |
| [Fixed, inherited PATH](shell-startup-105/fixed-inherited.json) | 10 CLI / 10 TUI; 33 Windows entries; phases; two attachments for each additional shell |
| [Fixed, clean PATH](shell-startup-105/fixed-clean.json) | Identical binary and workload; zero Windows entries; phases |
| [Growing, untraced](shell-startup-105/growing-untraced.json) | 10 CLI / 10 TUI; inherited Windows PATH; no phase trace |
| [Growing, phased](shell-startup-105/growing-phases.json) | Same workload; nested phase attribution |
| [Growing, Linux PATH](shell-startup-105/growing-linux-path.json) | Same binary/workload; Windows PATH entries removed in lab |
| [Process and initialization](shell-startup-105/processes-and-init.json) | Separate one-CLI/one-TUI process diagnostic; three init probes per binary/environment |

Normal runs report zero failures/timeouts. Individual bounds are 60 seconds for
CLI subprocesses, 20 seconds for connection transitions, 15 seconds for TUI
prompt observation and 1,800 seconds for the full diagnostic. Deliberate failure
checks are recorded separately below. Initialization probes use wall-clock
`GODEBUG=inittrace=1` metadata; they do not measure CPU utilization. The declared
Go runtime documents this format in `src/runtime/extern.go`.

## Matched environment comparison

The two fixed-attachment runs used the identical binary, phase instrumentation,
fixture, sample count and geometry. Only inherited PATH changed. Host load at
the start was 19.73 versus 19.48 on 32 CPUs; this was not an idle workstation.

| Post-approval / submitted work | Samples per run | Inherited PATH median / p95 (ms) | Clean PATH median / p95 (ms) |
| --- | ---: | ---: | ---: |
| CLI create → observed prompt snapshot | 10 | 3,749 / 3,814 | 680 / 725 |
| Additional TUI shell → visible prompt, two attachments | 9 | 3,283 / 3,368 | 785 / 834 |
| First TUI shell → visible prompt | 1 | 3,366 / descriptive only | 680 / descriptive only |
| Cold-manager connection → authenticated master | 1 | 3,389 / descriptive only | 609 / descriptive only |
| Warm-manager connection → authenticated master | 1 | 1,914 / descriptive only | 418 / descriptive only |

Additional TUI shell prepare/start spans fell from medians 1,554/1,561 ms to
303/320 ms. This is a measured environment repair, not an application runtime
optimization or a claim that instrumentation improves startup. The connection
and first-shell samples describe these runs only; they do not establish a
population p95. CLI prompt observation includes another frontend process;
TUI visible prompt is the closer match to the owner's reported symptom.

The independent growing-tab run with phase tracing **off** measured additional
TUI shells at median 3,218 ms / p95 4,017 ms (nine samples). The growing phased
run measured 3,351 / 3,767 ms. These reproduce the delay with and without phase
instrumentation; separate runs on a shared host do not isolate tracing overhead.

## Phase boundaries and overlap

All phase timestamps use Linux `CLOCK_MONOTONIC`, comparable between processes
and Python on this boot. `begin` in the lab is immediately before launching the
approved CLI or writing the TUI shell command. TUI input queueing/parsing occurs
before `shell-lifecycle:create` begins. That lifecycle span includes current
manager/resource verification and both sequential throws.

| Boundary/span | Observation and limit |
| --- | --- |
| Review ready → begin | Synthetic 200 ms CLI review wait; excluded from system work |
| Connection connected | Owner's authenticated-master timestamp, distinct from CLI receipt and inspect observation |
| `manager-throw:shell-prepare/start` | Build binding, fresh immutable operation/chain, setup RPCs, lock, Hovel process and result checks |
| `call:CreateOperation/CreateChain/AddModule/AddTarget/SetChainConfig` | Public setup RPCs including verified-peer checks; nine for prepare and ten for start |
| `dispatch-lock` | Existing per-workspace dispatch serialization wait; no change to the lock |
| `hovel-process:throw` | Actual subprocess `Run`, nested inside `hovel-cli:throw`; not just adapter runtime |
| `module-ready` | Burrow module reached its framed JSON-RPC serve entry, after package initialization; not adoption |
| `module:shell-prepare/start` | Adapter body, including existing identity/build/owner checks |
| `shell-sdk-allocation` | SDK session registration, which does not launch SSH |
| `shell-adoption-observation` | Existing exact-owner inspect during start; successful observation after adoption, not the daemon's exact adoption instant |
| `shell-pty-start`, `shell-ssh-start` | Owner's audited PTY setup and actual SSH child `Start`; does not prove a usable remote prompt |
| `shell-first-output` | First positive PTY read; metadata only, possibly banner/local SSH output; not asserted to be the prompt |
| `shell-attachment` | TUI initial snapshot and attachment setup; subsequent controller acquisition is asynchronous |
| `prompt_observed` | CLI snapshot contains the fixture prompt; includes a separate observer CLI startup; no terminal paint claim |
| `visible`, `control_visible` | First decoded VT cells carrying the fixture prompt and CONTROL indicator, observed independently |

The VT observer has up to a 50 ms read/drain window plus decoder overhead.
These are lab-observed frames, not physical display refresh times. A dispatch
receipt is never substituted for output. The pinned daemon adopts a prepared
session after `Module.Run` returns; this bounds adoption between the adapter
return and the successful later owner inspection. Exact internal broker timing
is not exposed by these Burrow-only probes. No polling was added to the broker.

Nested phases must not be summed as independent costs. Owner/module work is
inside Hovel execution; setup RPCs include daemon verification; first PTY output
can arrive before start dispatch returns. Background snapshots overlap the TUI
throw. Phase totals are workload observations, not a partition of wall time.

The syscall diagnostic follows the frontend and its newly launched descendants,
including the Hovel CLI's installed-package inspector. It does not attach to the
already-running daemon. Daemon-launched catalog/adapter processes are counted
by `module-ready` phases instead. Standalone initialization probes locate eager
package initialization costs, but do not allocate daemon CPU, scheduling delay,
or every internal Hovel operation to a Burrow phase.

The growing inherited-PATH phase run contained 20 complete creates: median
prepare 1,452 ms, start 1,451 ms; the two Hovel process spans totaled a median
2,816 ms per create. Adapter bodies were only 47/59 ms. SDK allocation was
0.053 ms, adoption observation 4.05 ms, PTY start 7.51 ms, SSH child start
0.205 ms, first positive PTY read 3.98 ms after drain began, and TUI attachment
5.83 ms. Median dispatch-lock total was 0.005 ms: contention is not the main
cause in this sequential workload. Setup takes tens of milliseconds, not the
seconds seen in Hovel process startup.

## Adjacent systems and bounded follow-ups

1. **Environment repair / eager clipboard initialization.** The owner requested
   no Windows PATH entries. `/etc/wsl.conf` now sets
   `[interop] appendWindowsPath=false`; `~/.zshenv` strips inherited
   `/mnt/<drive>/` entries for new Zsh processes in this already-running host.
   New shells were checked for zero entries and correct Linux tool resolution.
   Existing processes retain their old environment until relaunched. WSL reads
   its setting on restart ([Microsoft's configuration contract](https://learn.microsoft.com/en-us/windows/wsl/wsl-config#interop-settings)).
   The existing non-lab Hovel daemon and its retained Burrow processes still
   have 33 Windows entries; applying the repair to that live workspace requires
   its normal reviewed restart. No running user connection/daemon was restarted.
   A portable code follow-up
   would defer clipboard backend discovery until clipboard use, with real
   paste/copy acceptance; stripping arbitrary user PATH entries in production
   Burrow would change tool discovery and is not this ticket's implementation.
2. **Hovel inspection startup.** The pinned
   [CLI](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/rootcli/rootcli.go)
   gets the daemon catalog, then the
   [throw handler](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/app/commands/catalog.go)
   inspects installed packages again. The
   [catalog provider](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonrpc/daemonrpc.go)
   and [runner](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/moduleruntime/pythonrpc/runner.go)
   explain the separate process launches. A bounded upstream follow-up should
   investigate redundant inspection tied to immutable build/manifest identity,
   while retaining freshness, launch-key, confirmation and run evidence. This
   ticket neither caches catalogs nor upgrades Hovel.
3. **Shared dispatch callers.** [Connections](../../core/connection/manager.go)
   use activate/connect throws; [runs](../../core/connection/runs.go) use
   prepare/launch and further confirmed actions; [tunnels](../../core/connection/forward.go),
   [downloads](../../core/connection/downloads.go) and
   [chain operations](../../core/connection/chains.go) use the same `managerThrow`.
   They inherit repeated process initialization and inspection costs. This is
   traced call-path evidence, not new normal-workload benchmarks for every
   capability. Long transfer/command execution remains separate.
4. **History/profile setup.** [File history](../../core/connection/files.go)
   launches `op create` and `chain create` CLIs before appending each successful
   command. [Profile scope](../../core/connection/profiles.go) has a similar pair
   when saving/selecting scope. Their fresh-process costs are adjacent, but
   replacing either sequence must preserve history and failure semantics.
   File-history optimization is already scoped by #100; do not silently combine
   it with shell lifecycle changes.
5. **Background observers.** In the inherited-PATH growing run, delay from TUI
   submission to the create handler grew from 99 ms at two attachments to
   584 ms at ten. The Linux-PATH growing run had 2.0–3.9 second pre-handler
   outliers at five to seven attachments while host load rose; it still ended
   around 877 ms at ten. This implicates frontend scheduling/observer work as an
   additional attribution target, but does not separate rendering, polling and
   host contention. #106 should measure CPU/RPC/snapshot costs before changing
   the current 50 ms per-attachment sampler. No notification/polling rewrite or
   universal latency promise follows from these startup samples.

**Chain-file experiment:** low priority compared with initialization and
inspection. Current setup comprises 19 small public RPCs per shell. An immutable
chain file might remove some of that cost, but the pinned public throw handler
still loads module metadata and launches processes. These measurements do not
predict removal of the multi-second delay from chain files alone. Keep the
experiment as a bounded follow-up with matched timings and proof of immutable
request isolation, approval, exact-owner checks, build binding, audit and all
failure cleanup. It was not implemented here.

## Cleanup and acceptance

Four stale OpenSSH containers, aged 26–39 hours, used this pinned fixture and
`/tmp/bs-*` key mounts; no owning lab processes remained. They and their anonymous
volumes were removed explicitly. An unrelated `tirnaill` container was retained.

The lab now labels its containers, saves Docker's container ID before relying
on command acknowledgement, and uses `ExitStack` so artifact-copy, child or
daemon cleanup failures cannot prevent container teardown. Cleanup cancels the
outer alarm and keeps bounded individual waits. The intentionally nonexistent
`TEST_UNDECLARED_OUTPUTS_DIR=/tmp/burrow-105-no-such-directory/outputs` caused the
expected evidence-write failure; the lab's container was still removed.
An explicit SIGTERM during a 30-sample run recorded six completed samples and
the interruption, then passed post-shutdown database integrity/audit checks and
removed its container. These deliberate failures are outside the normal timing
sample sets.
After review, SIGALRM was injected during an active traced create. The report
recorded seven completed samples, one `TimeoutError` and `timeouts=1`; the
tracked strace/frontend/Hovel/inspector processes all exited, and the exact lab
container was absent afterward. `--kill-on-exit` now matches the existing
connection diagnostic's descendant cleanup convention.
SIGKILL/host loss can still bypass Python cleanup; labels provide provenance
for explicit reconciliation. There is no automatic global Docker prune.

`aspect burrow format`, `aspect burrow sessions-check` and
`aspect burrow shared-tui-check` passed. The acceptance checks cover retained
remote PIDs and master identity, exact-owner/stale refusal, controller/observer
and takeover behavior, detach/close, resize authority, frontend disappearance,
manager replacement, connection loss, terminal restoration and private-input
exclusion.

An initial full preflight executed all 39 targets successfully, but its evidence
collector correctly rejected source edits made while review was finishing.
A concurrently queued diagnostic also lost an execroot dependency during a
different Aspect command; it was rerun separately. Final gate collection uses
fixed source and sequential Aspect commands; neither infrastructure failure is
included in the normal timing samples.

The final `aspect burrow-check preflight` passed on 2026-09-26: all 39 targets
executed successfully, including the nine SSH partitions and three runs each
of setup/terminal race checks, without failed-test retries. The evidence
collector accepted the fixed source. The existing #86 advisory exceptions
remain outside this required gate. Final Docker inspection showed only the
unrelated `tirnaill` container; no Burrow lab containers remained.

## Standards review

No hard documented-standard violations. Review identified that the new process
diagnostic needed the existing `strace --kill-on-exit` convention to reap traced
frontend descendants on interruption; added and re-reviewed. Zero remaining
Standards findings; no additional smell justified a refactor.

## Spec review

Review identified that the whole-lab alarm was recorded as an interruption but
not counted as a timeout. SIGALRM now raises `TimeoutError`, counted explicitly;
SIGINT/SIGTERM remain interruptions. Zero remaining Spec findings. Lifecycle,
confirmation, exact-owner and private-input contracts were preserved, and raw
medians match the report.
