# Shared-shell input to visible output (#106)

Measured 2026-09-26 for [#106](https://github.com/Bochner/burrow/issues/106),
measurement row 10 of [#98](https://github.com/Bochner/burrow/issues/98).
This adds opt-in measurement support, with no polling, ownership, protocol,
input-limit, lifecycle or runtime dependency changes.

## Result

An ordinary one-byte remote echo took **62–73 ms median** from TUI input
submission to decoded visible cells in four runs on this machine. Sending the
same byte through a fresh CLI process to a TUI observer took **117–127 ms**.
The private input call itself took about 5 ms. Its acknowledgement establishes
accepted PTY bytes, not remote execution or a visible result.

| Run | Phase trace | Samples per path | TUI median / p95 (ms) | CLI → TUI observer median / p95 (ms) |
| --- | --- | ---: | ---: | ---: |
| [1](shell-interaction-106/phased-1.json) | on | 30 | 66.23 / 105.56 | 117.02 / 150.59 |
| [2](shell-interaction-106/untraced-1.json) | off | 30 | 72.55 / 104.88 | 121.63 / 149.98 |
| [3](shell-interaction-106/phased-2.json) | on | 30 | 61.98 / 94.05 | 117.15 / 161.49 |
| [4](shell-interaction-106/untraced-2.json) | off | 30 | 67.11 / 105.23 | 127.08 / 165.57 |

All 240 interactions completed; each run reports zero failures and timeouts.
Each p95 is nearest rank, `sorted[ceil(0.95*n)-1]` (29th of 30); medians are
ordinary sample medians. Runs remain separate rather than pooling away run
variation. Tracing-on results are not an optimization: these sequential runs
on a shared workstation do not isolate instrumentation overhead. No before/after
speedup, command-completion guarantee or product SLA follows from this work.

## Reproduce

Use the existing declared Docker/OpenSSH fixture through Aspect:

```sh
rtk proxy mkdir -p /tmp/burrow-interaction
rtk proxy env TEST_UNDECLARED_OUTPUTS_DIR=/tmp/burrow-interaction \
  aspect burrow interaction-latency -- --samples 30 --phases
```

Omit `--phases` to measure without instrumentation. Use a different output
directory for each run. `--samples` accepts 1–30 per path, default 10. The
directory must exist. Without an output directory the JSON report is printed;
with one, `shell-interaction.json` and the existing `phases.jsonl` are retained.
The committed JSON files preserve every report value with compact formatting.
Raw phase spans for every sampled window are included in traced reports; each
also records the hash of its complete setup/cleanup trace, retained locally
under `/tmp/burrow-106-phased-{1,2}/`.

The new mode reuses `shell_checks`' actual frontend PTY, streaming VT decoder,
controller handling, terminal restoration and verified shell/master cleanup.
It starts a fresh disposable workspace/daemon and one approved SSH master,
then opens one retained shell at 98×35 inside a 160×40 TUI. Startup, connection
activation, takeover and raw-mode setup are outside the measurement windows.

The remote shell runs `dd bs=1 count=N` with terminal echo disabled and raw
input enabled. A split readiness marker avoids matching an echoed setup
command. Each sample sends one harmless printable byte and waits for the
growing marker **in decoded VT cells**, not raw ANSI, a prompt substring or an
input acknowledgement. The fixed-length command finishes after N bytes and
restores remote terminal settings. Sample spacing varies by 0–66 ms to vary
arrival relative to existing samplers. This is one-byte application echo,
not shell-command completion, sustained typing or a network-scale benchmark.

First the TUI controls the shell. Then the CLI explicitly takes over the
observed control generation and sends private JSON through stdin, while the
same TUI becomes an observer. Each CLI sample starts a new process. The lab
observes its completion alongside visible cells, so a visible result may
precede the observed CLI return. CLI return observation has the decoder loop's
cadence and is not an exact acknowledgement timestamp. The phase trace gives
the narrower private-call boundary. Control is explicitly released afterward.

## Clock, correlation and gaps

All timestamps use Linux `CLOCK_MONOTONIC` on one boot. One shell, one outstanding
byte and no unrelated output make serial window/PID correlation possible
without adding request IDs or private data to the runtime protocol. The lab
asserts every required phase exists and locates an owner snapshot after the
output update, a containing frontend refresh, a subsequent frontend sample,
and frame acceptance before the visible result. These are temporal landmarks,
not a distributed causal trace suitable for arbitrary concurrent sessions.

| Boundary | Meaning and limits |
| --- | --- |
| Lab `begin` | Immediately before outer PTY write or CLI process launch. Includes OS scheduling, input decoding/routing or CLI initialization before instrumented application code. |
| `shell-frontend-input` | Entry/exit of the shared terminal input handler after frame routing. It does not time Bubble Tea's input parser separately. |
| `shell-input-queue` | Immediately before enqueue through dequeue; normal validation remains before enqueue. The consumer then encodes input and checks authority. |
| `shell-input-rpc` / `shell-private:input` | TUI private-call wrapper / validated private command through result decoding. Includes exact-owner discovery and inspection, then the existing `call:RunSessionCommand`; spans nest. CLI has no TUI input queue. |
| `shell-pty-write` | Owner's bounded local PTY write. It measures the attempted write; the CLI checks one accepted byte and no backpressure/input error, and visible echo verifies delivery in both cases. This is not remote completion. |
| `shell-output-drain` | First positive local PTY read through owner screen/buffer update. No output bytes are recorded. It excludes time waiting in `Read`; lock wait and VT application are inside this span. |
| `shell-snapshot` | Owner snapshot rendering after acquiring the shell lock. The selected landmark follows the output update; preceding stale snapshots remain in raw evidence. |
| `shell-refresh` | Attachment observation RPCs through updating its local screen; includes owner validation, transport and decoding. |
| `shell-frontend-sample` / `shell-frontend-accept` | Copying the attachment screen under its lock / accepting the resulting screen in the frame. Samples can also occur during input handling; selected landmarks follow the relevant refresh. |
| Lab `visible` | First VT-decoded screen containing the expected echo. Includes rendering/flush, outer PTY delivery, scheduling and decoder overhead. It is not a physical monitor paint timestamp. |

The unchanged attachment refresh timer is 50 ms, with an immediate refresh
after successful input; frontend reads use a 30 ms timer. The immediate
refresh can precede remote output, so it need not contain the echo. The lab's
existing decoder waits/drains in up-to-50 ms intervals and has a bounded
three-second decoder response deadline. Kernel/SSH/network/remote scheduling
between local PTY write and returned bytes is not separated. Hovel broker
internals are not instrumented. A remote hardware timestamp is unavailable.

Phase spans overlap and cannot be added as a wall-time partition. In particular,
PTY output can arrive before the private-call acknowledgement. Snapshot and
frontend observations continue independently. Do not add nested phase medians
or turn RPC duration into visible-echo latency.

## Attribution

Representative traced run 1, 30 interactions per path; values below are
median / nearest-rank p95 in milliseconds. Landmark differences come from each
sample's `landmarks` and `begin`/`visible` fields. Queue/private durations use
their raw phase `ns` values.

| Interval | TUI | CLI → observer |
| --- | ---: | ---: |
| Submission → owner PTY write begins | 6.03 / 11.47 | 57.17 / 61.46 |
| TUI input queue | 0.061 / 0.193 | not applicable |
| Validated private input call | 5.21 / 5.85 | 5.48 / 5.92 |
| PTY write ends → first positive output read | 9.93 / 19.30 | 8.73 / 19.79 |
| Owner output applied → following snapshot begins | 12.47 / 44.54 | 22.21 / 41.06 |
| Refresh ends → frontend samples screen | 10.87 / 29.92 | 12.00 / 27.67 |
| Frame accepts sample → lab observes visible cells | 16.29 / 21.53 | 16.37 / 24.24 |

The extra CLI pre-write interval includes process startup and frontend setup;
it is not evidence that a headless controller's long-lived RPC stream has the
same cost. The native TUI queue is small in this quiet workload. Owner PTY
writes are roughly 0.04 ms and positive-read screen updates roughly 0.008 ms;
these are operation durations, not echo times.

## Evidence-supported follow-ups

- **Screen-change waiting (#98 row 21):** the measured output-to-snapshot wait
  reaches roughly 42–45 ms at p95, consistent with the unchanged timer. A
  bounded experiment is justified, but would need revision/wakeup correctness,
  cancellation, independent observers, authority and proof that waits do not
  hold locks required by input/output. Frontend sampling and renderer/lab costs
  would still remain. No predicted speedup is established here.
- **Discovery consolidation (row 20):** source and `call:*` evidence show
  `ListSessions`, owner inspect and the requested command in each private
  input/snapshot path. The full private call is about 5 ms here. This is a
  smaller measured target than observation waits; larger tab-count/RPC evidence
  in #107 should set its priority. Any consolidation must retain exact-owner,
  generation and loss/replacement checks; caching identities is not justified.
- **Hidden-tab sampling (row 19):** insufficient evidence in this single-tab
  workload. #107 owns idle/background scaling, CPU and snapshot byte counts.
  The existing background-output acceptance scenario remains separate.

These are recommendations for bounded follow-up scope, not implemented
optimizations or authorization to change the polling/session model.

## Provenance, privacy and validation

Source base `5f75bc644b0db51122c8a79d1dbda19c802e1196`, with this ticket's
instrumentation and lab changes on `audit/general-use-architecture`.
Reports preserve the tracked source-diff hash and hashes of the three lab
sources, including the newly added file. All four runs use the same binary:
`70d2e559c1037105ee0914a33c5b66fcf8295b5c5b32b98a7b4a7d07935c091f`.

- Hovel v0.4.2 runtime `c461ba282a8aecc7aa3a079a4613bf5e2640c388`, wheel SHA-256
  `7edccdabe04e30098e417d27ab48064c11a8612149124b7df0797251e88a0933`;
  public SDK `a4cbfdf7769a9551695088c11061e3cabc368e07`.
- Aspect `2026.33.3`, Bazel `9.1.1`, Go `1.26.5`, fastbuild. Full pin/build
  files, fixture digest and actual binary/wheel hashes are in every report.
- Local digest-pinned OpenSSH Docker server:
  `linuxserver/openssh-server@sha256:47f82202bcbffe214cea77dc7f5ed24b875e81ce6ebf0e0368e2954727bf5116`.
- Linux WSL2, AMD Ryzen 9 9950X3D, 32 logical CPUs. Shared workstation with
  one-minute load about 12–13, no CPU affinity or artificial network delay.
  Kernel, memory availability and initial/final load are retained per run.

Only the existing opt-in `BURROW_PHASE_TRACE` private append file is used;
records retain PID, fixed/validated phase name, monotonic start and duration.
No input bytes, screen contents, tokens, credentials or workspace paths are
added. The lab checks the trace schema and excludes its private control token.
Module stdout remains framed JSON-RPC. Production tracing is off by default;
the diagnostic limits samples to 30 per path and uses the existing 1,800-second
outer alarm. Individual interactions time out after 15 seconds. Interruptions
and failures produce counts and exception types without private exception
arguments; partial visible samples remain identifiable.

The initial two-sample red run reached all four visible echoes and then failed
on missing phase evidence. Adding the hooks made the three-sample run pass;
the four 30-sample runs then passed stricter chronological correlation. These
development runs are excluded from the reported 240-sample set.

Review found that the reused terminal wait reported deadline expiry as an
ordinary assertion, so readiness/cleanup timeouts could be undercounted. It now
raises `TimeoutError`. A temporary impossible readiness marker exercised the
real PTY wait: the [separate fault report](shell-interaction-106/timeout-probe.json)
records zero samples, one `TimeoutError` and `timeouts: 1`. Database integrity
and disposable-container cleanup still passed. The temporary marker was
removed. This fault is excluded from ordinary measurements; their recorded lab
hashes precede this exception-class-only correction. The compiled Go binary is
unchanged.

Focused `aspect test //core/terminal:host_test
//core/cmd/burrow:interaction_test`, `aspect burrow sessions-check`, and
`aspect burrow shared-tui-check` passed. They preserve controller/observer
isolation, generation refusal, takeover, geometry, independent output,
gap recovery, detach/loss, terminal restoration and input secrecy. The unchanged
composite switch-and-command test during 4,000 background lines passed at
0.700 seconds; it is not a single-byte latency sample. A one-sample
`aspect burrow shell-latency -- --samples 1 --phases` smoke check also passed
after sharing its existing measurement cleanup helper.

After the timeout correction, a normal three-sample traced run passed both
paths with no failures. Final `aspect burrow-check preflight` passed all **45
targets** in 9m 53s, including nine SSH partitions, production/package builds,
format checks, site checks, and three runs each of setup/terminal checks.
Tests ran uncached without failed-test retries, and the evidence collector
accepted the unchanged source. The log is retained locally at
`/tmp/burrow-106-preflight.log`; structured gate evidence is in
`.report-input/all/`. Only this completion prose was added after collection.
No disposable Burrow lab containers remained after the gate. No #86 exception
is broadened, and the deferred #65 owner walkthrough remains separate.

## Standards review

No documented-standard violations or actionable baseline smells. The review
confirmed reuse of the existing tracer, PTY/VT fixture, cleanup and Aspect
workflow, with privacy and retained-owner boundaries preserved.

## Spec review

The timeout-accounting finding above was corrected and exercised at the real
terminal-wait boundary. No other missing scope or implementation findings.
