# File history and result latency (#100)

Measured 2026-09-26 for [#100](https://github.com/Bochner/burrow/issues/100),
rows 1 and 8 of [#98](https://github.com/Bochner/burrow/issues/98).
The shared history recorder now makes its existing named `AppendLog` directly.
Both Hovel setup launches are removed. The real pinned daemon creates missing
names, retains acknowledged history and publishes the command event without
the redundant `chain created` notification.

## Measured outcome

| Workload | Samples per variant | Before median / p95 (ms) | After median / p95 (ms) |
| --- | ---: | ---: | ---: |
| CLI, absent history chain | 10 | 227.06 / 249.66 | 90.93 / 100.27 |
| CLI, established history | 10 | 224.24 / 246.90 | 94.13 / 101.96 |
| TUI, absent history chain | 10 | 395.47 / 433.17 | 301.20 / 327.50 |
| TUI, established history | 10 | 393.48 / 443.95 | 287.80 / 311.14 |
| CLI, 32 links plus target | 10 | 263.15 / 269.87 | 138.99 / 145.32 |
| CLI, slow account lookup | 10 | 2223.49 / 2230.91 | 2090.19 / 2092.32 |
| CLI, unavailable account lookup | 10 | 228.67 / 234.39 | 96.28 / 99.49 |

Warm CLI wall time fell from 224.24 to 94.13 ms and warm TUI observed listing
time from 393.48 to 287.80 ms on this host. These are independently measured
end-to-end distributions. They are **not** a predicted speedup obtained by
subtracting nested phase medians. No SLA or general network-speed claim follows.
The initial CLI browse with both an absent history chain and a cold account
cache was 259.32 ms before and
98.73 ms after (one sample each, descriptive only).

Every one of the 71 before samples launched **two** history setup processes;
every after sample launched **zero**. Both variants issued one SFTP subsystem
and one remote directory open per explicit listing. CLI samples also launched
the frontend and two verified-master `ssh -O check` processes. Each TUI listing
had at least its one owner master check; periodic inventory work overlaps some
windows and raises the observed count. Account-cache misses add one SSH exec.
The 32-link workload issued 32 `readlink` and 32 `stat` operations per listing.
Process counts exclude remote shell/server descendants and out-of-window setup.

## Reproduce and inspect

The existing declared OpenSSH lab is the execution entry point. The output
directory must already exist; `--samples` accepts 1–30 per repeated case.

```sh
rtk proxy mkdir -p /tmp/burrow-file-after
rtk proxy env TEST_UNDECLARED_OUTPUTS_DIR=/tmp/burrow-file-after \
  aspect burrow file-latency -- --samples 10
```

For a matched baseline, apply the narrowly scoped
[restore-baseline patch](file-history-100/restore-baseline.patch) in this checkout,
run the same command with a different output directory, then reverse the patch.
It restores only the original two setup calls at the shared boundary. Run
measurements sequentially, without a concurrent build or test suite. The patch
is measurement evidence, never a production configuration or runtime toggle.

An initial baseline was captured before changing history behavior. Its owner
trace was incomplete because tracing began after daemon startup, so it is not
used in the tables. The preserved comparison rebuilt the original setup path
with complete tracing enabled before daemon startup, then removed the two calls
and repeated the same lab. Both successful runs used the identical measurement
script SHA-256 `20001ea9a092bfde703607efff96ea65a9afb2ccc5178e0fbac12fd831e59de5`.
After measurement, the report's `finally` block changed only
`item["totals"].items()` to `item.get("totals", {}).items()` so a failure before
phase collection preserves partial evidence. Successful samples are unchanged.
The committed script hash is
`89a0a0dbfe727c7480f6969e78fab87db26f67e95dd09dab897594fbadb1998e`;
reversing that one expression reproduces the recorded measured-script hash.
The normal acceptance path keeps its existing connection-startup checks; the
file measurement has a separate branch so its additional owner trace does not
change those checks' process scope.

[Before raw samples](file-history-100/before.json) and
[after raw samples](file-history-100/after.json) preserve every measured sample,
CLOCK_MONOTONIC boundary, raw per-process phase, per-sample total/count, summary,
remote operation count, process count, failure and timeout field. JSON was
reformatted with one sample per line; values are unchanged. Each has 71 complete
file samples, ten separate raw-SSH references, zero failed samples and zero
lab/command timeouts. The ten slow-account samples **each hit the intended
account lookup timeout** and successfully returned numeric fallback; these are
not counted as failed file commands. The exact timeout outcome is established
by the controlled three-second delay, two-second owner bound and numeric result.

Raw same-master `ssh ls -la` medians were 7.94 / 8.92 ms before/after (ten each;
p95 8.63 / 10.10 ms). This is a smaller contract: no structured SFTP metadata,
account enrichment, retained history, Burrow audit or TUI presentation. It is
only a reference, not an equivalent benchmark or performance target.

## Conditions and pins

- Source base: `cc21bb7da3183bcd9d3f4b2ef553bbe4753e5704`, plus this ticket's
  instrumentation and lab. Each artifact records the tracked diff hash;
  the separate lab hash covers the new measurement source. Before binary:
  `a26fd3fc09a28d1bb8bd179e7e979f69d918600f65798721ae101a995212a1be`;
  after binary: `e2e05b6d78160a3f7871ce635bd1b1a08e8d06119ddc27757363845f701357fa`.
- Hovel runtime v0.4.2, source `c461ba282a8aecc7aa3a079a4613bf5e2640c388`;
  wheel SHA-256 `7edccdabe04e30098e417d27ab48064c11a8612149124b7df0797251e88a0933`.
  Public SDK source `a4cbfdf7769a9551695088c11061e3cabc368e07`.
- Aspect 2026.33.3, Bazel 9.1.1, Go toolchain 1.26.5, SFTP v1.13.11;
  Aspect fastbuild, phase tracing enabled in both variants. Full declared pins,
  build flags and dependency manifest are embedded in both raw reports.
- `linuxserver/openssh-server@sha256:47f82202bcbffe214cea77dc7f5ed24b875e81ce6ebf0e0368e2954727bf5116`;
  synthetic key, loopback Docker network, one explicitly approved master reused
  throughout each run. Per-browse SFTP setup remains independent.
- Linux WSL2 6.18.33.2, Ryzen 9 9950X3D, 32 logical CPUs, roughly 31 GiB RAM.
  One-minute load before/after each run: 14.86→14.26 baseline, 13.46→13.75 changed.
  Reports include five/fifteen-minute loads and available memory. This was a
  shared workstation, without CPU pinning, network emulation or an idle-host
  claim. Both inherited zero Windows PATH entries after the prior #105 repair.
- Ordinary directories contain five empty files. Each repeated cold-history
  sample follows a public deletion of `burrow/files`; its paired warm sample
  uses established history. Account names are warm for these paired samples.
  The single initial browse has a cold account cache. The `burrow` operation
  already exists for the approved connection; genuinely absent **both** names
  are separately checked in the portable local-command acceptance test.
- Link case: 32 valid symlinks plus their target. Account cases: five files,
  changed to a new synthetic numeric UID/GID each sample; fixture-only remote
  wrappers delay lookup three seconds or exit 127. No cache expiry or production
  account-enrichment behavior is modified.
- CLI begins immediately before spawning the frontend and ends after JSON
  receipt/decoding. TUI uses a persistent 160×40 PTY, alternates two five-entry
  paths, begins before writing the submitted line, and ends when the expected
  listing header and file cells appear in the existing VT decoder. Startup,
  file-mode entry and human typing/review are outside those TUI samples.
  Observation includes up to 10 ms polling plus decoding and scheduling; it
  does not measure a physical display. OS page caches are not flushed.
- Median is the sample median. p95 is nearest rank
  `sorted[ceil(0.95*n)-1]`, hence the maximum for ten samples; no population-tail
  inference. Bounds: CLI 60 s, TUI observation 15 s, owner SFTP 30 s, account
  lookup 2 s, whole diagnostic 1,800 s.

## Phase attribution and remaining latency

| Span | Before median / p95 (ms) | After median / p95 (ms) |
| --- | ---: | ---: |
| CLI browse including owner RPC | 18.69 / 22.25 | 18.77 / 20.57 |
| Owner browse, including teardown | 15.40 / 18.41 | 15.88 / 17.14 |
| SFTP process + handshake | 6.86 / 7.77 | 6.87 / 8.12 |
| Path canonicalization | 0.73 / 0.83 | 0.76 / 0.85 |
| Directory metadata read | 2.92 / 3.14 | 3.00 / 3.13 |
| Warm account-cache processing | 0.00 / 0.01 | 0.00 / 0.00 |
| SFTP teardown | 0.61 / 0.76 | 0.61 / 0.73 |
| CLI history write | 131.65 / 156.38 | 1.96 / 2.93 |
| TUI browse including owner RPC | 20.18 / 21.94 | 19.61 / 20.49 |
| TUI history write | 134.26 / 141.73 | 2.12 / 2.94 |
| TUI history read | 1.35 / 2.05 | 1.37 / 1.54 |
| TUI result delivery | 5.85 / 30.98 | 0.04 / 0.05 |
| Accepted result to observed cells | 37.07 / 44.00 | 41.90 / 58.03 |
| 32 links: summed link calls per listing | 42.43 / 45.16 | 44.07 / 45.73 |
| Injected slow account enrichment | 2001.36 / 2002.44 | 2001.09 / 2001.74 |
| Unavailable account enrichment | 7.44 / 8.19 | 7.02 / 7.57 |

Owner SFTP setup includes process creation and version handshake. Directory
reads obtain structured attributes; the link phase wraps each sequential
`ReadLink`/`Stat` pair. `files-metadata` includes directory reads, all link work,
account enrichment, mapping and sorting. The owner total also includes exact
master validation, serialized browse access, canonicalization and teardown.
`files-browse` includes public owner routing and result decoding. History spans
include fresh verified-peer checks and the acknowledged append/read.

These spans **nest and overlap**. Do not sum their medians or treat their
difference as an end-to-end speedup. Each sample retains the raw spans so a
reader can inspect overlap and distinguish owner from frontend PID. The TUI
ready marker precedes result dispatch; delivery ends at `acceptFiles` entry.
Presentation starts at acceptance completion and ends at decoded expected cells.
The lab cannot isolate Bubble Tea rendering from PTY and decoder overhead.

The warm CLI launch-to-command-body median was 32.17 / 32.78 ms before/after.
The TUI submission-to-command-start median was 190.56 / 222.38 ms; it includes
input parsing, frame scheduling and command dispatch, not remote retrieval.
These values are computed per sample from `cli:scp` or `files-tui-command`
start minus the lab's `begin`, then summarized. They explain why removing the
history launches does not make the TUI as fast as the owner browse itself;
they do not identify which frontend operation is responsible. Additional
frontend attribution belongs with the separately scoped general-use work.

SFTP startup is about seven milliseconds here. This does not establish a need
for retained SFTP clients. Serial link metadata is measurable at 32 links, but
this loopback workload does not determine the value of parallelism at realistic
network RTT. Slow account lookup can dominate by design; this controlled fault
does not establish how often actual server NSS is slow. SFTP reuse, parallel
links and asynchronous enrichment remain unimplemented and require bounded
follow-up evidence and lifecycle checks.

## Behavior and verification

The recorder keeps explicit operation `burrow`, chain `files`, source
`burrow-files`, reconstructed command text and successful-command filtering.
It adds no create RPC, readiness cache, lock, history database, public module or
new dependency. The CLI's completion-with-history-warning now comes from the
shared recorder; the TUI retains the retrieved listing/navigation result and
shows that warning. History-read recovery is separate #98 row 2, unchanged here.

The existing portable file CLI check now covers absent operation/chain names,
simultaneous first submissions from two processes, one published command event
per acknowledged submission without a chain-created notification, recreation
after chain deletion, workspace isolation, profile-history separation, and
acknowledged persistence after clean daemon exit/reopen. Reopening is explicit
fixture recovery: it verifies process exit, preserves obsolete launch evidence,
and reopens the retained workspace database. It does not add automatic product
restart or bypass production stale-receipt refusal.

A trigger installed **only while the disposable daemon is stopped** rejects
persistence for a marked history command. CLI and real TUI checks verify that
browsing succeeded, the warning is truthful, its history event may already be
visible in daemon memory, and the submission is not retried. This is not a
rollback or exactly-once guarantee across failures, and it does not inject a
lost HTTP reply. The completed remote browse audit remains present. Normal
acknowledgements are the persistence contract checked across clean restart.

The real SFTP/PTY lab additionally covers simultaneous CLI/TUI writes against
an absent history chain, selected-connection recall, excluded invalid/failed/
completion/cancelled commands, approved-master reuse, exact-owner/generation
checks, close/loss, numeric fallback, metadata and link errors. The presentation
check exercises retained output and semantic warning color with and without
color at 160×40, 200×50, 120×30 and 80×24; it is not subjective visual acceptance.

During development, two full file-lab attempts reached upload submission failure
and SQLite integrity errors. The new recovery fixture had kept a Python SQLite
connection open during Hovel restart: a transaction context manager does not
close that connection. Explicit close before restart corrected the fixture;
the next full file lab passed, including the formerly failing uploads and final
integrity check. This is not a fix for Hovel #86. These acceptance failures and
the initial incomplete trace are excluded from the matched timing samples.

Focused portable, interaction and real-file checks passed during implementation.
The first full preflight stopped during build analysis: the shared Python
history helper appeared in two targets with different execution tags. A single
`py_library` now owns that source, with both checks depending on it.

The next full preflight executed all 39 targets: 38 passed, including the
expanded file partition; the shell partition failed awaiting `status` after
detach. Its helper waited for the management table, which appears before the
asynchronous release acknowledgement. That acknowledgement can replace the
next command's output. The helper now awaits the existing confirmed/unconfirmed
release outcome, retaining all separate success/failure assertions. This
serializes the lab's intended sequential commands; the existing production
late-release output overwrite remains outside #100. The focused
`aspect burrow-check preflight shell` then passed both targets.

The final `aspect burrow-check preflight` passed all 39 targets on 2026-09-26,
including all nine SSH partitions and three runs each of setup/terminal checks,
without failed-test retries. The evidence collector accepted the unchanged
source during that run. This covers the expanded history/real-SFTP/TUI behavior,
portable production/proof checks, metadata and documentation gates. Existing
#86 advisory diagnostics remain outside this required gate.

## Standards review

No documented-standard violations or actionable complexity findings. The
review independently reproduced all measurement summaries and process counts.
Its measured-script hash clarification is recorded above. The shared build
target and shell-lab synchronization repair were also reviewed without findings.

## Spec review

No missing requirements, incorrect implementation or scope findings. Review
confirmed retained history/audit, successful results with truthful warnings,
and the boundaries around deferred history-read and SFTP work. The gate repair
changes synchronization without weakening existing checks.
