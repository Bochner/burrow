# Quality checks (#98 Track A, rows 12–15)

Owner-requested implementation on `audit/general-use-architecture`, starting at
`c2131ffeffb0fb94e211e214781763660e4be2d1`. This report covers only
[#98 Track A](https://github.com/Bochner/burrow/issues/98). Existing untracked
audit notes remain separate. No runtime dependency, public module, approval,
owner-routing or performance behavior changes are introduced.

| Row | Delivered check | Evidence and limit |
| --- | --- | --- |
| 12 | Bazel-integrated `printf` analysis over the six production Go package areas, including tests | The real frontend build loads its production dependencies and rejects an intentional format/type mismatch. Prototype/upstream diagnostics are excluded. This is one diagnostic analyzer, not broad Go lint parity. |
| 13 | Selected Ruff lint over the existing Python formatter inventory | Undefined-name, broken assertion and discarded-expression probes fail without rewriting files. Executable shebangs, assertions, embedded fixtures and deliberate subprocess failures pass. No blanket lint policy or prototype churn. |
| 14 | `host_race_test` reuses the real PTY tests with `race = "on"` | Output drain, input/resize, snapshots, history and close/cancel execute with the detector. An intentional unsynchronized read/write fails with `DATA RACE`. No exhaustive owner/transfer race claim. |
| 15 | Coverage-guided `FuzzCommandLine` on the embedded production parser | Ten seconds, one worker, one-second minimization, 30-second deadline. Generated failures persist in test outputs and replay through Aspect. This is a smoke check, not exhaustive fuzzing. |

## Tools and graph

All execution enters through Aspect; cacheable source/tool/config/corpus inputs
are declared in Bazel. No root/core Go module or second task runner was added.
Existing pins remain: Aspect 2026.33.3, rules_go 0.61.1, Go 1.26.5, Python 3.12,
Ruff 0.16.3 and its existing release SHA256. The analyzer reuses rules_go's
x/tools v0.34.0 dependency and checksum through its `TOOLS_NOGO` label list,
selecting only `printf`. Hovel runtime/SDK pins remain unchanged.

`go_sdk.nogo` enables diagnostic validation for `core/agent`, `cmd/burrow`,
`connection`, `launch`, `reports`, and `terminal`; dependencies may supply
analysis facts but do not introduce their own diagnostics into the gate.
The production frontend imports all six packages. Builds and tests therefore
use the actual production graph rather than the historical prototype module.

The first analyzer run found an existing `fmt.Fprintln` with a trailing newline
in collected notes. Changing it to `fmt.Fprint` with an explicit second newline
preserves the exact bytes, metadata and presentation. The existing interaction
checks passed. No behavioral repair or latency improvement is claimed.

The selected Ruff rules are `F601`, `F602`, `F631`, `F632`, `F634`, `F821`,
`F822`, `F823`, `B012`, and `B018`. They target broken checks without unused
fixture/import churn. `S101` is excluded because assertions are acceptance
checks; `EXE` is not enabled and existing shebangs/modes are preserved.
`PLW1510` is not enabled: drivers explicitly inspect return codes and perform
deliberately failing commands/cleanup, where unconditional `check=True` would
replace contextual diagnostics. This decision does not remove existing
subprocess checks. Lint and formatting share the same automatically discovered
46-file production/tooling scope.

## Failure demonstrations and retained checks

- [Initial Go finding](quality-track-a/go-baseline.log) and
  [intentional production format/type defect](quality-track-a/go-defect.log):
  the actual collected-notes call changed temporarily to
  `fmt.Fprintf(out, "%d", "track-a-format-probe")`; the build rejected its
  wrong argument type. The probe was restored.
- `.aspect/python_format_test.py` now retains positive fixture/shebang/assert/
  subprocess examples and negative undefined-name (`F821`), tuple assertion
  (`F631`) and discarded-expression (`B018`) cases. Each negative case requires
  the diagnostic and unchanged source bytes/mode.
- [Detector failure](quality-track-a/race-defect.log): a temporary test in the
  existing host test file performed unsynchronized increments in two goroutines.
  The declared race target reported both `WARNING: DATA RACE` and
  `race detected during execution of test`. The probe was removed; the actual
  PTY tests then [passed](quality-track-a/focused.log).
- Fuzz seeds cover empty arguments, mixed quoting/escaping, Unicode, NUL/invalid
  UTF-8, unfinished input, and shell metacharacters as literal data. Valid UTF-8
  literal arguments must survive serialization/parsing unchanged; arbitrary
  parsed input must survive recall. Appended unfinished quotes/escapes must fail
  without partial executable arguments. Invalid UTF-8 still exercises parsing
  and recall, but does not assert byte preservation across rune decoding.
- A temporary fuzz assertion rejected generated one-byte strings. Native fuzzing
  [minimized it to `"0"`](quality-track-a/fuzz-defect-test.log), retained corpus
  hash `771e938e4458e983`, and the copied corpus
  [reproduced the failure](quality-track-a/fuzz-replay.log) through Aspect with
  fuzzing disabled. The [input](quality-track-a/intentional-fuzz-input.txt) is
  diagnostic evidence, not a production regression. The assertion and temporary
  checked-in corpus were removed. The probe initially assumed zipped outputs;
  this host retains an ordinary directory, which was used for replay.

The fuzz target uses the pinned compiler's `-d=libfuzzer` on the test archive
embedding production sources, not only on the callback. `TestMain` copies any
checked-in regression corpus into Bazel's writable retained outputs; workers use
that same directory. The existing CI diagnostic upload includes `test.outputs`.
No new failure-artifact service or CI exception is needed.

## Reproduction and verification

```sh
aspect burrow go-analysis
aspect burrow python-lint
aspect burrow race-check
aspect burrow fuzz-check
aspect burrow-site check
aspect burrow-check preflight
```

Fuzz-check is uncached. The required preflight is also uncached and automatically
discovers the new Python lint, real-PTY race and command fuzz tests. Setup and
terminal scenario repetition remains distinct; the existing three-diagnostic
#86 advisory exception is unchanged.

To replay a real future failure, copy its retained
`testdata/fuzz/FuzzCommandLine/HASH` into
`core/connection/testdata/fuzz/FuzzCommandLine/`, then run:

```sh
aspect test //core/connection:command_fuzz_test \
  --bazel-flag=--test_arg=-test.fuzz= \
  --bazel-flag=--test_arg=-test.run=FuzzCommandLine/HASH
```

[Focused checks](quality-track-a/focused.log) pass all six selected targets;
[interaction and initial fuzz checks](quality-track-a/fuzz-interaction.log)
also pass, as do the [production analyzer build](quality-track-a/go-green.log)
and [site checks](quality-track-a/site.log). Retained log transcripts normalize
trailing whitespace only. The final integrated gate result is recorded below.

## Standards review

Zero findings. Independent review found no documented-standard violations or
actionable baseline smells. Aspect/Bazel scope and pins, native fuzz
instrumentation and corpus retention, existing PTY race coverage, lint fixtures
and byte-preserving output cleanup conform to repository requirements.

## Spec review

Zero findings. Independent review verified the four row-specific demonstrations,
bounded scope and preserved #86 exception. Both reviews leave integrated
verification as the completion prerequisite. Review totals: Standards 0; Spec 0.

## Final integrated verification

`aspect burrow-check preflight` passed **48/48 targets in 10m 9s**, uncached
with no failed-test retries; source-bound report collection succeeded. This
includes the three new quality targets, production/proof packages, all nine
SSH partitions, documentation, and three attempts each of setup and terminal
checks. The [complete gate log](quality-track-a/preflight.log) and
[summary extracted from the collected metadata](quality-track-a/preflight-summary.json)
record the result, attempt counts and tested source fingerprint. No disposable
Burrow lab containers remained. Only this completion record and its evidence
were added after the gate; implementation, test and book sources are unchanged.

The prior independent Standards and Spec reviews each have zero findings.
The final gate satisfies their outstanding verification prerequisite. Rows
12–15 are complete. The exact #86 three-diagnostic release exception is unchanged;
advisory diagnostics were not rerun, and no strict-green or upstream-fix claim
is made. The deferred #65 owner walkthrough is unchanged.

Next in the agreed grouped sequence is **#98 Track B — UI cleanup (rows 16–18)**:
shared workspace inventory observations, narrower follow-view state, and
selective catalog descriptions. Its scope is already recorded but implementation
awaits an owner request; Track C performance work is also untouched.

## Primary implementation references

Context7 documentation was checked against the fetched rules_go 0.61.1 and
Go 1.26.5 sources before implementation:
[nogo scope](https://github.com/bazel-contrib/rules_go/blob/v0.61.1/docs/go/core/bzlmod.md),
[analyzer labels](https://github.com/bazel-contrib/rules_go/blob/v0.61.1/go/def.bzl),
[analyzer dependency pin](https://github.com/bazel-contrib/rules_go/blob/v0.61.1/go.mod),
[race modes](https://github.com/bazel-contrib/rules_go/blob/v0.61.1/go/modes.rst),
[Go fuzz instrumentation](https://github.com/golang/go/blob/go1.26.5/src/cmd/go/internal/work/init.go),
[Ruff rule selection](https://docs.astral.sh/ruff/linter/).
