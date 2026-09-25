# Operator book audit — #92

Source baseline: `a16c0698dff817b0ab203c1063a1ee2e7df45ed8` on `mvp5`.
Reviewed 2026-09-23 against production source, command help, the generated
capability contract, ADRs 0001/0002 and #92. This record is not a public chapter
or historical research/decision rewrite. Final release reconciliation and the
owner's daily-use walkthrough remain #65.

## Chapter coverage

Paths below are relative to `docs/site/src/content/`; production checks are
declared under `core/cmd/burrow/BUILD.bazel` unless qualified otherwise.

| Chapter | Source or accepted contract | Reconciliation / behavior check |
| --- | --- | --- |
| `index.html` | Production package; release API; capability inventory | Remove planning label, state source distribution, reuse the existing single-hero layout. |
| `spec/index.html` | Entire operator command surface | Task-oriented entry paths; distinct API/CI/operational reports. |
| `spec/user-guide.html` | `main.go`, `capabilities.go`, release tag | Released/current/planned table, first connection and command-surface examples. |
| `spec/launch.html` | `core/launch/package.go`, `files_linux.go`, package BUILD, `main.go` | Exact archive/install path, prerequisites, pins, offline setup and upgrades; `setup_test`, `//core/launch:online_test`. |
| `spec/workspace-navigation.html` | `workspace.go`, `frame.go`, `core/connection/manager.go` | Distinguish open from inspect and manager retirement from daemon restart; `workspace_test`, lifecycle partition, terminal checks. |
| `spec/connections.html` | `main.go`, `auth.go`, `core/connection/{commands,ssh_config,prompt_linux}.go`, ADR 0001 | Correct interactive CLI detection and command-only recap; retain explicit host-trust and secret-entry differences; lifecycle partition. |
| `spec/profiles.html` | `core/connection/profiles.go`, `profile_ui.go` | Load-only settings, selected collection, schema and conflict behavior preserved; `profile_test`, lifecycle partition. |
| `spec/lifecycle.html` (new) | `quit.go`, connection close/manager controls, ADR 0002 | Action/lifetime table; current local shells versus planned shared shells; lifecycle, shell and runs partitions. |
| `spec/safety.html` | ADR 0001, #86, launch/run ownership | Host-trust consequence, current official-runtime limitation and explicit recovery. |
| `spec/hovel-integration.html` | Public module manifest, `module.go`, retained manager/run sessions | Ownership table, single public module, frontend-local shell limit and optional MCP. |
| `spec/shells.html` | `terminal.go`, `core/terminal/host_linux.go` | Frontend-local scope prominent, history/resize separated from developer detail; shell partition. |
| `spec/forwarding.html` | `core/connection/{forward,proxy}.go` | Local/remote/SOCKS side table, shared controls/limits retained; shell, reverse and lifecycle partitions. |
| `spec/files.html` | `core/connection/{files,remote_files,downloads}.go`, `files_ui.go`, `downloads_ui.go` | Default-root transfer walkthrough; containment, progress and partial-publication semantics retained; `files_test`, files partition. |
| `spec/runs.html` | `core/connection/{runs,scripts}.go`, `follow_ui.go` | Preserve executable script examples, explicit staging/stdin and collection; correct viewer label; runs/follow partitions. |
| `spec/automation.html` | `runs.go`, `automation_lab.py`, public Hovel throw contract | Local/remote results and outer approval kept distinct; automation partition. |
| `spec/reports.html` | `core/reports`, `survey.go`, `survey_ubuntu.sh` | Operational report versus CI Reports distinction, Ubuntu-only preset and partial outcomes; reports partition. |
| `spec/chains.html` | `core/connection/chains.go`, `chains_lab.py` | Existing diagrams preserved; connection versus selected-tunnel flow and fixture prerequisites retained; chains partition. |
| `spec/logs.html` | `activity.go`, `core/connection/activity.go`, `logs.go`, audit sources | Mark workspace follower unreleased; retain operation-note/capture limits; `activity_test`, follow partition. |
| `spec/agent-skills.html` | `agent.go`, `core/agent/install.go`, bundle target, `agent_check.py` | Mark bundled inspection skills unreleased; preserve six destinations, update conflicts and native-discovery limits; `agent_test`, installed recipe in runs partition. |
| `spec/embedded-terminal.html` | `terminal.go`, `core/terminal/host_linux.go` | Keep resize limitation visible outside advanced details; `terminal_test`, `//core/terminal:host_test`. |
| `spec/development-guide.html` | `.aspect/{check,site,report}.axl`, CI/Pages workflows | Gate table, report assembly and publication evidence, scoped #86 exception retained. |
| `spec/troubleshooting.html` (new) | Above production refusal/results paths | Setup, owner, authentication, transfer and evidence recovery; no destructive recovery recipe or automatic retry. |

The README's stale no-release statement and old site-artifact flow were also
corrected. Current agent client destination documentation was already verified
for #63 on this date and is unchanged. No terminal behavior, authentication
policy, historical research or accepted decision was changed.

## Distribution evidence and follow-up

`gh-axi release list`, `release view v0.1.0`, and the release API confirmed the
source-only development release and empty asset inventory. The tag resolves to
`0515137b76b86e54bdccd40eada479bca5243eeb`. Its package BUILD has the executable,
manifest and Hovel license; current source additionally bundles the skills.
The missing prebuilt distribution is scoped in
[DISTRIBUTION-FOLLOWUP.md](DISTRIBUTION-FOLLOWUP.md), separately from this book work.
No release, visibility, push, PR or merge is authorized by this audit.

## Presentation and checks

The existing Pages gate validates every generated page's heading, local links
and fragments, assets, search entries and live-binary API inventory. Staging
materializes only the declared site. Normal examples use default roots, keys,
ports and budgets; advanced overrides keep their concrete use cases.

Local browser inspection enters through a temporary Aspect task, using installed
Chromium 149.0.7827.55 against the staged site. It checks every page at 320, 390
and 1440 pixels, including expanded disclosures, and exercises menu, sidebar,
search, keyboard results, Escape, skip links, heading offsets and keyboard access
to scrolling code blocks. Screenshots and layout evidence
are local inspection artifacts, not CI coverage or a screen-reader certification.
Long paths/hashes exposed horizontal page overflow; inherited dim labels had
poor contrast. Existing palette colors, native text wrapping and minimum table
label widths fix those issues.
The layout and native components remain Hovel-derived; see `UPSTREAM.md`.

Verification completed:

- `aspect burrow-check preflight`: all 39 required targets executed and passed,
  including all nine production SSH partitions. Setup and terminal checks each
  passed three runs; no failed-test retries were used. This builds the production
  package and exercises documented commands against controlled fixtures. The
  three known #86 advisory diagnostics are outside this required gate.
- `aspect burrow-site check` and `aspect burrow-site stage`: passed. The generated
  book, local links/anchors, assets, search index and API inventory are valid;
  the declared site is materialized under `_site/`.
- Browser inspection: all 39 generated pages at three widths (117 layouts),
  expanded disclosures, navigation, search and the keyboard checks above passed.
  Final table padding was included in its minimum label width, then the site
  and browser checks were repeated.
- Independent Standards and Spec reviews: no findings. `git diff --check` passed.

These are local checks, not published CI evidence or the #65 owner walkthrough.
