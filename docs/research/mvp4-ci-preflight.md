# MVP 4 CI and deployment preflight research

Research dates: 2026-09-19–20. This is evidence and recommendations, not a new
implementation specification or a claim that the MVP 4 GitHub run has passed.
The owner authorized preparing one PR after validation and explicitly reserved
the merge decision. Research inspected repository settings read-only; the
owner subsequently authorized enabling required checks.

Hovel comparison baseline:
[`a4cbfdf7769a9551695088c11061e3cabc368e07`](https://github.com/vibepwners/hovel/commit/a4cbfdf7769a9551695088c11061e3cabc368e07),
the 2026-09-15 merge of
[PR #58](https://github.com/vibepwners/hovel/pull/58).
GitHub, Bazel and Aspect documentation was checked through Context7 and the
official sites. Burrow observations refer to the current `mvp4` working tree
unless an immutable run or PR is linked.

## What actually failed previously

These failures were different problems. A passing local invocation did not
establish reliable behavior on a smaller, independently scheduled runner.

| Evidence | Observed failure | Documented cause and correction |
| --- | --- | --- |
| [Run 34737908441](https://github.com/Bochner/burrow/actions/runs/34737908441), `main`, 2026-09-13 | `connection_lab`: secret prompt appeared while terminal echo was enabled. | [PR #83](https://github.com/Bochner/burrow/pull/83) traced this assertion to the previous prompt's exit rendering. The lab now waits for the next secret prompt with echo disabled, preserving the secrecy assertion. |
| [Run 34780229988](https://github.com/Bochner/burrow/actions/runs/34780229988), `main`, 2026-09-13; [run 34803712433](https://github.com/Bochner/burrow/actions/runs/34803712433), `main`, 2026-09-14 | `setup_test`: expected `Workspace name`, but the Metadata overlay remained open. | [PR #84](https://github.com/Bochner/burrow/pull/84) found that the initial readiness text also existed in the sidebar. Escape could arrive before the overlay opened; fixed sleeps could also combine Escape with the next Alt sequence. The test now waits for overlay-specific readiness and observable dismissal. |
| [Run 34798987368](https://github.com/Bochner/burrow/actions/runs/34798987368), `mvp3` PR, and [run 34801057932](https://github.com/Bochner/burrow/actions/runs/34801057932), `main`, 2026-09-14 | `connection_lab`: interactive command did not finish after SIGTERM at a password prompt. | [PR #83](https://github.com/Bochner/burrow/pull/83) reproduced a real product deadlock under load. Bubble Tea's signal handler could block sending after the event loop stopped. The CLI form now uses its existing cancellation context without the competing signal handler; a repeated PTY SIGTERM regression check was added. |
| [Run 34799128791](https://github.com/Bochner/burrow/actions/runs/34799128791), `main`, 2026-09-14 | Shell switching/input during 4,000 lines of background output took 2.20 seconds against a 2-second bound. | [PR #82](https://github.com/Bochner/burrow/pull/82) retained the functional assertions and changed the bound to 3 seconds. Its PR description reports runner variance; this does not justify removing responsiveness limits elsewhere. |
| [Run 34608792671](https://github.com/Bochner/burrow/actions/runs/34608792671), Pages, 2026-09-11 | `configure-pages` could not find an enabled Pages site. | This was a repository configuration prerequisite. The current [Pages API](https://api.github.com/repos/Bochner/burrow/pages) reports `build_type: workflow`; the [environment policies](https://api.github.com/repos/Bochner/burrow/environments/github-pages/deployment-branch-policies) permit only branch `main`. |

The downloaded failed-step logs corroborate the assertions above. The linked
PRs supply the diagnoses and historical validation results; those results are
not new validation of this branch. The appropriate carry-forward rule is to
wait for the actual state transition with a deadline, preserve security and
functional assertions, and reproduce timing failures under constrained CPU
before deciding whether the code or the assertion is wrong.

## Hovel patterns that apply

Hovel's [CI workflow](https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/.github/workflows/ci.yml)
runs the checked-in Aspect gates on both PRs and `main`, uses Ubuntu 24.04,
pins actions to full SHAs, disables checkout credential persistence, and sets
job timeouts. Its matrix uses `fail-fast: false`, preserving evidence from
independent scopes. It uploads failed-test logs/XML and release/site artifacts.
Burrow adopts the same gate parity and diagnostics with partitions matched to
its own SSH acceptance paths; Hovel's Wine job is not applicable.

Hovel's [setup action](https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/.github/actions/setup-hovel/action.yml)
separates repository-download caching from BuildBuddy action caching. Its
repository cache key includes OS, architecture, scope/family, module files,
lockfiles, Bazel/Aspect pins and repository definitions. [PR #58](https://github.com/vibepwners/hovel/pull/58)
explains why: different scopes had shared an immutable cache snapshot, leaving
heavier scopes repeatedly fetching missing dependencies. This was a cache
population/performance problem, not evidence that test retries were safe.

Burrow now runs a portable/support job and nine independent SSH partitions.
The SSH partitions share the same declared dependency population. The pinned
setup-aspect action's built-in `disk-cache` and `repository-cache` keys separate
`burrow-portable` from `burrow-ssh`; all SSH partitions reuse the same family.
Cached builds/downloads never skip uncached test execution. GitHub cache entries are
immutable; an exact hit is not updated in place. Cached downloads or build
outputs do not establish that a timing-sensitive test ran during this
invocation. [GitHub caching reference](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching).

Hovel explicitly pins the Aspect launcher to `2026.33.3`. The pinned
[setup-aspect action contract](https://github.com/aspect-build/setup-aspect/blob/2306377a61c45954ab2df7c7311698b109364352/action.yml)
distinguishes launcher selection from `.aspect/version.axl`, which selects the
CLI: omitting `launcher-version` installs the latest launcher. It also documents
that Bazelisk defaults to latest when no Bazel executable is present. Burrow's
`.bazelversion` still selects Bazel itself; neither the launcher pin nor the
runner label freezes every tool on the hosted machine.

Hovel's [Aspect configuration](https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/.aspect/config.axl)
disables Aspect's GitHub status, comment and artifact integrations in Actions,
because the workflow already owns those surfaces. This avoids unnecessary
token requests. Burrow now disables those same redundant integrations in GitHub Actions. Historical Burrow logs contain Aspect authentication warnings
explicitly saying the task can still complete. They must not be mistaken for
the subsequent assertion failure. An Aspect API token is relevant only if
those additional integrations are intentionally used.
[Aspect authentication documentation](https://aspect.build/docs/cli/authentication).

Hovel's [Pages workflow](https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/.github/workflows/pages.yml)
downloads `docs-site` from the exact successful same-repository `main` push run
and grants Pages/OIDC write access only to the deploy job. Its
[release workflow](https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/.github/workflows/release.yml)
validates source/version eligibility, runs the gate, smoke-tests built packages,
and separates build artifacts from publishing jobs. Burrow has no release
publishing workflow; this audit does not propose one just to match Hovel.

## Current Hovel source and runtime pins

At the owner's request, the local upstream checkout was fast-forwarded to
`a4cbfdf7769a9551695088c11061e3cabc368e07`, and Burrow's public SDK archive pin was
updated to that commit (SHA-256
`9080e5060787f4ba5ee6715611e10112af2af316318369b748c9f566c1f3b6e9`). The
[comparison from v0.4.2](https://github.com/vibepwners/hovel/compare/c461ba282a8aecc7aa3a079a4613bf5e2640c388...a4cbfdf7769a9551695088c11061e3cabc368e07)
contains eight commits and 44 changed files, all build, CI, documentation or
test support. The Go SDK and production daemon/runtime sources are unchanged.
The latest published runtime remains
[v0.4.2](https://github.com/vibepwners/hovel/releases/tag/v0.4.2), whose tag resolves
to `c461ba282a8aecc7aa3a079a4613bf5e2640c388`; Burrow retains that verified wheel
and its executable digest. The new SDK pin is not a claim of a new runtime
release or a fix for the daemon disappearance observed below.

## Applicable best practices and branch changes

The following implementation observations describe source inspected during
this audit, not successful execution on GitHub. See
[the gate](../../.aspect/check.axl), [Repository workflow](../../.github/workflows/ci.yml),
[Pages workflow](../../.github/workflows/pages.yml), and
[declared workflow lint target](../../BUILD.bazel).

| Priority | Practice | MVP 4 status / remaining action |
| --- | --- | --- |
| 1 | Run the identical full gate locally and on PR/`main`. | Repository now invokes `aspect burrow-check preflight`, discovering declared tests instead of maintaining another test list, including Docker acceptance. Portable `ci` mode remains explicitly narrower. |
| 1 | Require actual repeated success. | Preflight sets `--nocache_test_results`, repeats the setup/terminal race targets three times with a `--runs_per_test` regex, and sets `--flaky_test_attempts=1`. Keep all selected repetitions mandatory; do not use successful retries to turn a failed gate green. |
| 1 | Enforce the gate before merging. | Initial [branch protection](https://api.github.com/repos/Bochner/burrow/branches/main/protection) was absent and [rulesets](https://api.github.com/repos/Bochner/burrow/rulesets) were empty. With subsequent explicit owner authorization, the audit enabled required `repository` status from GitHub Actions (app ID `15368`), strict up-to-date checking, administrator enforcement and PRs (zero required review approvals). Merge remains owner-controlled. |
| 1 | Publish the checked artifact with least privilege. | Pages now downloads the originating successful CI run's artifact, requires a same-repository `main` push, restricts manual deployment to an existing successful run of the exact `main` commit, separates build/deploy permissions, and checks the source SHA against current `main` before deployment. |
| 2 | Control local test contention. | Preflight limits local test execution to two jobs; CI runs independent acceptance partitions on separate runners. This bounds competing tests; it does not emulate CPU speed or cap every subprocess. |
| 2 | Preserve failure evidence. | CI uploads test logs, XML and undeclared test outputs with `always()` and 14-day retention; the site artifact also has 14-day retention. Check the first hosted artifact to confirm all repeated-run logs are present. |
| 2 | Pin and check pipeline code. | Actions remain SHA-pinned; launcher version and Ubuntu major release are explicit; checkouts do not retain credentials. A digest-pinned actionlint target checks workflow syntax/expressions through Aspect. |
| 3 | Keep performance machinery proportional. | Native GitHub matrix jobs provide parallelism; no new remote execution service, release workflow or automatic rerun service is needed. Adopt Hovel's larger cache/timing machinery only after measured need. |

Bazel documents different meanings for repeated runs and retries. With the
default `--runs_per_test_detects_flakes=false`, any failed repeated run fails
the target. Retrying a failed test until it passes can instead yield a successful
exit status with a `FLAKY` label. Explicitly retaining the former setting also
guards against a developer's local rc override. `--test_output=errors` emits
failed-test output; uploaded logs retain the fuller evidence.
[Bazel 9.1 testing options](https://bazel.build/versions/9.1.0/docs/user-manual#running-tests).

GitHub documents 2 CPUs/8 GB for standard private Linux runners and 4 CPUs/16 GB
for standard public Linux runners. Burrow is private, so Hovel's public runner
capacity is not the right assumption. An Ubuntu version label still receives
image updates. Use the hosted run's runner image details when comparing a
failure with local Docker/OpenSSH behavior.
[Runner specifications](https://docs.github.com/en/actions/reference/runners/github-hosted-runners),
[run logs and runner image details](https://docs.github.com/en/actions/how-tos/monitor-workflows/use-workflow-run-logs).

Required checks should have a stable, unique job name and run for every PR;
workflow path filters can leave required checks pending. Strict up-to-date
checks ensure the base branch used for acceptance is current. Add a
`merge_group` trigger only if a merge queue is actually adopted.
[Protected branches](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches),
[required-check troubleshooting](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks).

`workflow_run` fires even when the source run failed and can receive privileged
credentials. The success/event/repository guards and exact run ID are therefore
part of the deployment boundary. Full action SHAs and narrowly scoped job
permissions reduce mutable dependencies and unnecessary privileges.
[Workflow-run semantics](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_run),
[GitHub secure-use guidance](https://docs.github.com/en/actions/reference/security/secure-use).
Serial deployment alone does not guarantee revision order. Burrow's additional
current-`main` check prevents an already superseded run from deploying, though
`main` can still advance immediately after that check; it is not an atomic
branch lock. [Concurrency semantics](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#concurrency).

## Additional failures reproduced during preflight

A fresh-output-base rehearsal limited to two CPUs reproduced a workspace-switch
race in `terminal_test`: one of three runs clicked the Hovel tab while the
workspace dialog was still closing. The entered workspace path was itself
visible in the dialog, so matching that path did not establish readiness.
Submission removes the input fields before the launch completes, while the
`NEW WORKSPACE` title and destination remain. The wait now requires that whole
dialog to be absent, matching the existing setup check. Waiting only for the
input label to disappear passed five focused repeats but failed again in the
full suite; those focused passes did not establish a fix.

A subsequent partition rehearsal reproduced the same class of readiness error
in the guided CLI connection form: every field label was already visible, so
matching `SSH port` did not mean that field had focus. The test typed part of
the port into the host. The shared authentication terminal helper now matches
the focused input line while the details form is displayed. The existing VT
decoder now exposes the inverse software cursor used by standalone Huh forms;
its hardware-cursor mode remains for the fullscreen TUI. A replay first proved
that the hidden terminal cursor remains at the footer in standalone forms. It also observes the affirmative confirmation selection
before Enter. Guided entry repeats three times inside the lifecycle check,
without repeating its entire fixture or relaxing authentication/leakage checks.

The constrained full SSH run also reached its old 840-second overall alarm
while executing the expanded retained-command suite. Its daemons were still
alive and cleanup completed normally. Raising the whole-suite budget alone did
not address runtime, so routine acceptance is now partitioned into lifecycle,
files, reverse, shell, chains, reports, automation, follow and runs. Shell also
covers local forwarding. Lifecycle retains SOCKS, manager/profile controls,
authentication, TUI and failure ownership; it seeds a real reviewed download so
nonempty retained evidence still crosses reconnect, quit and owner loss. The runs partition includes scripts. Every partition retains the
post-shutdown database and confirmation assertions. The composed monolith
remains an explicitly selected diagnostic target, excluded from routine gates.

CI runs these nine partitions and portable/support checks in parallel, with a
15-minute job cap and no failed-test retries. Only setup and terminal race
checks repeat three times; the SIGTERM regression already exercises 40 prompt
cycles per invocation. The required `repository` aggregate uses `always()` and
fails unless the entire matrix succeeds, including when upstream jobs fail,
skip or cancel. Pages always promotes a verified artifact rather than rebuilding
and repeating acceptance. Individual operation and responsiveness bounds remain
unchanged. Interrupted runs are not recorded as successful checks.

The required aggregate follows GitHub's guidance for dependent checks: it runs
with `always()` and accepts only an upstream `success`, so a failed or skipped
matrix cannot silently satisfy branch protection. Matrix `fail-fast: false`
retains independent failure evidence. [Required checks](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks),
[dependency results](https://docs.github.com/en/actions/reference/workflows-and-actions/contexts#needs-context).
Manual Pages deployment selects a successful main push by the exact commit
using the documented [GitHub CLI run filters](https://cli.github.com/manual/gh_run_list)
and downloads that run's artifact. An absent/expired artifact fails deployment;
it never falls back to publishing unverified output.

## Validation limits and completion evidence

The audit includes source review, historical logs, repository configuration,
and uncached local rehearsals. Main protection is enabled as authorized. An
initial uncached composed acceptance run lost its daemon after approximately
633 seconds during proxy UI acceptance; the cause remains unproven. Partitioning
alone does not establish that this disappearance is fixed. Daemon logs and phase
traces are now preserved before fixture teardown to make a recurrence diagnosable.

The first six-partition rehearsal passed every target uncached on two CPUs,
with two tests sharing those CPUs: reports 53s, automation 133s, follow 75s,
runs 337s, chains 243s and lifecycle 751s. Total elapsed time was 13m38s.
Lifecycle dominated, so it was split further into files, reverse, shell and the
residual lifecycle scenario. The extracted files, reverse and shell checks passed in 153s, 64s and 104s
respectively. The residual lifecycle exposed the guided-field race above; its corrected
rerun passed in 188s, including three guided-entry repetitions. All nine SSH
partitions have passed uncached. The first five shared two CPUs with another
lab, while the final lifecycle rerun used the same two CPUs alone. These are
local observations with cached build inputs, not promised hosted durations.
Routine partition alarms are ten minutes, leaving build/cleanup/diagnostic
headroom inside the 15-minute job cap. The final portable preflight passed all
26 targets uncached in 3m45s on two CPUs, including three successful executions
each of setup and terminal checks, both Docker manager proofs, workflow lint,
and documentation checks. The declared site output also staged successfully.
Hosted validation is recorded in the milestone PR after local checks; this
report does not claim that an unobserved GitHub run passed. Merge remains an
explicit owner decision.
Record the exact tested commit and the hosted PR run before declaring the
branch ready for the owner's merge decision. Local repeated success lowers
flake risk; it cannot prove the future hosted image, fresh dependency downloads,
Docker service or deployment service will behave identically. A successful PR
check proves verification on that PR revision; Pages deployment remains a
separate post-merge operation with its own result. No finite number of repeats
proves that an intermittent failure is impossible.
