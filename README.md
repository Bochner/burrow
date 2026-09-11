# Burrow

![Burrow](docs/site/public/assets/burrow.png)

Burrow is a planned Go SSH manager for homelab operations, authorized red-team
emulation, controlled lab exercises, defensive validation, and operator workflow
automation. Burrow will transparently manage a pinned Hovel dependency, initially
supporting only Hovel instances it starts. Normal quit will detach and retain
the daemon and live resources. General existing-daemon attachment is deferred.

The project is charting an implementation-ready specification for useful LazySSH
SSH parity: shells, file transfers, forwarding, saved connections, and user
scripting through supported Hovel contracts. Linux is the initial operator
platform. **No SSH executable or installable package exists yet.**

> **Authorized red-team emulation only.** Use Burrow only in environments you own
> or are explicitly authorized to assess, with written scope and approvals. See
> [SECURITY.md](SECURITY.md).

## Documentation

The documentation uses the same Astro GitHub Pages book format as Hovel.

## [bochner.github.io/burrow](https://bochner.github.io/burrow/index.html)

GitHub Pages publishes the documentation after successful checks on main.
Start with the [User Guide](docs/site/src/content/spec/user-guide.html) for current
scope and status, then [Hovel Integration](docs/site/src/content/spec/hovel-integration.html).
Contributors should read the [Development Guide](docs/site/src/content/spec/development-guide.html).
The source for the book lives under [`docs/site/src/content/`](docs/site/src/content/).

The [Wayfinder map](https://github.com/Bochner/burrow/issues/1) is the canonical
index of implementation decisions. Existing [research](docs/research/) provides
evidence, not an approved architecture.
Use the [Burrow project](https://github.com/users/Bochner/projects/9) to scan
open and completed issues in the same views as Tirnaill.

## Install

There is no production runtime package to install yet. Inert external SDK,
terminal and setup proofs are preserved as evidence; the first real SSH slice
remains a Wayfinder decision. See the
[proposed Hovel compatibility convention](docs/research/hovel-daemon-compatibility-handoff.md)
for the deferred upstream handoff and current development boundary.

The [prototype evidence guide](docs/research/prototype-evidence.md) indexes the
preserved code, pins, results and commands for continuing development.

## Develop

Aspect CLI is the single entry point for building, testing, linting, formatting,
packaging, and local runs. Tool versions are pinned in `.aspect/version.axl`,
`.bazelversion`, `MODULE.bazel`, and the documentation dependency lockfile.

```sh
aspect help
aspect burrow-check
aspect burrow-site stage
```

Useful commands:

| Command | Description |
| --- | --- |
| `aspect burrow-check` | Run the current repository metadata and documentation gates. |
| `aspect build //:research` | Check the research metadata build graph; not SSH behavior or prose. |
| `aspect burrow-site build` | Build the hermetic Astro documentation book. |
| `aspect burrow-site check` | Validate generated pages, internal links, assets, and search. |
| `aspect burrow-site stage` | Materialize the documentation site under `_site/`. |

CI runs the same Aspect gates and uploads the validated site. Pages promotes
that exact artifact after successful main-branch CI.

## Repository layout

The repository follows Hovel's separation of application, documentation, tooling,
and agent instructions. Only areas with actual content are created.

| Path | Purpose |
| --- | --- |
| `.aspect/` | Repository workflows backed by declared Bazel targets. |
| `docs/site/` | Astro Pages source, book content, shared components, and assets. |
| `docs/tools/docs/` | Documentation validation and staging tools. |
| `docs/research/` | Primary-source evidence and upstream provenance. |
| `docs/agents/` | Tracker, domain, triage, and documentation conventions. |
| `.agents/skills/` | Installed project agent skills. |
| `AGENTS.md`, `CLAUDE.md` | Canonical agent instructions and a symlink to the same file. |

The Go application will belong under `core/` when implementation starts.
`modules/` and `sdk/` are reserved for real module packages or a supported Burrow
SDK if needed. Internal architecture and nested build workspaces remain map
decisions. Local upstream checkouts live under ignored `.references/`.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Run `aspect burrow-check` before landing
changes. Describe what was tested and distinguish documentation/build checks
from runtime interoperability evidence.

## License

Burrow has no project-wide distribution license selected yet. The adapted Hovel
README structure, warning, agent conventions, and documentation theme retain
Hovel's [Apache-2.0 license](docs/site/public/LICENSE-HOVEL) and
[provenance/attribution](docs/site/UPSTREAM.md).
