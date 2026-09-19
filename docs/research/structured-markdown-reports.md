# Structured Markdown reports in Burrow

Researched 2026-09-19 for [#61](https://github.com/Bochner/burrow/issues/61).
This is evidence and a recommendation for the owner's requested reconsideration,
not a replacement implementation specification. No runtime code or dependencies
were changed. Context7 discovery was checked against the release-tagged primary
sources below; no renderer integration or security behavior was executed.
Repository baseline: local `mvp4` at
`b662422590d8468e3a9531e1262439bf99c47be1`.

## Current scope

The owner clarified during this research that the first survey should be a
proof of concept for the existing lab and Ubuntu, because LazySSH's command
assumptions broke across systems. They also proposed a Reports tab under Ctrl+L,
and explicitly requested the full recommendations before implementation begins.
These directions narrow the proposal below; no implementation has begun.

The owner subsequently clarified that existing Ctrl+L Collected output and
Activity log behavior must remain intact. Reports are a separate capability;
the proposed replacement tab strip is withdrawn. They suggested
`run survey ubuntu-test --ubuntu`, with an explicitly selected OS command set,
automatic report saving as part of that operation, and a report format that
future commands/plugins/modules can also produce. The command and shared report
contract below are recommendations for discussion, not implemented behavior.

#61 already asks for a selected enumeration script, a structured Markdown Hovel
artifact, and an embedded reader with scrolling, resize, no-color and unchanged
source bytes. It separates collection from viewing and allows Glow reuse only
when it reduces maintenance without a mandatory process or application fork.
Its Dracula references are superseded by the accepted Catppuccin Mocha
[TUI standard](../agents/tui.md).

## Library choice

**Recommendation: use Glamour if a concrete Markdown-producing workflow is kept.**
It provides the missing rendering function; Burrow already has the surrounding
terminal application. Glow is useful as a standalone reader and implementation
reference, but its public integration surface does not remove Burrow's work.

| Option | Verified current surface | Fit for Burrow |
| --- | --- | --- |
| Glamour [v2.0.1](https://github.com/charmbracelet/glamour/releases/tag/v2.0.1), released 2026-06-12 | `charm.land/glamour/v2`; Markdown input produces a rendered string, with explicit stylesheet and wrapping options | Embed inside Burrow's existing Bubble Tea application |
| Glow [v3.0.0](https://github.com/charmbracelet/glow/releases/tag/v3.0.0), released 2026-08-11 | Standalone reader; now uses Bubble Tea, Bubbles and Lip Gloss v2 | Same framework generation, but still a separate application surface |

Glamour's `Render` does not itself own a terminal. It supports GitHub-flavored
Markdown and definition lists; its public options include `WithStyles`,
`WithWordWrap` and table wrapping. Headers, paragraphs, lists, small tables and
code blocks are sufficient for a first report. These are presentation features,
not report generation, artifact storage or a findings data model.
[Renderer source](https://github.com/charmbracelet/glamour/blob/v2.0.1/glamour.go).

Glow's exported `ui.NewProgram` returns another `tea.Program`; its root model and
`pagerModel` are unexported. The pager includes document editing, clipboard
actions and file watching. Extracting that private model or wrapping the whole
application would retain integration work and couple Burrow to Glow internals.
This is a source-based maintenance assessment, not a claim that Glow cannot be
embedded by any means. [UI entrypoint](https://github.com/charmbracelet/glow/blob/v3.0.0/ui/ui.go),
[pager](https://github.com/charmbracelet/glow/blob/v3.0.0/ui/pager.go).

## Integration with the current TUI

The current production TUI already uses Bubbles viewports for live output and
help: [follow_ui.go](../../core/cmd/burrow/follow_ui.go) and
[theme.go](../../core/cmd/burrow/theme.go). Collected output currently follows a
different path: [collected_notes.go](../../core/cmd/burrow/collected_notes.go)
builds a plain snapshot for the Vim viewer and limits each saved stream preview
to 64 KiB. An embedded Markdown artifact reader is still new work; it is not an
existing reader with a theme switch.

Keep Ctrl+L's Collected output and Activity log tabs, their Vim reader, search,
keybindings and navigation intact. Reports should have their own inventory and
native Glamour reader. Recommend an explicit `reports` command and menu entry
to open that view. An additional entry from Ctrl+L remains optional only if it
is additive and preserves both existing tabs; do not make restructuring those
tabs a prerequisite for this feature. Opening and closing Reports must preserve
the previous management context. This supersedes this note's earlier proposed
Burrow-owned Results tab strip.

Keep original Markdown and derive a rendered display for the available center
panel width. Re-render when that width changes, then update the viewport while
preserving a useful reading position. Bubbles already provides scrolling,
dimensions and content updates; it does not reparse Markdown. Glow demonstrates
the same source-document-to-Glamour-to-viewport pattern on resize.
[Bubbles viewport](https://github.com/charmbracelet/bubbles/blob/v2.2.1/viewport/viewport.go),
[Glow resize/render path](https://github.com/charmbracelet/glow/blob/v3.0.0/ui/pager.go).

Glamour v2 removed `WithAutoStyle` and `WithColorProfile`: terminal capability
adaptation is now outside its rendering function. Inside Burrow, return the
rendered string to Bubble Tea; the documentation's standalone `lipgloss.Print`
example must not become an extra stdout writer.
[v2 migration guide](https://github.com/charmbracelet/glamour/blob/v2.0.1/UPGRADE_GUIDE_V2.md).

Use a small stylesheet derived from Burrow's existing theme roles, not a second
palette or environment-selected style file. Glamour exposes heading, table,
inline-code and Chroma token styles. Generic Markdown cannot infer that arbitrary
prose contains a hostname or connection ID: keep authoritative connection/run
metadata in Burrow's semantic header. Respect the existing no-color path and
verify retained text; the `ascii`/`notty` stylesheet alone is not proof that no
terminal controls remain, because link rendering is separate.
[Style schema](https://github.com/charmbracelet/glamour/blob/v2.0.1/ansi/style.go),
[built-in styles](https://github.com/charmbracelet/glamour/blob/v2.0.1/styles/styles.go).

## Untrusted text and unchanged evidence

Glamour is a renderer, not a verified terminal-safety boundary. Its HTML handling
uses Bluemonday's strict policy, but normal text and code spans also call
`html.UnescapeString`. Its base renderer applies styles to the resulting tokens.
Therefore escaping raw control bytes only before Markdown rendering does not
establish safety for entity-encoded controls. This is a source-derived concern,
not a demonstrated exploit or a security audit.
[HTML handling](https://github.com/charmbracelet/glamour/blob/v2.0.1/ansi/context.go),
[element conversion](https://github.com/charmbracelet/glamour/blob/v2.0.1/ansi/elements.go),
[base rendering](https://github.com/charmbracelet/glamour/blob/v2.0.1/ansi/baseelement.go).

Links generate OSC 8 hyperlinks. The link helper checks URL parseability and
excludes fragment-only references; it does not express Burrow's desired scheme
policy. Images render textual labels/destinations, also with hyperlinks, rather
than fetching raster content. For an initial passive report reader, recommend
visible URLs without active hyperlinks or automatic opening/fetching.
[Links](https://github.com/charmbracelet/glamour/blob/v2.0.1/ansi/link.go),
[images](https://github.com/charmbracelet/glamour/blob/v2.0.1/ansi/image.go).

Reuse the existing controlled artifact-reading and remote-text handling where
applicable, but prove the complete rendering boundary: raw and entity-encoded
controls, OSC 8/52, invalid UTF-8 and format characters must not acquire terminal
effects. Allow only intended display formatting. Restrict document size and
report incomplete capture explicitly. All escaping/rendering belongs to a
derived display; keep the registered original bytes and verify their hashes
before/after viewing, as #61 requires. Viewing must never imply collection.

## Dependency and maintenance cost

Burrow currently pins Bubble Tea v2.0.9, Bubbles v2.2.1 and Lip Gloss v2.0.6 in
[go.mod](../../core/prototype_sdk/go.mod), with Go 1.26.5 in
[MODULE.bazel](../../MODULE.bazel). Glamour v2.0.1 requires Go 1.25.8 and Lip Gloss
v2.0.4, so the declared minimum versions fit the existing generation. This is
manifest compatibility only; an Aspect build remains necessary.
[Glamour manifest](https://github.com/charmbracelet/glamour/blob/v2.0.1/go.mod).

Glamour adds Markdown parsing and syntax rendering dependencies, including
Goldmark, Chroma, Goldmark Emoji and Bluemonday. Glow depends on Glamour too, plus
application concerns such as Cobra/Viper, filesystem watching, directory search,
editor and clipboard helpers. The smaller dependency surface is Glamour with the
already-installed viewport, not the full Glow application. Any implementation
must pin and package its selected graph through the existing Aspect workflow.
[Glamour manifest](https://github.com/charmbracelet/glamour/blob/v2.0.1/go.mod),
[Glow manifest](https://github.com/charmbracelet/glow/blob/v3.0.0/go.mod).

The meaningful acceptance proof is one real script-to-collected-artifact-to-reader
flow: correct success/failure facts, unchanged bytes, reopening after connection
close, and restoration of the previous context. Add narrow/large resize,
no-color, malicious text, missing/unreadable/partial artifacts and bounded-size
behavior to that flow. Library documentation alone does not prove these outcomes.

## Which reports would be useful?

Recommendation: start with one **lab/Ubuntu host survey**, produced only after
the operator selects a connection and approves an ordinary script run. This gives the viewer
a repeatable job and a real collected artifact for its acceptance check. Glamour
and Glow format Markdown; neither decides which facts to gather or authors an
operational report. The options below are proposed scope, not new accepted
requirements.

| Report | Useful contents and source | Recommendation |
| --- | --- | --- |
| Host survey | Host/OS/kernel/architecture, current identity, uptime, memory/disk usage, interfaces/routes/listeners, failed services where supported; results from a small selected remote script. | First report for #61. Useful for homelab inspection and an initial engagement snapshot. |
| Operator script report | Markdown emitted by a selected remote script or local tool; for example a backup check or application health check. | Reuse the same artifact format and reader. Burrow need not understand the report's domain or ship a plugin registry. |
| Run or transfer recap | Existing run metadata, stdout/stderr, transfer outcomes and activity records. | Current Results/activity views already cover much of this. Add portable Markdown summaries only when sharing them has a concrete use. |
| Security findings or engagement summary | Selected checks with evidence, interpretation and limitations; a cross-host summary also needs aggregation rules. | Later, after choosing actual checks and consumers. A reader alone does not establish vulnerability assessment, compliance, or complete engagement coverage. |

The portability proof should test the existing digest-pinned
[OpenSSH lab](../../core/connection/lab-image.txt) and one explicitly recorded
Ubuntu release. This research observed Ubuntu 26.04.1 in the local machine's
`/etc/os-release`; it did not execute a survey there. Record the actual tested
release rather than promising every Ubuntu version or Linux distribution.

Use a small fixed set of probes with compatible arguments and command-availability
checks, not a distribution adapter framework. Prefer ordinary `uname`, `id`,
`df -P` and bounded reads of Linux `/proc` data where applicable. Optional network
and service probes must report unavailable tools and absent service managers;
do not install packages, invoke sudo, or reinterpret an unsupported option as
an empty successful result. Reading `/etc/os-release` as data must not execute
its contents. A missing optional section should leave the rest of the survey
useful. These are proposed probe choices to verify on both environments, not
claims that their complete command syntax has already been exercised.

The first survey should have a title, collection time and target context,
sections for observed facts, and a clearly visible list of failed, skipped or
unavailable checks. Each check should retain its command, exit status and useful
error text. Distinguish "no matching results" from "permission denied",
"tool unavailable" and "capture incomplete". An exit-zero script does not prove
every probe succeeded, and complete output can accurately describe probe failures.

Avoid broad environment dumps, credential files and unrelated process arguments
in the starter survey. This is a proposed data-minimization choice, not a claim
that Burrow can redact arbitrary script output. Scripts and local tools can
already print sensitive data into retained evidence. The existing
[automation guide](../site/src/content/spec/automation.html) states this boundary.

An illustrative report outline, **not collected data**:

- Host survey: selected host, observation time, script identity.
- Coverage: completed checks, failed checks, skipped checks.
- System and current user.
- Resources: uptime/load, memory, disks.
- Network and services: interfaces, routes, listeners, failed services.
- Check details: actual command, observation, status and error.

## What the existing sources establish

Burrow was inspected at local commit
`b662422590d8468e3a9531e1262439bf99c47be1` on `mvp4`.
It already includes the #58 script path, #59 independent output viewers and #60
local tools. [Script inputs](../../core/connection/scripts.go) retain reviewed
source snapshots and explicit stream/inline/stage semantics;
[run execution and collection](../../core/connection/runs.go) retain stdout,
stderr and run metadata. Collection currently registers stdout/stderr as
`application/octet-stream`, not recognized Markdown reports. Explicit report
identity/media type and artifact selection are still work, not a capability
provided by installing Glamour.

[Collected Results](../../core/cmd/burrow/collected_notes.go) is a projection of
Hovel's artifact inventory. It shows run outcomes and a bounded 64 KiB preview
of each stream through the existing [Vim viewer](../../core/cmd/burrow/logs.go).
Its file reader checks workspace artifact paths, regular-file ownership,
permissions, link count and recorded size. The Markdown reader should reuse
that boundary where applicable and add report integrity/status handling; it
must not treat that truncated preview as the complete report. Existing
[run lab assertions](../../core/cmd/burrow/runs_lab.py) check artifact hashes and
retention after run close; these assertions were inspected, not executed for
this research.

The pinned public Hovel SDK supports file artifacts with a caller-supplied MIME
type via `FileArtifact`, and returns them through `WithArtifacts`. This provides
the existing mechanism for a Markdown artifact; a second artifact database,
Hovel module or execution layer is unnecessary. [Pinned SDK result contract](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/result.go).

LazySSH provides a concrete precedent, rather than a speculative report source.
At inspected commit `9eb84452c31cb527bf8e938e23ffc92974fb91cb`,
its enumeration implementation retains per-probe command/status/stdout/stderr,
computes separate heuristic findings, and writes JSON plus a plain-text survey.
It does **not** generate Markdown. Its probe plan covers system, users, network,
processes, packages, filesystem and security, including much broader collection
than the proposed starter report.
[Enumeration source](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/plugins/enumerate.py),
[probe plan](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/plugins/_enumeration_plan.py).

The LazySSH tests explicitly check failure counts and rendered exit/error
details. Preserve that useful truthfulness. The proposed small survey does not
reproduce its full probe catalogue, exploit suggestions or findings heuristics;
[#43's exclusions](https://github.com/Bochner/burrow/issues/43) do not require all
bundled engagement-plugin capabilities.
[Failure and report tests](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_enumerate_summary.py).

## Smallest useful delivery to discuss

1. Add a `run survey` entry point selecting a named connection and an explicit
   OS survey preset. Supply one inspectable Ubuntu survey for the proof, with
   literal probe output safely separated from authored Markdown structure.
   Reuse confirmed retained execution, timeouts, budgets and collection; show
   the selected command set and that the operation will save a report in review.
   Do not automatically enumerate on connect.
2. Save the report through Hovel's existing artifact contract as part of the
   confirmed survey operation, following the existing `run now` execution-plus-
   collection precedent. Expose execution, capture and saving outcomes separately.
   Keep source bytes, metadata, stderr and partial evidence intact. Ordinary
   commands retain their current behavior and are not automatically converted
   into reports. Viewing never triggers collection.
3. Open the collected report in a separate Reports view, using Glamour and its
   existing viewport/context patterns. Keep the Markdown file useful outside
   Burrow. Returning from the reader restores the prior context; opening it
   never launches, collects, updates or consumes anything.
4. Prove the real script-to-artifact-to-viewer path: per-check failures,
   incomplete capture, missing/unreadable/replaced artifacts, hostile Markdown
   and controls, original hashes before/after viewing, persistence after close,
   scrolling, resize/reflow, Catppuccin roles, NO_COLOR and context return.

### Command and reusable report contract

The owner's example is `run survey ubuntu-test --ubuntu`: `ubuntu-test` selects
the existing connection, and `--ubuntu` selects the commands to run. A suggested
syntax refinement is `run survey ubuntu-test --os ubuntu`, making the choice one
required value instead of accumulating mutually exclusive flags. This refinement
is not an owner decision. Either spelling should select a known reviewed plan,
refuse unknown selections and avoid silent OS substitution.

Keep the survey preset responsible for command selection and execution needs.
The report reader should not care whether a report came from a survey, another
command or a future producer. A common contract needs Markdown content and
enough metadata for title, producer, selected target, creation time, provenance
and completeness. Reuse Hovel's existing artifact/run metadata where it already
supplies those fields; keep survey-specific probe details in the survey report.
Ordinary Markdown remains the portable document, not a proprietary replacement
format. Do not build a second report database or duplicate daemon-owned state.

The report capability can expose the small operations to save/register a report,
list reports and read one. Survey is its first caller, not its permanent owner.
All Burrow capabilities still use the single base `burrow` Hovel module. Future
producers should reuse the artifact convention when implemented, without adding
a plugin loader, a provider registry or empty adapters now. Future OS presets
must validate their own execution requirements; sharing the report contract
does not establish that a non-Ubuntu target accepts the Ubuntu script path.

Exact command spelling, the starter probe list and any optional Ctrl+L access
remain choices to settle after this research. A reporting framework, Markdown-to-PDF export,
automatic AI summaries, third-party output parsers and a Glow fork are not
needed for this first workflow. If there is no recurring need for even the host
survey or user-authored Markdown, deferring #61 is also reasonable—but would be
a deliberate scope change from the current accepted ticket, not completion.

## Research validation

`aspect burrow-check ci` passed while preparing this note: the declared package
builds succeeded and all 22 existing portable checks passed from cache. These
checks cover existing behavior, not the unimplemented survey or report reader.

This note records source inspection and recommendations only. No enumeration
was executed, no report viewer or report producer was implemented, and no
runtime dependency was added. The research metadata target can establish that
the note is included in the declared build inputs; it does not validate this
prose or demonstrate the proposed runtime behavior.
