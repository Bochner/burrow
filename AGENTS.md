# Burrow

Read `CONTEXT.md` and `docs/wayfinder-start.md` before planning. Research in
`docs/research/` is evidence and recommendations, not an approved implementation
specification. This repository currently contains no SSH implementation.

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

## Agent skills

### Issue tracker

Use the private `Bochner/burrow` GitHub repository. See
`docs/agents/issue-tracker.md`. Skill installation is left to the owner.

### Domain docs

Single-context layout: root `CONTEXT.md`, with decisions added to `docs/adr/`
when resolved. See `docs/agents/domain.md`.

## Local environment

When `/home/bochner/.codex/RTK.md` exists, read it and follow its shell-wrapper
instructions. Its absence on another machine is not a project prerequisite.
