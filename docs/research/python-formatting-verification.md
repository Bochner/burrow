# Scoped Python formatting (#103)

Baseline: `22299775c2abb94dd48cf8b506c7f6568573d930`, 2026-09-26.

Ruff 0.16.3 matches Hovel's
[locked formatter](https://github.com/vibepwners/hovel/blob/541e78ada0af48165aef932732145c418aa2d290/sdk/python/uv.lock).
The Linux amd64 archive URL and release-asset SHA256 are declared in
`MODULE.bazel`. Python 3.12 and 120 columns follow Hovel's
[configuration](https://github.com/vibepwners/hovel/blob/541e78ada0af48165aef932732145c418aa2d290/sdk/python/pyproject.toml).
Only formatting is adopted, not its SDK lint rules. Docstring code formatting
stays disabled to preserve fixture content. Current
[Ruff documentation](https://docs.astral.sh/ruff/formatter/) was checked through
Context7; the declared binary was exercised through Aspect.

Both workflows use the same Bazel source groups and configuration. The scope
is `*.py` in `core/cmd/burrow`, `docs/tools/docs`, `agent/tools`, `.aspect`,
and Python files under `.github`. Prototypes, external sources and generated
build outputs are excluded. Untagged formatter tests are discovered by the
existing portable CI/preflight gates. No partition or #86 exception changed.

## Verification

- Before formatting, `aspect burrow-check preflight` passed all 41 targets,
  including the nine production SSH suites, tooling and documentation checks.
- The new behavior check first failed against an empty implementation, then
  passed with the wrapper: nonmutating rejection, explicit workspace repair
  distinct from runfiles, executable shebang/mode preservation, successful
  recheck and syntax-error propagation.
- The initial scoped check rejected 38 existing files; explicit write mode
  formatted exactly those 38. The two new wrapper/test files were already clean.
- A one-off declared Aspect review compared all 38 changed files with the
  baseline: identical Python ASTs including type comments, assertions, string
  literals, diagnostics and subprocess arguments; identical shebangs and modes.
  The temporary review target was removed.
- A newly added `core/cmd/burrow/format_probe.py` was discovered without a
  workflow/list change. Aspect check failed without changing its content;
  Aspect write repaired it and the subsequent check passed. Probe removed.
- After formatting, `aspect burrow-check preflight portable` passed all 33
  targets, including three setup/terminal repetitions and docs/agent checks.
  `aspect burrow-site check` passed both targets. Standards and Spec reviews
  each reported zero findings. The final full preflight will also cover the
  separately requested pipx release preparation before session completion.

## Actual changed-file inventory

38 existing files, not the prior 53-file all-repository observation:

```text
.aspect/go_format.py
.aspect/go_format_test.py
.github/check_workflows.py
agent/tools/bundle.py
agent/tools/discovery.py
core/cmd/burrow/activity_check.py
core/cmd/burrow/agent_check.py
core/cmd/burrow/agent_faults.py
core/cmd/burrow/agent_lab.py
core/cmd/burrow/authentication_lab.py
core/cmd/burrow/automation_lab.py
core/cmd/burrow/capabilities_check.py
core/cmd/burrow/chains_lab.py
core/cmd/burrow/connection_check.py
core/cmd/burrow/connection_lab.py
core/cmd/burrow/file_history_lab.py
core/cmd/burrow/file_latency.py
core/cmd/burrow/files_check.py
core/cmd/burrow/files_lab.py
core/cmd/burrow/follow_lab.py
core/cmd/burrow/forward_lab.py
core/cmd/burrow/latency_lab.py
core/cmd/burrow/manager_compatibility_check.py
core/cmd/burrow/manager_lab.py
core/cmd/burrow/profile_check.py
core/cmd/burrow/reports_lab.py
core/cmd/burrow/runs_lab.py
core/cmd/burrow/sessions_lab.py
core/cmd/burrow/setup_check.py
core/cmd/burrow/shell_lab.py
core/cmd/burrow/terminal_check.py
core/cmd/burrow/workspace_check.py
core/cmd/burrow/workspace_lab.py
docs/tools/docs/report.py
docs/tools/docs/report_test.py
docs/tools/docs/site_test.py
docs/tools/docs/stage_site.py
docs/tools/docs/stage_site_test.py
```
