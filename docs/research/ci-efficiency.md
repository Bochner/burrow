# CI/CD efficiency research and proposed changes

Research date: 2026-09-28. The observations below describe the inspected baseline.
The owner subsequently authorized implementation on `research/ci-efficiency` and
explicitly requested copying Hovel’s report format and enforcement. GitHub documentation was checked through
Context7 (`/websites/github_en_actions`) and the official documentation site.
Repository-specific observations and the implementation sequence accompany the
external findings below.

## Recommendation

Keep the full application gate on PRs. Replace the routine second full run on
`main` with verification that the merged tree is the tree already tested,
final-package build/smoke checks, and Pages publication. Make Release promote
that verified package bundle to GitHub independently of optional PyPI publishing.
Keep a full-check fallback whenever evidence cannot be safely reused.

Implementation stays local on the current branch until the owner requests the
PR. No repository visibility, protection, publishing environment or release is
changed. The owner confirmed Burrow is public; current documentation is corrected.

## Initial owner direction and implementation

The follow-up instruction “we should copy hovel on both accounts” supersedes the
initial proposal below to extend Burrow's strict report provenance contract.
The active Hovel report JavaScript/CSS, page shell and Python data model are
copied at `1789ce47554a2ee17c7bd46d391b63946af60d0a`. Reports enforce structured
results, referenced evidence and operational parity; the extra same-source/run,
per-file-hash and site-hash publication guards are removed. Pages follows Hovel's
successful main artifact promotion and retains the current-main guard.

