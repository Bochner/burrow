# Retained workspace manager proof — #71

Owner performance acceptance, 2026-09-12: warm dispatch median improved from
2.980 s to 1.841 s (38.2%). The owner said this is sufficient for now, disputed
the 50% hard requirement, and prioritized getting other features working.
[ADR 0001](../adr/0001-manual-connection-approval.md) records this clarification.
Faster connections remain desirable; no ordinary-SSH comparison was measured.
Production integration stays in #73 after the separate #72 consumer/stream proof.

## Latency result

| Variant | Cold dispatch p50 / p95 | Warm dispatch p50 / p95 | Warm connected p50 / p95 |
| --- | --- | --- | --- |
| Existing per-connection path | 2.974 / 3.372 s | 2.980 / 3.334 s | 3.285 / 3.617 s |
| Retained manager | 2.889 / 3.153 s | 1.841 / 2.031 s | 2.049 / 2.248 s |

Each dispatch/connected group has 20 samples. Warm dispatch-to-password-prompt
p50/p95 was 0.240/0.254 s for baseline and 0.114/0.121 s for manager (10 each).
The original 50%/one-second warm criterion was not met; the cold nonregression
criterion was met. The owner's clarification removes the warm cutoff as a blocker.
[Raw samples and exact statistics](retained-manager-measurements.json) are retained.

## Implementation and provenance

The runnable source is [core/prototype_manager](../../core/prototype_manager/README.md).
All work stays local on `mvp1`; production application source is unchanged.
The fixture installs only the single base `burrow@0.1.0` in disposable workspaces.
It uses real confirmed activation and adapter throws, public session commands,
existing verified daemon/Unix-peer transport, and the private askpass helper.

Hovel is v0.4.2 at `c461ba282a8aecc7aa3a079a4613bf5e2640c388`, using the existing
pinned SDK overlay and wheel. Context7 resolved `/vibepwners/hovel` and provided
current session/launch-key documentation; the local pinned source controls
version-specific conclusions. No private Hovel package is imported.

The container is the existing declared OpenSSH fixture:
`linuxserver/openssh-server@sha256:47f82202bcbffe214cea77dc7f5ed24b875e81ce6ebf0e0368e2954727bf5116`.
Credentials are synthetic; only its loopback port is used. LazySSH's
`src/lazyssh/ssh.py` and `tests/test_ssh.py` at the existing research pin were
traced: ordinary identity selection is delegated to OpenSSH, an explicit key
adds `-i`, and command review precedes launch. Mocked LazySSH tests are not
counted as live authentication evidence.

## Observed behavior

- Two simultaneous first attachments produce one manager and one safe refusal.
  Two independent frontend processes see two real SSH masters owned by that
  manager. Equal connection names in another workspace remain independent.
- Activation and each connect create real Hovel plan, confirmation and run
  records. Connection summaries correlate generation/session, immutable request
  digest, creation ID and adapter run ID. A successful dispatch summary means
  an attempt started, not that authentication succeeded.
- Changed request bytes, duplicate creation, wrong generation, wrong session,
  wrong selected creation and cross-workspace routing refuse. Each request uses
  an isolated operation/chain. Both missing danger allowance and an unapproved
  live launch-key reviewer prevent connection dispatch.
- Synthetic explicit-key and private-password connections succeed. Cancelling
  a stalled password attempt closes only its creation. List plus sibling close
  during a stalled challenge took about 0.18 seconds in the first passing run;
  this responsiveness assertion is bounded at one second in the behavior check.
  Fixture files are scanned for the password; it travels only over the private
  same-user prompt socket and anonymous pipe, never Hovel metadata or argv.
- Dropping a frontend preserves the connections. Selected close preserves
  siblings and workspace evidence. Unknown runtime contents cause truthful
  cleanup refusal and remain untouched; retry succeeds after the harness removes
  its injected conflict.
- More than 256 inventory requests spanning multiple connections leave the owner
  manageable. One manager-wide budget emits at most 200 ordinary milestones and
  one suppression warning. This is neither complete durable logging nor a new
  audit database.
- Killing a manager ends its masters; the other workspace survives. Killing the
  daemon closes the module's input pipe and its masters end. The SDK's pinned
  `ServeIO` EOF path returns without `closeAll`; process exit and Linux
  parent-death signals provide termination. Remaining reservations are never
  adopted or removed as if their ownership were verified. Relaunch refuses and
  recovery/reconnect remains manual.

Session commands themselves do **not** authenticate a throw approval. The proof
checks the supported frontend → confirmed adapter → exact owner ordering and
correlation from #70; a supplied run ID is not a signed credential. It does not
claim isolation from arbitrary same-user callers of Hovel's privileged APIs.

