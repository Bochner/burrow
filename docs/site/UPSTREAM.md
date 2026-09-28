# Hovel documentation adaptation

Source: [vibepwners/hovel](https://github.com/vibepwners/hovel/tree/c461ba282a8aecc7aa3a079a4613bf5e2640c388),
commit `c461ba282a8aecc7aa3a079a4613bf5e2640c388`, inspected 2026-09-11.
Copyright 2026 William Born. The [upstream Apache-2.0 license](public/LICENSE-HOVEL)
is retained verbatim, including its copyright notice.

Adapted sources: root `README.md`, `SECURITY.md`, and `AGENTS.md` conventions;
`docs/site/src/lib/catalog.ts`; book/header/search/layout components and page
routes; `docs/site/public/assets/site.css`; Astro configuration; JavaScript
manifest/lock and toolchain pins; Aspect/Bazel documentation wiring and the
CI-to-Pages artifact promotion pattern.

Changes: Burrow name, artwork, repository/Pages URLs, operator content,
and the authorized-use warning's product name. Book navigation, chapter
numbering, responsive layout, search, and the upstream visual style are retained.
Hovel-specific modules, generated SDK references, demos, version
macros, and Tidewave integration are omitted because Burrow does not have those
surfaces. Staging and checks are small Python tools under `docs/tools/docs/`.
The inherited stylesheet retains its layout and branding. The #92 accessibility
corrections wrap long prose tokens and use the existing muted text color for
dim labels so navigation remains readable on the dark background. The existing
hero grid now fits the image-first Burrow fragment and allows its text column
to shrink instead of overlapping the image or clipping on small screens.
Heading anchors leave room for the sticky header; table labels retain a minimum
readable width on small screens.
Burrow's staging tool also restores owner write access to previously copied
output directories so repeated staging can replace read-only Bazel artifacts.

## Public test reports (#91)

The owner requested Hovel's report application and enforcement model on
2026-09-28. Source pin:
[`1789ce47554a2ee17c7bd46d391b63946af60d0a`](https://github.com/vibepwners/hovel/tree/1789ce47554a2ee17c7bd46d391b63946af60d0a).
Copied active `docs/site/public/assets/report.js` and `report.css`, the report
page shell, and `tools/testreport/testreport.py` (vendored as
`docs/tools/docs/hovel_testreport.py`). The UI regression check adapts Hovel's
`docs/site/report_ui_test.py`. Copyright 2026 William Born; Apache-2.0 under
the retained license.

The full tabbed application replaces Burrow's former reduced static rendering.
Adaptations: `/reports/` data URL, Burrow commands and metrics, honest missing
measurements, links to original repetition outputs, and original execution
identity in Jobs. Static checks reuse their actual target logs; source ignore
inventories are explicitly unmeasured. Test language comes from the declared
BEP rule kind rather than Hovel-specific label guesses. Coverage describes the measured six Go targets without
inventing a threshold; Burrow's JSON schemas are not labeled typed MCP coverage.
The Python adapter supplies Burrow's BEP, XML, LCOV and capability inventory to
Hovel's report model. No Hovel results or coverage percentages are reused.

Publication follows structured evidence, complete referenced files, parity and
the successful workflow gate. Burrow's additional exact-source/run/log-hash and
site-hash report guards are removed by the owner's instruction. Safe CI evidence
selection still verifies the successful run, source tree and artifact download;
release bundles retain checksums. These are pipeline eligibility checks, not a
second enforced report format. Hovel v0.4.4 compatibility checks stay required.

The #65 comparison also inspected the published Hovel report generated on
2026-09-24 at source `b4190bb548c49263003d7d398571c3837717890f`.
`MVP-ACCEPTANCE.md` records applicable differences, including unmeasured Burrow
branch coverage and the absence of a Burrow SDK or required typed MCP surface.
No additional upstream implementation was copied for that comparison.

The API tab (#88) reuses that header, sidebar, table styling and search. Its
operation pages and downloadable JSON are generated from the production
`burrow capabilities` command as a declared build input. It documents Burrow
CLI and base-module routes and links the pinned upstream RPC/SDK sources;
it does not add a Burrow REST service or SDK. `src/lib/api.ts` is original
Burrow rendering code; the upstream stylesheet remains unchanged.

Dependency pins match the inspected Hovel source: Astro 7.0.7, Node 22.20.0,
pnpm 10.20.0, rules_js 3.2.2, rules_nodejs 6.7.5, and rules_python 2.2.0.
Action revisions match Hovel's CI/Pages workflows. No upstream credentials,
remote cache configuration, runtime history, or test reports are copied.

The book publishes only explicit `docs/site` content and assets, not the full
research directory or local worktrees. This attribution applies to
adapted upstream material; it does not select a license for all Burrow code.

## Operator book reconciliation (#92)

The operator reading path, short concept tables, ordered workflows and native
advanced-detail disclosures follow Hovel's user guide at the already-inspected
`a4cbfdf7769a9551695088c11061e3cabc368e07` revision. Existing book/sidebar/search
components and flow-diagram classes are reused; no design system, frontend
library or runtime dependency was added. Burrow's current-source versus v0.1.0
availability, installation, lifetime and troubleshooting text is original.
Historical research and ADRs are unchanged. See `BOOK-AUDIT.md` for the
chapter/source audit and verification scope.

## Chain walkthrough diagrams (#62)

The chain walkthrough reuses the inherited `.diagram`, `.flow-diagram`,
`.flow-step` and semantic state styles without modifying the upstream stylesheet.
Its original Burrow content follows the numbered workflow and separate-interface
presentation in Hovel's [chains chapter](https://github.com/vibepwners/hovel/blob/a4cbfdf7769a9551695088c11061e3cabc368e07/docs/site/src/content/spec/chains-runs.html),
inspected 2026-09-19. Hovel's corresponding VHS tape and declared demo renderer
were also inspected: they produce terminal recordings separately from the HTML
diagrams. No Hovel recording is presented as a Burrow execution, and no VHS,
Chromium, ttyd or ffmpeg runtime dependency is added to the book.

## Embedded terminal forms (#69)

Huh `charm.land/huh/v2 v2.0.3` (MIT), verified as the latest stable release on
2026-09-12: [release](https://github.com/charmbracelet/huh/releases/tag/v2.0.3),
[pinned module graph](https://github.com/charmbracelet/huh/blob/v2.0.3/go.mod),
[license](https://github.com/charmbracelet/huh/blob/v2.0.3/LICENSE).
Its built-in `ThemeCatppuccin(true)` supplies Mocha field styles through
`github.com/catppuccin/go v0.2.0`; Burrow preserves its shared semantic roles and
popup compositor. Context7 integration docs were checked against the pinned
source because Huh's v2 `Model.View` returns a string and Confirm's native Tab
binding submits. Burrow explicitly maps Tab to selection and Enter to approval.
The existing Bubble Tea v2.0.9, Bubbles v2.2.1 and Lip Gloss v2.0.6 pins remain.
Dependency resolution enters through `aspect burrow-prototype deps`; Go module
checksums and declared `MODULE.bazel`/BUILD dependencies record the graph.

## README reconciliation (2026-09-28)

The root README now follows Hovel's README at
`1789ce47554a2ee17c7bd46d391b63946af60d0a`: project overview, documentation,
agent installation, operator installation, development commands, repository
layout, contribution and licensing. Burrow-specific release assets and Linux
requirements replace Hovel's PyPI/SDK/module installation instructions. Milestone
history, CI internals, private-project links and detailed restart/cleanup notes
are removed from the front page; development shortcuts remain in the book.