Sources: Hovel [report model](https://github.com/vibepwners/hovel/blob/1789ce47554a2ee17c7bd46d391b63946af60d0a/tools/testreport/testreport.py),
[active report application](https://github.com/vibepwners/hovel/blob/1789ce47554a2ee17c7bd46d391b63946af60d0a/docs/site/public/assets/report.js),
[materialization](https://github.com/vibepwners/hovel/blob/1789ce47554a2ee17c7bd46d391b63946af60d0a/docs/tools/docs/materialize_site.py),
[parity gate](https://github.com/vibepwners/hovel/blob/1789ce47554a2ee17c7bd46d391b63946af60d0a/core/cmd/hovel-operator-parity/main.go).

The pipeline separately verifies eligible successful GitHub runs, artifact ZIP
digests and tested tree/merge parents before reusing evidence. A small `ci.json`
receipt identifies the producing source/run and inherited validation; this is
CI selection metadata, not an enforced report format. Original suite files are
copied unchanged. Missing, invalid, expired or unsupported evidence runs the full
gate. Same-repository merge commits and already successful identical main runs
are supported; squash/rebase/fork/forced pushes conservatively run fully.

Prose additions/edits can reuse an exact successful main baseline only when the
application input digest matches. New docs still run their declared checks.
Unknown paths, deletions and renames select full verification. The required
`repository` aggregate explicitly accounts for all selected and skipped jobs.
Routine `verify` permits deterministic test caching; external/process/race/fuzz
checks stay fresh, setup/terminal retain three repetitions, and no failures are
retried. Fully uncached `preflight` and manual `force_full` remain available.
`requires-docker` distinguishes local environment prerequisites from Bazel's
`external` always-execute caching tag.

Main builds and smoke-tests the exact archive/wheel bundle. Manual Release
fetches only the exact successful main bundle, validates its identity and
checksums, and supports rehearsal, GitHub, or GitHub plus PyPI. GitHub does not
depend on PyPI. An expired bundle for current main can be rebuilt with an explicit full Repository
rehearsal. Older expired revisions require preparing a new release revision on main. Evidence/site/bundles retain 30 days; duplicate bulk
failure diagnostics retain seven. Existing cache families are retained. No
remote cache service, extra matrix partitions or scheduled full runs are added.
Coverage remains separately instrumented; replacing normal test execution with
coverage has not been demonstrated equivalent.

The remaining acceptance is the owner-requested PR/merge and hosted rehearsal.
Compare its runner time with the baseline; no new hosted saving is claimed yet.

## Follow-up: automatic releases and test execution (2026-09-28)

After merging PR #114, the owner requested automatic release tags and removal
of avoidable test repetition on `ci/automatic-releases-and-test-efficiency`.
They selected releases when `VERSION` changes, with required version increments
and matching changelog entries recorded in `AGENTS.md`. This supersedes the
initial recommendation above to retain three setup/terminal repetitions and
separate normal executions of the coverage targets.

Successful main Repository CI now triggers Release. A version increase creates
`vVERSION` at the tested commit and publishes that run's exact bundle with its
`CHANGELOG.md` notes. An unchanged version skips; downgrades, missing notes,
conflicting tags, failed CI and wrong source identity fail. Reruns preserve
existing tags/assets. PyPI remains a manual choice. Version 0.2.2 is prepared
locally; this branch has not been pushed or published.

The six `production-coverage` targets execute only under coverage in a full
gate; the report uses their passing assertions for behavior/parity too. Setup
and terminal suites execute once. Focused race checks, internal regression
loops and every SSH/Hovel partition remain. Explicit diagnostic repetition is
still available. Prototype binaries build when selected tests depend on them;
the extra blanket prototype packaging pass is removed.

The full uncached local preflight passed in 9m 25s: 47 ordinary targets followed
by six instrumented targets. The prior local gate recorded 57 ordinary test
executions plus six coverage executions for the same 53 distinct targets.
The new gate therefore removes ten duplicate executions without removing a
target. Comparing the old and new label sets found no missing or added targets
and no duplicate labels. Site checks and the assembled release report also
passed with all 53 results and measured coverage. This local elapsed time is
not a hosted-runner performance comparison.

The report behavior test keeps its cases and one external CLI smoke check,
calling the real CLI entry point in-process for subsequent cases. On the same
local machine, its uncached execution fell from 14.3s to 0.8s. New checks cover
single-run evidence, coverage supplying parity, and duplicate-target refusal.
Release tests also exercise version/changelog/tag guards and the triggering
CI run's identity. CI syntax is checked by the existing pinned actionlint gate.

Each PR's required version bump selects full application validation; deterministic
caching still applies. Main can reuse the identical passing PR tree, and release
promotion does not rerun the application gate. Hosted PR, merge and automatic
publication still need the next owner-authorized end-to-end run.

## Burrow: measured current state

Inspected source: `8ad32054fb26241d7c86d85bf0e9618533e08f27`.
Live API queries were read-only on 2026-09-28. Times below sum job
`completed_at - started_at` for attempt 1; they are runner occupancy estimates,
not billing records, and exclude the separate Pages workflow.

| Run | Jobs | Summed runner time | Workflow elapsed |
| --- | ---: | ---: | ---: |
| [PR #113 validation](https://github.com/Bochner/burrow/actions/runs/36418855407) | 14 | 37.83 minutes | 8m 57s |
| [Its merged main validation](https://github.com/Bochner/burrow/actions/runs/36419825472) | 14 | 37.25 minutes | 9m 11s |
| [First main run for #112](https://github.com/Bochner/burrow/actions/runs/36415996808) | 14 | 39.20 minutes | Not used in estimate |
| [Second main run for the same #112 commit](https://github.com/Bochner/burrow/actions/runs/36416022786) | 14 | 39.15 minutes | Not used in estimate |

The newest PR/main pair consumed **75.08 runner-minutes**. The two #112 runs
both name `5154138c50ec40ec0af3e8e9e60dd5306b696279`, event `push`, attempt 1,
and `.github/workflows/ci.yml`; they started 15 seconds apart with different
check-suite IDs. They consumed 78.35 minutes together. The API establishes
duplicate execution, but not why two push runs were created. Do not attribute
this to the release workflow or claim that concurrency alone deduplicates
completed work.

The live [repository metadata](https://api.github.com/repos/Bochner/burrow)
reports **public, personal-account owned**, despite local instructions/docs
describing a private repository. Standard hosted runner minutes in public
repositories are free under GitHub's current policy; these measurements support
reduced compute and waiting, not a claimed dollar saving. Visibility was not
changed, and account billing was not inspected.
[GitHub Actions billing](https://docs.github.com/en/billing/concepts/product-billing/github-actions).

### What actually repeats

- [Repository CI](https://github.com/Bochner/burrow/blob/8ad32054fb26241d7c86d85bf0e9618533e08f27/.github/workflows/ci.yml)
  runs on every PR and main push without change selection. Its eleven matrix
  suites, separate coverage, report assembly, and final `repository` check
  produce fourteen jobs. PR cancellation, pinned actions, least-privilege
  permissions and a final required aggregator are already present.
- [The Aspect gate](../../.aspect/check.axl) forces uncached test results in
  every mode, not just releases. Preflight also repeats setup and terminal
  targets three times; retries of failed tests are disabled. The separate
  [coverage task](../../.aspect/report.axl) executes six production targets that
  the portable suite also runs, with different instrumentation. Coverage is
  useful evidence, so merging those executions requires checking that the
  instrumented execution can satisfy the existing behavioral contract.
- The latest main `portable` job took 466 seconds; `report` then took 71 seconds.
  The other acceptance jobs took 92–255 seconds. The slowest path is portable
  plus report, not a shortage of SSH partitions. More matrix jobs are not the
  first optimization.
- [Pages](../../.github/workflows/pages.yml) already downloads a successful
  main run's exact `docs-site` artifact. It does not rebuild/retest the whole
  application. It verifies the report/site digest, requires the originating
  main push, and avoids deploying a superseded main commit. Preserve this work.
- [Report collection](../../docs/tools/docs/report.py) currently requires the
  exact source snapshot, commit, CI run ID, every required suite, and log hashes.
  Publication checks the same commit again. Cross-run or cross-commit reuse is
  intentionally rejected today; it needs a tested provenance extension.

The [main protection API](https://api.github.com/repos/Bochner/burrow/branches/main/protection)
reports required `repository` status with `strict: true`, enforced for admins,
and required PR reviews configured with zero approving reviews. The ruleset list
is empty. Merge, squash and rebase are all enabled. Keep protection intact;
do not assume that every allowed merge strategy preserves commit identity.

### Direct evidence of the redundant source tree

The downloaded `evidence-hovel` artifact for PR run `36418855407` records the
actual checkout commit `f7dff25baeefc6a85810a534951312aac447addf`. The Actions
run API instead reports PR head `46cd81a7a5b632cb83ab1c785117d5bae1d918cf`.
Selecting evidence solely by that API head SHA would identify the wrong tested
commit. The artifact records a clean tree and run/attempt identity.

The Git commit API confirms that the
[tested merge commit](https://api.github.com/repos/Bochner/burrow/git/commits/f7dff25baeefc6a85810a534951312aac447addf)
and [actual main commit](https://api.github.com/repos/Bochner/burrow/git/commits/8ad32054fb26241d7c86d85bf0e9618533e08f27)
have identical parents and Git tree `d437f8fb66a2d2dd7480b6506215a33b70e0db1f`.
This proves equivalent tracked source for this pair, not identical runtime
environments or a general guarantee about future merges.

### Release is unused, and its purpose differs from the actual release path

The [Release runs endpoint](https://api.github.com/repos/Bochner/burrow/actions/workflows/release.yml/runs)
reported `total_count: 0`. The workflow only supports manual dispatch: pushing
a tag does not start it. With publication enabled, it runs another full
preflight, builds/checks a wheel, publishes to PyPI, and only then attaches the
wheel to a GitHub prerelease. With publication disabled it only rehearses.
[Release workflow](../../.github/workflows/release.yml).

The actual [v0.2.1 release](https://github.com/Bochner/burrow/releases/tag/v0.2.1)
has an archive, a wheel and `SHA256SUMS`, and is not marked prerelease. The
[release guide](../site/src/content/spec/releasing.html) explicitly describes
building locally and attaching GitHub assets, with PyPI setup deferred. Thus
the unused workflow is prepared PyPI infrastructure, not the current GitHub
release implementation. Its lack of runs is not itself proof of a trigger bug.
Removing it would save no existing runner minutes; making it useful removes
manual release work and avoids future third full gates.

The existing wheel test builds its own temporary wheels. It proves deterministic
packaging and installed behavior, but does not directly accept the final
`dist/*.whl` file for testing. Add final-artifact smoke capability by reusing
this test's install logic before claiming build-once/test-once promotion.
[Wheel test](../../core/tools/release/wheel_test.py).

### A repeated check did catch real failures

[PR #110 validation](https://github.com/Bochner/burrow/actions/runs/36364553415)
passed; [its main run](https://github.com/Bochner/burrow/actions/runs/36412984899)
failed `activity_test` and `host_race_test`. Logs show an activity assertion and
`TestBoundedPTYHistory`; [PR #112](https://github.com/Bochner/burrow/pull/112)
then fixed activity capture and PTY output-drain races. Removing repeated
execution reduces another opportunity to expose timing bugs. Keep current race
checks and the three-run setup/terminal requirement on PRs; do not hide flakes
with automatic success-seeking retries. Use targeted repetition when a race is
under investigation, and retain an explicit full verification dispatch.

## What current Hovel does

Inspected Hovel main `1789ce47554a2ee17c7bd46d391b63946af60d0a`, not just the
older revisions cited in Burrow research.

| Area | Hovel today | Useful lesson for Burrow |
| --- | --- | --- |
| Validation triggers | PR, main push, merge group and manual dispatch; six normal matrix scopes plus docs and Wine jobs | Hovel does not eliminate PR/main duplication. Keep scope separation, adapt its trigger policy. |
| Cache/execution | BuildBuddy remote action cache and remote execution when credentials exist; separate GitHub dependency caches; local fallback without credentials | Reuse stable build outputs. Remote compute is a separate cost/resource, not free work eliminated. |
| Checks | Aspect scope tasks; no blanket `--nocache_test_results` in its normal check task | Keep deliberate fresh acceptance runs separate from safely reusable deterministic results. |
| Distribution | Normal CI builds release artifacts; tag/manual Release still verifies, independently checks reproducibility, and builds/smokes packages | Package evidence matters; Hovel is more conservative than the proposed Burrow promotion path. |
| Pages | Successful main CI artifact is downloaded for automatic deployment | Burrow already follows this useful pattern. |
| Diagnostics | Failed-test logs uploaded on failure, seven-day retention; invocation/timing summaries | Keep evidence required by Burrow reports; avoid redundant bulk logs on green runs. |

Sources: Hovel
[CI](https://github.com/vibepwners/hovel/blob/1789ce47554a2ee17c7bd46d391b63946af60d0a/.github/workflows/ci.yml),
[setup action](https://github.com/vibepwners/hovel/blob/1789ce47554a2ee17c7bd46d391b63946af60d0a/.github/actions/setup-hovel/action.yml),
[cache selector](https://github.com/vibepwners/hovel/blob/1789ce47554a2ee17c7bd46d391b63946af60d0a/repo-tools/tasks/configure_bazel_cache.py),
[Bazel configuration](https://github.com/vibepwners/hovel/blob/1789ce47554a2ee17c7bd46d391b63946af60d0a/.bazelrc),
[check task](https://github.com/vibepwners/hovel/blob/1789ce47554a2ee17c7bd46d391b63946af60d0a/.aspect/check.axl),
[Release](https://github.com/vibepwners/hovel/blob/1789ce47554a2ee17c7bd46d391b63946af60d0a/.github/workflows/release.yml),
[Pages](https://github.com/vibepwners/hovel/blob/1789ce47554a2ee17c7bd46d391b63946af60d0a/.github/workflows/pages.yml).
The [live release history](https://github.com/vibepwners/hovel/actions/runs/36361280239)
confirms a successful tag-triggered release at this revision.

## Initial proposed implementation sequence (report contract superseded above)

### 1. Make the release workflow serve the release process

Keep the existing CI gates initially. Have successful main verification produce
one release bundle: Linux archive, wheel, checksums and a provenance manifest.
Smoke-test the exact bundle files, then upload them under that immutable commit
and run identity. Reuse the current Aspect packaging/release targets and Python
tools; avoid another build system or release service.

Adapt manual Release dispatch to select an explicit existing tag and publication
destination: rehearsal, GitHub, or GitHub plus PyPI. GitHub publication must not
depend on PyPI configuration. Reuse the successful exact-main run's bundle
instead of running `preflight` again. Validate tag/VERSION/main membership,
required validation, artifact identity and checksums. Retain owner-controlled
publication, scoped write/OIDC permissions, and PyPI digest verification.
Make prerelease status explicit instead of hard-coding it for every release.

Keep evidence/packages for an initial 30-day release window; retain bulky
diagnostics for seven days. If required artifacts have expired, fail clearly
or perform an explicit fully validated rebuild of that exact revision. Never
substitute the latest run's files. This delivers useful release automation
without waiting for the larger report-provenance change.

### 2. Remove the routine second full gate, after provenance support lands

Keep `repository` as the required PR check and keep the complete existing
behavioral/race/coverage suite for code changes. The main workflow should:

1. Find the successful required run for the PR actually integrated into this
   main revision. Initially support the demonstrated same-repository merge
   case; use the full fallback for unsupported/fork/ambiguous cases.
2. Verify workflow, repository, run/attempt, PR/base relationship and all
   required results against GitHub; compare the recorded actual tested Git
   tree and full input digest with the main checkout. Do not rely solely on
   artifact-supplied metadata or the run API's PR head SHA.
3. Reuse matching test evidence without editing its original execution SHA or
   run ID. Extend the existing report model to distinguish tested revision,
   main revision and the verified equivalence between them. Build and validate
   commit-sensitive package/site metadata for main. Preserve artifact hashing
   and the current final-main deployment guard.
4. Build/smoke the final bundle and produce the eligible Pages artifact from
   main, retaining the original test evidence and accurate attribution.
5. If any proof is missing, failed, expired, unsupported or mismatched, execute
   the full existing gate on main and use that fresh evidence. Workflow/build
   input changes should conservatively use this path during rollout.

This can remain in the existing workflows and report/release helpers. There is
no need for a general-purpose CI deduplication service. Land/test the provenance
contract before switching the trigger behavior. Current exact-commit/run checks
must not simply be deleted or have PR metadata rewritten to look like main.

The measured opportunity is **37.25 runner-minutes per comparable merge, minus
the replacement provenance/package/publication work**. That is nearly half of
the sampled PR/main pair before replacement overhead, not a measured future
50% saving. It also removes most of the sampled nine-minute post-merge wait.

Investigate the two #112 push events separately. The current ref concurrency
queues rather than cancels main runs; it does not prevent a second complete run
of the same SHA. First identify whether the release/merge procedure issued
redundant pushes or GitHub created duplicate events. The new gate can reuse
already-verified evidence, but do not build a bespoke deduper without need.

### 3. Add conservative change selection

Start with a narrow prose-only allowlist. Run relevant documentation/metadata
checks and always complete `repository`; skip application suites only when the
selection is explicit and validated. Unknown paths, renamed/deleted relevant
files, dependency pins, workflows, Aspect, BUILD files, packaging, test and
report logic must default to the full gate. Keep application changes on the
full suite initially rather than mapping every Go package to SSH partitions.

This follows, not precedes, the report work: the current report requires every
suite against the entire exact source snapshot. For prose-only changes, display
the last applicable application validation and its real revision as inherited
evidence; never label skipped tests as freshly passed. Validate that the
application/build inputs are unchanged. Release eligibility must remain explicit
and fall back to full validation if that narrower evidence cannot prove it.

### 4. Tune caching, job shape and evidence with measurements

- Allow native Bazel reuse for a reviewed set of deterministic tests in routine
  CI; retain an uncached `preflight` mode and fresh external/race-sensitive
  checks. Bazel's default test caching already tracks dependencies and reruns
  external, multi-run or previously failing tests. The collector must correctly
  retain cached-result provenance and logs; verify that behavior before changing
  `.aspect/check.axl` or coverage flags.
  [Bazel test caching](https://bazel.build/docs/user-manual#cache-test-results),
  [hermeticity](https://bazel.build/basics/hermeticity).
- Keep useful GitHub disk/dependency caching first. The live cache inventory was
  25 entries totaling 10,376,226,224 bytes (about 9.66 GiB); the portable dependency
  cache alone was about 1.64 GB compressed. Measure eviction/hit rates before
  increasing storage. Main cannot consume PR-ref caches; preserve main build
  cache writes, and check whether concurrent writers populate complete cache
  families. These observations do not prove cache thrashing.
  [Cache inventory API](https://api.github.com/repos/Bochner/burrow/actions/caches),
  [pinned setup action](https://github.com/aspect-build/setup-aspect/blob/2306377a61c45954ab2df7c7311698b109364352/action.yml).
- Investigate combining the six overlapping coverage tests with their normal
  validation, but preserve coverage scope, meaningful non-instrumented/race
  execution and all evidence expectations. Benchmark grouping short SSH suites
  into fewer runners only after the duplicate run is removed. Portable remains
  the observed critical path; extra partitioning may improve latency while
  increasing runner time.
- Keep the compact `evidence-*` artifacts required by reports on every run.
  Limit duplicate `test-results-*` uploads to failed jobs initially, with a
  separate explicit diagnostic mode if needed. The latest run uploaded 25
  artifacts totaling 44,691,389 bytes; portable bulk diagnostics accounted for
  32,983,563 bytes. This is an upload/storage optimization, not the primary
  runner-time saving.
  [Run artifacts](https://api.github.com/repos/Bochner/burrow/actions/runs/36419825472/artifacts).
- Consider Hovel-style remote caching only if remaining compilation/cache
  transfer measurements justify the additional service and trust configuration.
  Do not start by copying its remote execution, multi-platform reproducibility
  matrix, self-hosted infrastructure or merge-queue setup. Remote cache reuse
  depends on reproducible declared inputs and trusted writers.
  [Bazel remote caching](https://bazel.build/remote/caching).

Do not add a daily full scheduled run just to replace the duplicate main run.
Use manual verification and targeted race repetition first. Add scheduled
checks only for a demonstrated environment-drift need, with an explicit cadence
and runner budget.

### Acceptance and rollout

Use existing Aspect gates: workflow syntax, report behavior, release guards,
package installation, site validation, and the full required preflight after
nontrivial pipeline changes. Add focused behavior cases to existing report and
release tests for equal-tree/different-SHA promotion, mismatched inputs,
stale/missing/tampered artifacts, wrong run/repository, unknown paths and
unexpected skipped/failed/cancelled jobs. No duplicate test framework.

Rehearse the new main equivalence decision while retaining the old full gate
for one normal PR/merge. Prove that both identify the same test inputs, then
enable reuse. Exercise the full fallback and a nonpublishing release rehearsal.
Measure a code PR, a prose PR and their merges, including cancelled/retried jobs.
Success means one expensive validation per unchanged integrated source tree,
truthful Pages evidence, a working GitHub-only release path, no weakened branch
protection, and a measured reduction from the sampled 75.08-minute pair.

Implementation files are the existing `.github/workflows/{ci,pages,release}.yml`,
`.aspect/{check,report,release}.axl`, report/release Python helpers and their
declared targets, plus the release/development guidance. This research does not
authorize implementation, publishing, or changes to branch protection.

## What GitHub's documented behavior permits

### Validate before merge, publish after merge

A normal `pull_request` workflow checks out the synthetic merge result at
`refs/pull/<number>/merge`, unless the workflow overrides checkout. It already
tests the proposed integration with the base branch.
[GitHub: pull request event](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#pull_request).

Strict required checks require the branch to be current with its base. Loose
checks permit merging without that update and therefore permit incompatible
changes to meet only after merge. GitHub explicitly permits an up-to-date,
passing PR to be locally merged and pushed without rerunning checks on the final
merge commit. Running every suite again on every `main` push is therefore a
repository policy choice, not a universal GitHub requirement.
[Protected branches](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches#require-status-checks-before-merging),
[required-check troubleshooting](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks).

However, a passing PR run is not evidence that the final commit SHA was executed.
Squash creates one new commit; GitHub rebase-and-merge creates new SHAs. The PR
merge ref and the final merge commit must not be treated as interchangeable
identities. GitHub also documents indirect merges, where a PR becomes marked
merged without its own protection requirements being satisfied.
[GitHub: merge strategies](https://docs.github.com/en/pull-requests/reference/pull-request-merges).

**Proposed application:** make PR validation the expensive merge gate, retain a
small trusted `main` publication/build check, and retain strict protection with
no routine bypass. Before reusing validation, resolve the actual successful run
and compare its recorded tested source tree with the final tree. Record both
the tested commit and the published commit. If the evidence is missing,
expired, inconsistent, or from a different tree, run the full gate or stop
publication. A commit label or PR's `merged` flag alone is insufficient.

This is a design inference from the documented semantics, not a GitHub promise
that tests are redundant. Commit-derived versioning, generated metadata,
packaging, and deployed output still need checks against the final commit.
Environment-sensitive failures can justify periodic or manually requested full
checks. Existing exact-commit report contracts must be deliberately updated
before removing their producing `main` suites; preserving an old report while
changing its commit label would misstate the evidence.

### Merge queue is not the default solution here

GitHub's merge queue checks the proposed changes against the current target
branch and earlier queued changes. It is useful for busy branches, requires a
`merge_group` workflow trigger, and is available for public organization-owned
repositories or private organization-owned repositories on Enterprise Cloud.
A repository owned by a personal account does not meet those stated eligibility
rules, including Burrow's currently public personal repository. Do not recommend an account migration or enterprise plan
solely to optimize this small repository's CI.
[GitHub: managing a merge queue](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/configuring-pull-request-merges/managing-a-merge-queue).

### Select jobs without breaking the required gate

A workflow skipped by path filters leaves its required checks pending. A job
skipped through a condition reports success; a downstream job skipped after a
dependency fails can also fail to block the merge. GitHub recommends `always()`
with `needs` for required checks dependent on other jobs.
[GitHub: skipped required checks](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks#handling-skipped-but-required-checks).

**Proposed application:** keep one always-triggered workflow and the stable
required `repository` job. Classify changes inside the workflow; run only
applicable suites. The final job must evaluate results explicitly, reject
failure/cancellation/unexpected skips, and accept skips only for suites the
classifier declared unnecessary. Classification failure and unknown paths
should select the full gate. Initially allow only a narrow, reviewed category
such as prose-only changes; changes to workflows, Aspect, build declarations,
dependencies, package configuration, tests, or classification rules require the
full gate. Test the classification and aggregator with representative changes
before making them authoritative. This avoids a large path-to-suite framework.

### Cancel superseded validation, preserve publication work

Concurrency groups can cancel an older active run when a newer run enters the
same group. Including the workflow and PR identity keeps unrelated work apart.
Concurrency and cancellation are separate from reducing the work inside each
run.
[GitHub: workflow concurrency](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency).

**Proposed application:** cancel superseded runs of the same PR if not already
configured. Decide separately how Pages should handle newer commits. Do not
apply the PR cancellation rule to an in-progress package publication.

## Cache and artifact reuse have different purposes

PR runs can restore caches from the default/base branch, but a cache created by
a PR run belongs to its merge ref. `main` and other PRs cannot restore it. A new
workflow shape must not assume a PR cache will warm the following `main` run.
Cache keys and scope affect whether a restore actually saves work.
[GitHub: cache access restrictions](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching#restrictions-for-accessing-a-cache).

**Proposed application:** measure download, compilation, restore and save time
separately. Keep useful trusted default-branch cache population when it is
already a by-product of building publication artifacts. Do not create a large
cache-warming service before measurements justify one. Build/download reuse
does not require skipping deliberate uncached integration-test execution.

Artifacts carry outputs between jobs and workflows. Cross-run downloads need
the run identity and token; artifact retention is configurable. GitHub's
artifact download action checks the uploaded digest, but its documented
mismatch behavior is a warning, not a guaranteed fatal error.
[GitHub: storing and sharing artifacts](https://docs.github.com/en/actions/tutorials/store-and-share-data).

**Proposed application:** build the intended release package once, smoke-test
that package, and publish those exact files. Identify evidence by repository,
workflow, run/attempt, source revision and artifact digest. If digest equality
is a release requirement, explicitly fail on mismatch. Set retention long enough
for the actual release cadence; missing artifacts require reconstruction and
appropriate validation, not an unchecked substitute. Do not publish the newest
artifact merely because its name matches. A PR build with different embedded
version or commit metadata is not automatically the intended release package.

## Preserve the publication trust boundary

`workflow_run` can gain secrets and write permissions unavailable to its
triggering workflow. It fires for completed runs regardless of success, and its
own default SHA is the default branch's current commit. Therefore publication
must inspect the triggering run's conclusion and source identity explicitly.
[GitHub: workflow_run](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_run).

GitHub warns against executing untrusted checked-out code or treating upstream
artifacts as trusted in privileged workflows. It recommends minimal token
permissions and full-commit action pins.
[GitHub: secure workflow use](https://docs.github.com/en/actions/reference/security/secure-use).

**Proposed application:** retain same-repository, correct-event, correct-branch,
successful-run checks for publication. Download from the exact approved run and
use its recorded source SHA instead of the publishing workflow's incidental
default SHA. Keep parsing/building in jobs without publishing credentials; give
write/OIDC permissions only to the publication job. If PR evidence is reused,
parse a constrained report as data and validate provenance; do not run scripts
from that artifact. Preserve owner-controlled release authorization independently
of whether CI has already passed.

## How to measure an improvement

Compare summed job runtime and critical-path duration separately. More parallel
jobs can reduce waiting while increasing runner minutes through repeated setup,
compilation and cache transfers. Count retry attempts and cancelled runs as work;
do not compare only successful final attempts. Report runner-time estimates as
estimates unless billing data is available.

For a representative code PR and a prose-only PR, record the suites selected,
job minutes, total elapsed time, cache transfers, test execution time, retry
count and artifacts produced. Compare the same categories after each change.
Removing one full validation run per merge has a clear avoided-work count;
claiming a fixed percentage saving before measuring the replacement build,
publication checks and cache behavior would be unsupported.

## Research validation

`aspect build //:research` passed on 2026-09-28 after adding this note. It proves
the documentation metadata target accepts the file; it does not validate the
proposed pipeline or the accuracy of prose. Findings were checked against the
linked source, live read-only GitHub responses and the downloaded PR evidence.
That initial research check changed no workflows or settings. The subsequent
implementation is recorded above; hosted PR/merge validation is still pending.


## Implementation validation

Local validation on `research/ci-efficiency` on 2026-09-28:

- `aspect burrow-check preflight`: all 53 targets passed, with 57 actual test
  attempts and zero cached attempts. This includes nine SSH partitions, three
  Hovel compatibility checks, and three repetitions each of setup and terminal.
- Focused CI selector, workflow, report, release, formatting and lint checks
  passed after implementation. Selector cases cover same-tree/different-commit
  reuse, changed merge parents, documentation baselines, run identity, missing
  required gates, expired/corrupt archives and unsafe archive paths.
- `aspect burrow-ci docs` passed with two real cached test results; collection
  retained both their original log/XML outputs and their cached status.
- `aspect burrow-report coverage`: all six production coverage targets passed.
  `aspect burrow-report release` assembled complete measured evidence and parity.
- `aspect burrow-release bundle` built and smoke-tested the exact final Linux
  archive and wheel, including offline pipx installation and retained Hovel state.
  Local dirty-source artifacts are inspection outputs, not publication candidates.
- `aspect burrow-site check` passed. A temporary Aspect-declared browser runner
  exercised all six report views, keyboard navigation, filtering, evidence tabs
  and desktop/mobile layouts in locally installed Chromium, with no browser
  exceptions or page overflow. The runner was removed after inspection; the
  checked-in report UI structure check remains part of the documentation gate.

The README now follows the pinned Hovel README's product, documentation, install,
development, layout and contribution structure. Stale milestone history and
implementation/CI detail were removed from its main reading path; useful local
Makefile guidance moved into the Development Guide.

No push, PR, merge, hosted run, deployment, publication or repository-setting
change was performed. The next acceptance step is the owner's requested PR/merge
exercise, followed by a nonpublishing Release rehearsal and runner-time comparison.
