# Documentation authoring

The Pages book follows Hovel's custom Astro format. Edit literal HTML fragments
under `docs/site/src/content/`. Each starts with metadata and exactly one `h1`:

```html
<!-- burrow-doc: {"title":"Example","group":"Foundations","order":120} -->
<article><h1>Example</h1><p>Page content.</p></article>
```

The relative content path determines the URL. `group` and `order` generate
contents, chapter numbering, sidebar, previous/next navigation, and search.
Orders are unique within a group; supported groups live in `src/lib/catalog.ts`.
Optional `navTitle` and `description` improve navigation and search.

Shared chrome belongs in `src/components/` and `src/layouts/`; static assets in
`public/`. Do not copy full HTML documents or generated chrome into fragments.
`_site/` is generated. Keep research in `docs/research/` and link it from the book
when useful; GitHub decision tickets remain the canonical resolutions.

Run `aspect burrow-site check` after changes, and `aspect burrow-site stage` to
materialize the artifact. All tools are declared Bazel inputs; update JS locks
through the Bazel-managed pnpm target via Aspect. No CDN/runtime network assets.

Optional local browser inspection uses `aspect burrow-site browser` with
`BURROW_DOCS_BROWSER` set to an absolute Chromium executable path. This manual
target is a host-tool exception: it checks rendering and scrolling but supplies
no pinned CI evidence. Record the browser version with inspection results.

Application PRs run eleven required `aspect burrow-check verify SUITE` jobs:
portable, lifecycle, files, reverse, shell, chains, reports, automation, follow,
runs and hovel. `verify` permits deterministic test caching; external/process,
race and fuzz checks stay fresh. Each target runs once; failed-test retries remain
disabled. The six `production-coverage` targets run only in the coverage job and
supply both their behavior results and line measurements. `preflight` and a forced full Repository
dispatch execute everything uncached. The Hovel partition still requires its
WAL regression and two retained-manager proofs.

The CI selector may inherit evidence from successful same-repository Repository
runs. Main requires the same tested Git tree and merge parents; narrow prose-only
PRs require an exact successful main baseline and unchanged application inputs,
then run current documentation checks. Missing/expired evidence, lookup errors,
unknown paths, deletions and unsupported merges select full validation. Original
suite commits and run IDs are retained; Jobs shows their execution identity.
The required `repository` aggregate rejects failed, cancelled or unexpected
skipped jobs. Main also requires the final distribution build and smoke checks.

The report follows Hovel's structured data model and interactive application:
Overview, Linters, Coverage, Suites, Jobs and Targets with logs/XML/cases. The
owner explicitly chose Hovel's report enforcement on 2026-09-28. The former
same-source/run/per-file-hash/site-hash publication contract is removed. Keep
schema/target completeness, referenced-file validation and operational parity
checks. CI eligibility and release checksums are pipeline concerns, separate
from the report's presentation/data contract. Pages downloads the successful
same-repository main artifact and skips deployment if main has advanced; manual
Pages dispatch finds an exact successful main run. It does not rebuild or add a
second report manifest gate. Evidence/site/bundle retention is 30 days; additional
failure diagnostics are seven days. Burrow and its release downloads are public.
Never stage the repository or research tree as the Pages site.

Generate initial missing-evidence state from `burrow capabilities` and
`docs/tools/docs/parity.json`; each inventory ID needs a behavior-check binding.
Preserve separate reachability, JSON schema and selected-behavior measurements;
optional MCP is not typed MCP coverage. Prototypes do not contribute production
coverage. For a local report, run preflight (including coverage), site staging and
`aspect burrow-report render`. Archive individual suite inputs before switching
to an all-suite preflight to avoid overlapping evidence. Repeated local suites
preserve their previous files in `.report-input/archive/`; diagnose failures.
`aspect burrow-report release` requires all suites, measured coverage, usable
operational agent routes and passing selected behavior checks. Owner acceptance
and exhaustive semantic equivalence are separate.

See [the upstream pin and adaptation notes](../site/UPSTREAM.md) before updating
copied Hovel theme code or dependency versions.

A new main `VERSION` triggers automatic GitHub tagging/release publication only
after Repository CI succeeds and its exact bundle verifies. The matching
`CHANGELOG.md` section supplies release notes. Unchanged versions skip publication;
downgrades, missing notes and tags naming another commit fail. Manual rehearsal
and optional PyPI publishing remain available. See root AGENTS.md for PR versioning.
