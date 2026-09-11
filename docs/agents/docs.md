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

CI uploads the checked `_site/` as `docs-site`; Pages promotes that exact artifact
from successful main CI. Manual dispatch repeats the same gate. Repository
privacy alone does not make a Pages site private: confirm intended publication
visibility before enabling Pages. Never stage the repository or research tree
as the public site.

See [the upstream pin and adaptation notes](../site/UPSTREAM.md) before updating
copied Hovel theme code or dependency versions.
