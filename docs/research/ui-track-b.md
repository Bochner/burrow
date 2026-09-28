# General-use Track B: UI state cleanup

Issue [#98](https://github.com/Bochner/burrow/issues/98), rows 16–18.
Baseline: `3823c017f6d5ac9fb7f081a3e48dc803c5e48fd6`, on
`audit/general-use-architecture`. This is a state-ownership cleanup; no latency
or allocation benchmark claim is made.

## Row decisions

**16 — implemented.** Management and file tabs share one workspace inventory
observation: daemon information, connections, tunnels and their observation
status. The four-field propagation loop and initial field copies are gone.
Daemon observations now reach file tabs immediately, including updates that do
not pass through management command handling. Different workspaces allocate
different observations. Each file tab still owns its draft, history, output,
directory/cache, cancellation and exact connection creation/generation.

**17 — implemented.** Visible and saved follow tabs store `runView` directly,
including its independent stream cursors, bounded previews, pause/viewport,
generation/cancellation and two help fields. They no longer allocate or retain
`newUI`'s prompt, completion, inventory, profiles, downloads, history and command
state. A temporary projection reuses the existing renderer and help layout;
dimensions and color mode come from the workspace. Workspace/view identity and
generation checks still fence results, and only the selected view schedules
further reads. No generic tab interface or duplicate rendering system was added.

**18 — no change.** Comparing the 46 exact command-key overlaps between
`completionDescriptions` and catalog patterns found only one identical summary:
`profile save` → `Save authenticated settings`. That entry also describes the
incomplete `profile save ` candidate. A complete-candidate-only fallback would
still need that partial hint, plus new matching rules, so it removes no meaningful
duplication. Keep the current hints until equivalent complete-only descriptions
provide an actual deletion. In particular, catalog tunnel-check wording omits
the current connectivity hint, shell summaries differ from tab-specific hints,
and options/aliases/connection guidance cannot be replaced wholesale.

## Verification

Two focused checks extend the existing interaction target:

- `TestFileTabsShareWorkspaceObservation`: two file tabs, independent drafts,
  fresh daemon/tunnel observations, failed observation and recovery, same-name
  generation replacement, workspace isolation and unmatched file results.
- `TestFollowViewRetainsOnlyItsOwnReadingContext`: paused in-flight output,
  independent streams, resume, isolated scrolling help, text selection, return
  from logs, background/workspace routing, reopen and stale-generation rejection
  after close or explicit offset reset.

The existing live-output presentation check continues to cover empty and
populated views at 160×40, 200×50, 120×30 and 80×24 with semantic colors and
NO_COLOR. Existing file-tab and completion checks cover the unchanged hints,
metadata, return context and rendering.

The owner requested only necessary tests. The combined affected gate is:

```sh
aspect test //:go_format_test \
  //core/cmd/burrow:interaction_test \
  //core/cmd/burrow:terminal_test \
  //core/cmd/burrow:ssh_files_test \
  //core/cmd/burrow:ssh_follow_test \
  --bazel-flag=--local_test_jobs=2 --bazel-flag=--flaky_test_attempts=1
```

All five targets passed in 1m 54s, executing once without failed-test retries:
format 0.3s, interaction 55.7s, terminal 36.8s, SSH files 70.4s and SSH follow
56.5s. This compiles/analyzes production and test code, checks formatting,
and exercises the frame/VT presentation suite and real terminal/file/follow
workflows. The SSH labs confirmed cleanup and database integrity afterward.
Full release preflight, unrelated SSH partitions, race/fuzz checks and advisory
#86 diagnostics are intentionally outside this invocation. The three-diagnostic
#86 release exception is unchanged.

Development checks caught a missing inventory reference in the temporary
follow renderer (repaired), plus two test-fixture mistakes: reusing a connection
slice already marked unverified, and expecting scrolling help at a size where
it fits. The corrected focused checks pass.

Independent code-review results: **Standards: 0 findings; Spec: 0 findings**.
The reviewers traced workspace and generation fences, shared observation writes,
help/paste/cursor routing, selection and return behavior. Existing untracked
audit notes remain outside this change. Track C is the remaining scoped package;
the deferred #65 owner walkthrough remains separate.
