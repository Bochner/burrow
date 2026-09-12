# Agent instructions for Burrow

These instructions apply to all agents in this repository. `CLAUDE.md` is a
symlink to this file, following Hovel; edit this canonical file.

Read `CONTEXT.md`, `docs/wayfinder-start.md`, and the
[Wayfinder map](https://github.com/Bochner/burrow/issues/1) before planning. Research in
`docs/research/` is evidence and recommendations, not an approved implementation
specification. `core/prototype_*` contains bounded proofs, not a production SSH application.

## Development conventions

- Follow the Hovel conventions recorded in `docs/research/hovel-integration.md`.
  All build, test, format, lint, and package workflows must enter through Aspect
  CLI. Do not invoke Bazel, gofmt, or a second task runner directly. Put new
  workflows in `.aspect/*.axl` and cacheable work in declared Bazel targets.
- Pin upstream versions and record provenance. Prefer the public Hovel SDK and
  protocol; do not import `core/internal` or duplicate daemon-owned state.
- Keep Go application behavior independent of terminal rendering. Hovel module
  stdout belongs exclusively to framed JSON-RPC; progress uses SDK logging.
- Reuse existing code, the Go standard library, and Hovel capabilities before
  adding abstractions or dependencies. No empty SMB/WinRM runtime adapters;
  their placeholders are research topics until those capabilities are scoped.
- Trace LazySSH source and tests before calling a workflow feature-complete.
  Preserve useful functionality, document intentional differences, and do not
  preserve accidental bugs as compatibility requirements.
- Validate remote inputs, verify SSH host keys, keep credentials out of logs
  and source control, and preserve Hovel's confirmation and audit contracts.
- For nontrivial implementation changes, leave a focused runnable behavior
  check and run the relevant Aspect gate. The current `//:research` target
  only verifies the metadata build graph, not application behavior or docs prose.

## Repository layout and documentation

Follow the layout in `docs/site/src/content/spec/development-guide.html` when
adding or moving files. Application code belongs under `core/` when it exists;
documentation source belongs in `docs/site/`, tooling in `docs/tools/docs/`, and
research in `docs/research/`. Create module/SDK areas only for real capabilities.

When editing the Pages book, components, assets, or deployment workflow, read
`docs/agents/docs.md`. Preserve upstream attribution in `docs/site/UPSTREAM.md`.

## Common tasks and completion

- `aspect help`: discover the checked-in workflows.
- `aspect burrow-check`: full metadata, documentation, SDK, daemon reuse and SSH proof gate.
- `aspect burrow-check ci`: builds all proofs and runs portable checks; excludes the host-pinned SSH fixture.
- `aspect burrow-site check`: generated book, links/assets, and search checks.
- `aspect burrow-site stage`: materialize declared site output under `_site/`.

Run the relevant gate before completion and state what it proves. Add behavior
checks and expand the gate when application code arrives. Prefer declared,
pinned toolchains; use Python for nontrivial repository tooling as Hovel does.

Work in the user's current checkout. When creating a branch, switch that
checkout to it and stay on it until the owner explicitly requests a PR and merge.
The active implementation branch is `mvp1`, starting with issue #44. Keep all
MVP work and commits local on this shared branch until the entire MVP is done.
Do not push, create a PR, or merge until the owner explicitly requests it;
completion of a ticket or milestone does not authorize any of these actions.
Use this shared branch across MVP tickets and milestones. Research follows the same branch policy,
overriding skill suggestions for throwaway research branches. Use additional
worktrees only when the owner requests them.
After an authorized merge, return the checkout to clean, synchronized `main`
and remove merged branches and temporary worktrees after preserving all work.
Do not discard unrelated changes. Keep prototype labels and open decisions explicit.

## Agent skills

### Issue tracker

Use the private `Bochner/burrow` GitHub repository. See
`docs/agents/issue-tracker.md`.

### Triage labels

When triaging issues, use the default role-to-label mapping in
`docs/agents/triage-labels.md`.

### Domain docs

Single-context layout: root `CONTEXT.md`, with decisions added to `docs/adr/`
when resolved. See `docs/agents/domain.md`.

## Local environment

When `/home/bochner/.codex/RTK.md` exists, read it and follow its shell-wrapper
instructions. Its absence on another machine is not a project prerequisite.
