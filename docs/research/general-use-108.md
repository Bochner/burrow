# Combined general-use acceptance (#108)

Integration evidence for [#108](https://github.com/Bochner/burrow/issues/108),
covering initial rows 1–11 and conditional rows 12–25 of
[#98](https://github.com/Bochner/burrow/issues/98). Verification began on
2026-09-26 at `ee4e5a8` on `audit/general-use-architecture`. This ticket adds
an evidence report, not another runtime change or conditional optimization.
The parent spec and deferred #65 owner walkthrough remain open and unchanged.

## Initial-scope ledger

“Verified” means the named automated behavior/workflow checks passed; it does
not imply subjective owner acceptance, an upstream fix, or a latency SLA.
The gate record below separates current integration runs from historical
measurements. Measurement reports retain their original source and binary pins;
they are not presented as new benchmarks of `ee4e5a8`.

| Row | Outcome | Implementation and evidence |
| --- | --- | --- |
| 1 | Verified fix; measured improvement | #100, `ad4023e`: `RecordFileCommand` retains one explicitly named public append and removes both setup CLIs. Real file CLI/PTY checks cover absent names, concurrent first writes, recreated chain, acknowledged clean-restart persistence, filtering, workspace/connection scoping, audit and truthful write uncertainty without retry. [Evidence](file-history-100.md). |
| 2 | Verified fix | #101, `ee05d87`: separate history-read error; successful listings remain usable, last loaded recall/draft survive failure, unavailable/outdated differs from empty, and later reads recover. Real file workflow and frame presentation checks cover failure/recovery and stale-origin refusal. [Resolution](https://github.com/Bochner/burrow/issues/101). |
| 3 | Verified workflow | #102, `2229977`: pinned Go check/write workflows share six production-package globs. Existing/new-source probes demonstrated nonmutation, discovery and repair; probes removed. Required gate runs formatter and behavior tests. [Resolution](https://github.com/Bochner/burrow/issues/102). |
| 4 | Verified workflow and behavior preservation | #103, `95be41a`, verification `5180c0f`: pinned Ruff format scope, 38 existing files formatted with matching ASTs, shebangs and executable modes. Prototype cleanup excluded. [Inventory and evidence](python-formatting-verification.md). |
| 5 | Verified guidance fix | #104, `5f75bc6`: retained-shell descriptions corrected; `shells` recommendation remains suppressed; dynamic, partial, flag, alias and tunnel hints preserved. Frame/completion checks cover visible text, color/NO_COLOR and four terminal sizes. [Resolution](https://github.com/Bochner/burrow/issues/104). |
| 6 | Verified readability change only | #104, `5f75bc6`: one frame Enter-guard parse; final validation, malformed-input errors, report validation, transfer review and binding before shell renumbering preserved. No parsing speedup measured. |
| 7 | Verified state simplification only | #99, `1ac01c0`: six return fields replace whole saved UI. Interaction checks cover prompt/cursor, recall/draft, output scroll, repeated exits, workspaces, cancellation and late-result rejection. No model-copy speedup measured. [Resolution](https://github.com/Bochner/burrow/issues/99). |
| 8 | Matched measurements; file-history speedup | #100: 71 before and 71 after samples, phases/process counts, cold/warm CLI and observed TUI, links and account faults. See comparison below. |
| 9 | Measurements; environment repair distinguished | #105, `eff0ba5` / `cc21bb7`: connection activation, review wait, prepare/adoption/start, attachment, first read and visible prompt separated. Same-binary PATH comparison identifies repeated eager initialization; no shell lifecycle optimization. [Report and raw samples](shell-startup-105.md). |
| 10 | Measurements only | #106, `4e4c821`: 240 one-byte interactions across four separate runs; input/queue/private call/PTY/output/snapshot/frame/visible landmarks. No polling or input-path optimization. [Report and raw samples](shell-interaction-106.md). |
| 11 | Measurements only | #107, `ee4e5a8`: traced and untraced 54-sample matrices at 1/2/4 attachments, idle/output, visible/hidden/background workspace. RPC counts, payload bytes and frontend/owner CPU measured independently. No scheduling optimization. [Report and raw samples](shell-attachments-107.md). |

## Matching file measurements

Recomputed from [before.json](file-history-100/before.json) and
[after.json](file-history-100/after.json), using each sample's `end - begin`.
Ten samples per repeated case; median / nearest-rank p95 in milliseconds.
For ten samples p95 is the maximum, not a population-tail estimate.

| Workload | Before | After |
| --- | ---: | ---: |
| CLI, absent history chain | 227.06 / 249.66 | 90.93 / 100.27 |
| CLI, established history | 224.24 / 246.90 | 94.13 / 101.96 |
| TUI, absent history chain | 395.47 / 433.17 | 301.20 / 327.50 |
| TUI, established history | 393.48 / 443.95 | 287.80 / 311.14 |
| CLI, 32 links plus target | 263.15 / 269.87 | 138.99 / 145.32 |
| CLI, slow account lookup | 2223.49 / 2230.91 | 2090.19 / 2092.32 |
| CLI, unavailable account lookup | 228.67 / 234.39 | 96.28 / 99.49 |

All 71 before samples launched two history setup processes; all 71 after
samples launched zero. Each listing still opened one SFTP subsystem and one
directory. Both reports have zero failed file samples and lab/command timeouts;
the ten slow-account samples per variant deliberately reach the two-second
account bound and return numeric fallback. A separate single first browse with
cold accounts took 259.32 → 98.73 ms; it is descriptive only. “Absent chain”
means a deleted history chain with warm accounts and an established approved
SSH master, not a cold OS cache or new login. Both entirely absent history
names are separately covered by the portable behavior check.

The matched source base was `cc21bb7` plus #100 measurement support, with only
the two history setup calls restored for the baseline. The
[baseline patch](file-history-100/restore-baseline.patch), measured script
hash, binary hashes, pins, workstation load and exact reproduction commands
remain in [#100's report](file-history-100.md). Both variants used the same
measurement script and tracing. The later failure-report robustness change
is disclosed there; it does not change successful samples.

Warm CLI history-write median fell from 131.65 to 1.96 ms. After-change owner
browse remained 15.88 ms, including 6.87 ms SFTP setup, 3.00 ms directory
metadata and 0.61 ms teardown. These nested spans are **not additive**.
The 32-link case still spends 44.07 ms in its sequential link calls; the
injected slow-account case spends 2001.09 ms in bounded enrichment.
TUI command-start delay remained 222.38 ms median, history read 1.37 ms,
result delivery 0.04 ms and accepted-result-to-observed-cells 41.90 ms.
Those independently summarized boundaries do not partition end-to-end time
or establish which frontend operation causes the remaining delay.

The TUI observer sees decoded VT cells, including PTY/decoder/scheduling
overhead, not physical display refresh. Raw same-master `ssh ls -la` medians
of 7.94 / 8.92 ms are a smaller-contract reference, excluding structured
metadata, history, audit and TUI presentation; they are not equivalent results.

## Separate shell findings

**Startup (#105).** With identical executable and fixed two-attachment
workload, nine additional TUI shells measured 3283 / 3368 ms with 33 Windows
PATH entries versus 785 / 834 ms without them (median / p95). Ten CLI prompt
observations measured 3749 / 3814 versus 680 / 725 ms. These are environment
comparisons, not before/after Burrow runtime changes. Standalone initialization
probes locate roughly 280–292 ms in eager clipboard discovery, repeated through
two Hovel CLIs and six Burrow processes per shell. The owner-authorized host
PATH repair was recorded in #105; existing processes keep inherited environments
until their normal reviewed restart. No live workspace restart is claimed here.

Connection activation is separate: single cold-manager observations were
3389 → 609 ms and warm-manager 1914 → 418 ms. CLI review wait was a synthetic
200 ms excluded from system work; the TUI shell command had no extra review
dialog. Prepare/start are not a usable prompt. First PTY output can be a banner;
adoption is bounded by module return and subsequent successful inspection,
not an internal daemon timestamp. Growing-tab scheduling outliers are separate
from the fixed comparison and remain incompletely attributed.

**Input to visible output (#106).** Four runs each contain 30 TUI bytes and
30 CLI-to-TUI-observer bytes; all 240 completed without failure/timeout.
Separate TUI medians were 66.23, 72.55, 61.98 and 67.11 ms; p95s 105.56,
104.88, 94.05 and 105.23 ms. CLI-to-observer medians were 117.02, 121.63,
117.15 and 127.08 ms. A roughly 5 ms private-call acknowledgement means
accepted PTY bytes, not remote completion or presentation. In traced run 1,
output-applied-to-snapshot p95 was 44.54 ms for TUI input; frontend sampling
and paint/observation add separate delays. These overlapping measurements
must not be summed. Traced/untraced runs vary with host load and do not show
an instrumentation speedup. The 4,000-line background switch-and-command
acceptance case is a different workload, not a per-key sample.

**Idle/background costs (#107).** The traced matrix has three five-second
samples per case (54 total), mirrored by 54 untraced samples with the same
binary. All completed without failure/timeout. Idle background-workspace
frontend CPU medians at 1/2/4 attachments were 43.7/77.2/150.1% traced and
63.6/89.5/154.9% untraced; one logical CPU is 100%. Owner CPU at four was
11.8% traced / 12.3% untraced. Traced RPC rates were 214.4/388.0/756.9 per
second. Four idle background attachments produced 79.9 snapshots/s and
364.1 KiB/s snapshot payload. Hiding tabs did not stop this work.

Counts include daemon identity checks and ordinary inventory work. Command
counts are subsets of RPC counts, never additional calls. Bytes exclude RPC
envelopes and transport. CPU excludes Hovel daemons, SSH children, the remote
workload and lab decoder; this is not total application CPU. The ordering,
shared workstation and three repeats limit causal claims; p95 is the maximum.
Traced/untraced differences change sign and do not isolate append overhead.

All four measurement families use the pinned Hovel v0.4.2 runtime
`c461ba282a8aecc7aa3a079a4613bf5e2640c388`, public SDK
`a4cbfdf7769a9551695088c11061e3cabc368e07`, Go 1.26.5, Aspect 2026.33.3,
Bazel 9.1.1 and the digest-pinned OpenSSH fixture. Full raw provenance remains
in the linked reports. #108 recalculated the cited distributions and setup
counts; it did not rerun benchmarks alongside acceptance work or invent
new timing samples. [Artifact hashes](general-use-108/measurements.json)
identify the exact retained evidence used.

## Conditional rows: disposition and remaining proof

“Supported” authorizes no implementation: it recommends a bounded future task.
“Insufficient” means the stated gate has not been demonstrated. “Not justified
currently” means the observed cost/benefit does not warrant that change now.

| Row | Candidate | Disposition and evidence / remaining proof |
| --- | --- | --- |
| 12 | Bazel-aware Go analyzer | Insufficient. No candidate has demonstrated loading the actual production graph and catching a relevant intentional defect without SDK/prototype noise. Formatting is not analysis. |
| 13 | Selected Python lint | Insufficient. Ruff formatting establishes no useful lint findings; first select rules with explicit assertion, fixture, shebang and subprocess treatment and show useful results. |
| 14 | Focused race-detector targets | Insufficient. Concurrent shell/control and owner paths are concrete candidates, but no selected detector-enabled target was proved here. Repeated setup/terminal scenarios are not Go race-detector coverage. |
| 15 | Bounded fuzz smoke | Insufficient. Select a concrete parser/trust-boundary risk, useful corpus, declared bounded execution and retained reproducible failures before adopting a gate. |
| 16 | Shared workspace inventory observation | Insufficient. `frame.go` copies inventory into file views, but meaningful removal and safe freshness/identity behavior have not been demonstrated. Preserve per-tab drafts and stale-result rejection in a proof. No model-copy bottleneck measured. |
| 17 | Narrow follow-view model | Insufficient. `follow_ui.go` still uses a broader UI; no bounded refactor has established meaningful benefit while preserving stream cursors, pause, selection and return state. #99 proves only file-mode return state. |
| 18 | Catalog completion fallback | Not justified currently. #104 repaired the demonstrated stale wording while retaining contextual guidance. No additional equivalent complete-candidate subset has been proved useful; partial/flag/alias/tunnel/connection hints must survive any later proposal. |
| 19 | Reduce hidden-tab polling/sampling | Supported, rank 1. #107 shows material idle work independent of visibility. Prove immediate fresh verified views on focus/input/control change, observer positions, controller geometry, generation/loss/gap reporting, uninterrupted owner PTY drain and retained lifetime. Measure before/after at the existing seam. |
| 20 | Consolidate discovery/inspection | Supported, rank 2. #107 shows repeated identity/discovery/inspect calls multiplying requests; #106 puts private-call cost near 5 ms. Prove exact session/module/kind/name, fresh peer/daemon identity, generation fencing and replacement/loss detection. RPC count is not a CPU attribution; no identity cache is justified. |
| 21 | Bounded screen-change wait | Supported, rank 3, after the smaller scheduling proof. #106 observes output-to-snapshot delay and #107 transfers unchanged screens. Prove revisions/wakeup races, cancellation, independent observers, authority changes and no wait holding locks needed by input/output. Reuse public session commands; native raw reads are not screen subscriptions. |
| 22 | Immutable chain-file setup | Not justified currently as the next optimization. #105 finds 19 small setup RPCs, with much larger repeated initialization/inspection cost. A later experiment still needs matched timings, immutable configuration/build binding, independent requests, confirmation, audit and failed-adoption cleanup; it cannot assume process inspection disappears. |
| 23 | Retain SFTP client | Not justified currently. #100 measures about 6.9 ms setup after history cleanup on loopback. That does not justify a new retained-client lifecycle. Representative costly setup plus cancellation, stale-owner refusal, close/reconnect cleanup and sibling isolation are prerequisites. |
| 24 | Parallel symlink metadata | Insufficient. 32 links cost 44.1 ms locally, but representative RTT/link distributions and useful bounded concurrency have not been measured. Preserve cancellation, limits, order, visible errors and no traversal through links. |
| 25 | Shorten/defer account lookup | Insufficient. The controlled timeout proves the two-second cost ceiling and fallback, not real slow-NSS prevalence. Establish ordinary workload cost, then preserve truthful numeric fallback, bounded work, freshness and stable metadata presentation. |

The first three recommendations are intentionally separate. Start with row 19
and remeasure before deciding whether rows 20–21 still justify their added
complexity. No conditional issue was published or implementation begun.
The startup evidence also supports investigating lazy clipboard initialization
and upstream inspection duplication, outside these numbered candidates;
copy/paste and immutable-build/freshness/confirmation acceptance would remain
necessary. Neither is silently folded into a shell lifecycle rewrite.

## Combined gates and CI scope

At source `ee4e5a852ec517d1e2a0a941c2de24a0b5a1efe8`,
`rtk proxy aspect burrow-check preflight` passed **45/45 targets** in 9m22s.
All tests ran uncached, with no failed-test retries; setup and terminal tests
ran three times each. Report collection accepted the unchanged source tree
`c49489a8a0efb6f2da10ea4e8fca117224e17269d068c26257b923afbaa3bd4d`.
[Target-by-target evidence](general-use-108/preflight.json) retains exact
selection, attempts, results, pins and log hashes.

This full invocation executes the portable checks plus all nine real SSH
partitions. In particular, `ssh_files_test` invokes `--files-check` (the same
file gate as `aspect burrow files-check`); `ssh_shell_test` invokes
`--shell-check`, including both `session_checks` and `shell_checks` (the shared
TUI gate), plus forwarding. Their [file log](general-use-108/ssh_files_test.log)
and [shell log](general-use-108/ssh_shell_test.log) are retained, so passing the
aggregate is not substituted for evidence that those paths actually ran.

The remaining commands ran sequentially against the same runtime/build/test
source at `ee4e5a8`; only this ticket's report/artifacts were added afterward.
Exact wrapper commands, exit codes, source fingerprints where collected and
log hashes are in [checks.json](general-use-108/checks.json).

| Command (all entered through `rtk proxy`) | Outcome |
| --- | --- |
| `aspect burrow-check preflight` | Passed 45/45, 9m22s; valid report collection. [Log](general-use-108/preflight.log). |
| `aspect burrow-check ci` | **Failed**, exit 3, 1m57s: 35/36 passed; only `hovel_wal_test` failed with “lost SQLite WAL lifetime lock: type=2 owner=0”. This strict portable gate is not green. [Log](general-use-108/ci.log). |
| `aspect burrow-check preflight hovel` | **Failed**, exit 3, 14.5s: 2/3 passed. Both manager diagnostics passed; WAL failed again. The collector retained a valid **FAILED** suite; unchanged source does not mean passing tests. [Log](general-use-108/hovel.log). |
| `aspect burrow-site check` | Passed both targets from cache; both had also executed uncached in full preflight. Covers generated book, links/assets/search and staging behavior after the combined docs/tooling changes. [Log](general-use-108/site.log). |

The current shared-TUI composite switch-and-command under 4,000 background
lines took 0.734 s. That is acceptance evidence, not a new per-key sample or
speedup. No failed tests were retried to turn a run green. Default strict full
mode was not separately rerun: the required full gate and advisory selection
above expose its production and three-diagnostic scopes without claiming that
its command passed. Required portable checks are included in the 45-target
preflight; the separate strict portable invocation reports its failure honestly.

CI source inspection confirms `.github/workflows/ci.yml` runs
`aspect burrow-check preflight <suite>` for portable plus lifecycle, files,
reverse, shell, chains, reports, automation, follow and runs. The portable
selection discovers `//:go_format_test`, `//:go_format_behavior_test`,
`//:python_format_test` and `//:python_format_behavior_test` through `//...`.
The aggregate repository job requires every partition, coverage and report.
This is configuration plus local execution evidence, not a new hosted CI run.

Go inputs come from `glob(["*.go"])` in the six declared production package
areas (including tests); the tool is `@burrow_go_sdk//:bin/gofmt` at 1.26.5.
Python includes production lab/check drivers, Aspect/GitHub tooling, docs,
agent and release tools, with pinned Ruff 0.16.3 and recorded archive SHA-256
in `MODULE.bazel`. Check/write share source/config/tool inputs. Prototypes
are excluded. Formatting does not imply static type analysis; Go production
and proof compilation occurs in the gates, while Python has no newly added
typechecker. Existing formatter behavior tests and prior removed-probe
evidence establish nonmutation, repair and syntax rejection.

The exact #86 exception remains `//core/launch:hovel_wal_test`,
`//core/prototype_manager:check` and
`//core/prototype_manager:consumer_check`. Required preflight excludes the
`hovel-followup` tag; the separate advisory workflow runs it visibly.
Strict `ci` includes WAL but excludes external manager diagnostics; default
full mode includes all three. No exception or failure retry was added.

## Contracts, cleanup and limits

Source inspection and the integration gates cover the unchanged single public
`burrow` module, public SDK/daemon calls and framed module stdout. Recap approval,
confirmed immutable throws, exact owner/build/generation binding and explicit
reconnect remain governed by [ADR 1](../adr/0001-manual-connection-approval.md).
Retained prepare/adopt/start, single controller, independent observers, explicit
takeover, geometry, detach/close and visible loss/gap recovery remain governed
by [ADR 2](../adr/0002-shared-interactive-sessions.md).
The [terminal standard](../agents/tui.md), meaningful metadata, NO_COLOR and
small-terminal behavior remain exercised by existing presentation checks.
The useful LazySSH workflow contract is not changed by this evidence-only ticket.

History keeps its names, source, successful-command filtering, audit and
uncertain-append no-retry rule. The #100 fixture fault is an explicit persistence
failure, not a lost-reply/exactly-once proof. A late asynchronous shell-release
acknowledgement can still replace subsequent management output; #100 synchronized
the sequential lab with the release outcome, not the production race. Hovel #86,
representative non-loopback/NSS performance, growing-tab frontend attribution,
multiple observers of one shell and broad workspace scaling remain limits.
Passing automated gates does not complete the deferred #65 owner walkthrough.

Temporary audit phase names/loop, formatter probe files and impossible readiness
marker are absent from current runtime/lab source. The historical audit patch,
baseline-restoration patch and fault JSON reports are evidence, not active
configuration. Retained measurement modes reuse the existing labs, cap latency
samples at 30 per path and attachment repeats at five, and enforce the existing
1,800-second outer bound plus individual waits. Private trace files are opt-in
through `BURROW_PHASE_TRACE` / `BURROW_ATTACHMENT_TRACE`, opened without symlink
following and checked for regular-file type, current ownership and mode 0600.
Records contain timing/operation metadata and payload lengths, not terminal
contents, private input or tokens. The time/sample bound is imposed by the lab;
manual tracing is not a size-rotating production telemetry service.

Existing untracked general-use audit research was preserved outside this commit.
It is included in the gate's source fingerprint, not silently adopted as an
approved spec. New evidence is limited to this report and its artifact directory.
Final Docker inspection found no remaining labelled Burrow connection-lab
containers. `rtk proxy aspect build //:research` passed after adding this report;
it verifies metadata inputs only, not prose or runtime behavior. Retained command
logs preserve their original tool-generated whitespace and hashes.
No live owner workspace was restarted; no push, PR, merge, deployment or release
was performed. Completion is published only to #108 under the implement skill's
closeout authorization; #98 is not edited or closed.

## Review

The implement skill's code-review ran independent Standards and Spec passes
against the prospective evidence-only diff from `ee4e5a8`.

- Standards: zero documented-standard breaches or actionable baseline smells.
- Spec: zero missing-scope or inaccurate-claim findings. The reviewer independently
  recomputed all distributions across ten raw variants and verified all 20 raw
  artifact hashes. File/shared-shell logs substantiate the named integration paths.

Final gate-result edits passed both review axes with zero findings; reviewers
also verified all four command-log hashes. Tracker publication follows the
verified local commit; conditional
follow-ups remain recommendations and no ready sibling remains under #98.