## Final review coverage

The first Spec review found three gaps. The follow-up behavior check adds:

- **Cancellation and uncertain replies:** dismiss activation/connect before
  submission and assert no reservation; deliberately discard real confirmed
  activation/connect acknowledgements, then reconcile only the known generation
  and creation through public retained-session observations. Unknown generation
  and duplicate activation refuse. Post-dispatch prompt cancellation still closes
  only that exact attempt. The proof does not retry a request after uncertainty.
- **Manager-aware quit:** review both opened workspaces, keep or cancel without
  mutation, refuse changed inventory before close, and verify both inventories
  empty before reporting successful close. Each owner serializes its final
  review check and selected cleanup against connection admission. Unknown runtime
  contents preserve evidence and report incomplete cleanup; no rollback is
  promised. Existing production #76 terminal presentation remains separate.
- **Current identities:** `prepare` reuses the existing frontend parser before
  request hashing, capturing its current agent socket. A daemon started without
  that socket successfully uses the frontend's synthetic agent. A separate
  no-explicit-key request authenticates with a controlled SSH config identity.
  Operator key files are not touched; default-file selection remains delegated
  to OpenSSH, not replaced by a hard-coded filename.

#72 consumer/stream work is not established by the harmless forwarded probe;
no generic Mesh bridge or production manager migration was added.

## Measurement method

The baseline is a declared build of the actual current per-connection source,
with only monotonic phase timestamps and the same accepted host-trust flags
injected by an anchor-checked generator. A small non-TUI wrapper calls the existing
`Execute` / `ExecutePrompt`. Thus these are fresh comparable control-path samples,
not timings of the full shipping TUI binary or historical CLI-return observations.
Both variants preserve installation/catalog checks and real supported throws.

The monotonic submission timestamp precedes local orchestration, including cold
manager activation. Dispatch is the owning handler's entry before SSH work;
connected is verified-master publication. Prompt readiness is measured separately
and includes server/network time, with a synthetic 20 ms reply delay. Human time
is excluded. Cold follows a clean daemon restart with the package installed and
no manager. Warm follows on that same daemon. The harness measures startup and
installation separately, but the original run did not retain their aggregate
record before the later trace failure. No startup-cost claim is made here.

There are 20 cold and 20 warm **untraced** samples per variant, split equally
between keys and passwords. Separate traced key/password cold/warm pairs count
successful executable starts, including short-lived CLI/module/OpenSSH children;
Go threads and failed PATH searches are excluded. Initial traced timing trials
were discarded because ptrace overhead was substantial and variable. Their
numbers are not included in acceptance percentiles. All final samples use the
same host, fixture, pinned Hovel and Aspect fastbuild mode.

## Measurement limits and validation

All 80 untraced phase samples completed. The subsequent optional trace pass
failed on the manager password case; a count-only retry also timed out there.
The retained partial count pass observed baseline key/password cold/warm and
manager key cold/warm, but is not a complete process-count comparison. Trace
failures do not invalidate the earlier untraced samples and are not counted as
passing measurement runs. No further benchmarking is being done per owner
priority.

Host identity and binary hashes were captured before subsequent correctness
edits; load during samples and the original aggregate startup record are missing.
The evidence JSON identifies these limits. Measurements belong to that sampled
build, before the prepare/reconcile/quit additions, not an unmeasured claim about
the final binary. Future runs emit provenance and startup records incrementally
and keep tracing optional so an interrupted trace cannot hide phase evidence.

`aspect burrow-prototype manager-check` passed the expanded behavior cases.
The full gate also passed the final manager check, including simultaneous close
from two frontends; stalled-prompt list plus sibling close took 0.171 s in that run.
`aspect burrow-check` passed: production/proof packages and metadata build,
18 portable checks (cached), the final manager lab, and the existing production
SSH/PTY acceptance lab. This verifies production regression coverage as well as
the new disposable manager behavior; it is not a final-build latency benchmark.

## Standards

No outstanding findings. The initial measurement-description finding was fixed.
Follow-up review caught concurrent closes overwriting an already-closed entry
with cleanup-pending; a second locked closed-state check fixes it, and the real
fixture exercises simultaneous cancellation and verifies inventory removal.
No additional documented-standard violations or actionable heuristic smells.

## Spec

No outstanding actionable findings in the bounded proof. Frontend agent/config
selection, exact-identity reconciliation and manager-aware quit now have focused
checks. The review accepts the explicit distinction between sampled builds,
partial measurements and final correctness additions. #72 and #73 retain their
separate consumer and production-integration responsibilities.

Review totals: Standards 0 outstanding; Spec 0 outstanding; no remaining issue
on either axis. No production migration, push, PR or issue closure is implied.
