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

Changes: Burrow name, artwork, repository/Pages URLs, planning-stage content,
and the authorized-use warning's product name. Book navigation, chapter
numbering, responsive layout, search, and the upstream visual style are retained.
Hovel-specific modules, generated SDK references, demos, report pages, version
macros, and Tidewave integration are omitted because Burrow does not have those
surfaces. Staging and checks are small Python tools under `docs/tools/docs/`.
The inherited stylesheet is kept intact apart from attribution/branding.
Burrow's staging tool also restores owner write access to previously copied
output directories so repeated staging can replace read-only Bazel artifacts.

Dependency pins match the inspected Hovel source: Astro 7.0.7, Node 22.20.0,
pnpm 10.20.0, rules_js 3.2.2, rules_nodejs 6.7.5, and rules_python 2.2.0.
Action revisions match Hovel's CI/Pages workflows. No upstream credentials,
remote cache configuration, runtime history, or test reports are copied.

The book publishes only explicit `docs/site` content and assets, not the full
private research directory or local worktrees. This attribution applies to
adapted upstream material; it does not select a license for all Burrow code.

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
