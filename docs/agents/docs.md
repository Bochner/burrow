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

CI runs ten `aspect burrow-check preflight SUITE` jobs: portable, lifecycle,
files, reverse, shell, chains, reports, automation, follow and runs. Each has a
15-minute cap. Under the owner-approved #86 exception, follow failures produce
a warning but do not fail its hosted job or the `repository` aggregate. The
other nine suites, coverage and report assembly remain required. Follow results
and logs retain their actual failed/missing status; they earn no passing
behavior credit. Local preflight and the default full gate still return failures. The manually dispatched Hovel compatibility workflow runs the three #86
`hovel-followup` targets as advisory diagnostics; failures remain visible but
do not block Repository or Pages. It does not run automatically on pushes or
pull requests; unrun advisory checks remain missing in Reports. Restore follow and the three diagnostics to
the required gate after an official fixed runtime is pinned and verified. The strict default full gate
still includes them. Each preflight records commit-bound BEP results under
`.report-input/`; the portable job stages `docs-base`. A required coverage job
measures production Go lines. The report job combines all ten partitions and
coverage from the same source/run, attaches available same-commit advisory
evidence, and uploads `docs-site` only after `aspect burrow-report release` passes.
Unavailable advisory results remain explicitly missing. Repository requires
checks, coverage and report assembly to succeed.
Pages verifies the report commit and the complete site artifact's hashes through
`aspect burrow-report verify SHA`, then promotes that artifact from successful main CI. Manual dispatch
finds a successful Repository run for its exact main commit and promotes the
existing artifact; it does not rebuild or rerun checks. Both paths skip
deployment if main has advanced. Site artifacts and each job's
`test-results-SUITE` diagnostics are retained for 14 days. Repository
privacy alone does not make a Pages site private: confirm intended publication
visibility before enabling Pages. Never stage the repository or research tree
as the public site.

The public Reports page adapts Hovel's report layout using native links, tables
and disclosure controls. Generate its initial missing-evidence state from
`burrow capabilities` and `docs/tools/docs/parity.json`; every inventory ID must
have an explicit behavior-check binding. Update those bindings when adding a
capability, and preserve separate reachability, schema and selected-semantics
metrics. Prototype tests never contribute production coverage. For a local
report, run preflight, coverage, site staging, then `aspect burrow-report render`
without editing source between steps. Dirty local evidence is inspectable but
cannot pass publication. `aspect burrow-report release` additionally requires
usable agent routes and passing required behavior evidence for every inventory capability;
only the exact SSH follow target has advisory behavior bindings under #86.
Its failed/missing checks stay visible and never count toward demonstrated parity;
it requires usable direct routes or explicit supported equivalents for all
operational capabilities. Presentation-only entries stay visible with their
own behavior checks. Schema documentation and owner acceptance remain separate.
Repeated local suites preserve previous evidence in `.report-input/archive/`;
archived failures must be diagnosed and never silently replaced by a passing retry.
The required CI report job uses `aspect burrow-report release`, enforcing these
operational route and behavior checks before the `repository` aggregate passes.

See [the upstream pin and adaptation notes](../site/UPSTREAM.md) before updating
copied Hovel theme code or dependency versions.
