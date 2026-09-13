# Burrow

![Burrow](docs/site/public/assets/burrow.png)

Burrow is a Go SSH manager for homelab operations, authorized red-team
emulation, controlled lab exercises, defensive validation, and operator workflow
automation. Burrow transparently manages a pinned Hovel dependency, initially
supporting only Hovel instances it starts. Normal quit reviews live connections
and can keep them running in the retained daemon or close them explicitly.
General existing-daemon attachment is deferred.

The project is implementing useful LazySSH SSH parity in five approved
milestones: shells, file transfers, forwarding, saved connections, and user
scripting through supported Hovel contracts. Linux amd64 is the initial operator
platform. **MVP 1 (setup, profiles and connections) is complete: the production
Linux package launches a verified Hovel workspace, manages saved connection
collections and authenticates real retained OpenSSH connections. Interactive
shells, forwarding, file workflows, scripts and skills follow in MVP 2 to 5.**

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

Build the production Linux amd64 package and run it against a private
workspace. First launch downloads the pinned Hovel wheel into the user cache;
`--offline` reuses a verified cached executable.

```sh
aspect burrow package
aspect burrow run -- --workspace /absolute/private/workspace
```

The declared `//core/cmd/burrow:package` target produces an archive containing
the `burrow` executable and the public-SDK module manifest. Follow the
[launch guide](docs/site/src/content/spec/launch.html), then the
[named connection](docs/site/src/content/spec/connections.html) and
[saved collection](docs/site/src/content/spec/profiles.html) guides. There is no
distribution release yet. See the
[proposed Hovel compatibility convention](docs/research/hovel-daemon-compatibility-handoff.md)
for the deferred upstream handoff and current development boundary.

The [prototype evidence guide](docs/research/prototype-evidence.md) indexes the
preserved proofs, pins, results and commands that preceded the production code.

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
| `aspect burrow-check` | Run metadata, documentation, SDK, daemon reuse and production SSH checks; requires Docker and OpenSSH client tools. |
| `aspect burrow-check ci` | Build production packages and proofs and run portable checks without Docker. |
| `aspect burrow package` | Build the production Linux amd64 package. |
| `aspect burrow check` | Run the production frontend, launch, terminal and profile checks. |
| `aspect burrow ssh-check` | Run production connection acceptance against a digest-pinned disposable OpenSSH Docker server. |
| `aspect build //:research` | Check the research metadata build graph; not SSH behavior or prose. |
| `aspect burrow-site build` | Build the hermetic Astro documentation book. |
| `aspect burrow-site check` | Validate generated pages, internal links, assets, and search. |
| `aspect burrow-site stage` | Materialize the documentation site under `_site/`. |

CI runs the portable `ci` gate, the Docker-backed SSH acceptance lab and site
staging, then uploads the validated site. Pages promotes that exact artifact
after successful main-branch CI. The historical transport proof keeps its own
host-binary prerequisites documented in
[its README](core/prototype_transport/README.md).

## Repository layout

The repository follows Hovel's separation of application, documentation, tooling,
and agent instructions. Only areas with actual content are created.

| Path | Purpose |
| --- | --- |
| `.aspect/` | Repository workflows backed by declared Bazel targets. |
| `core/cmd/burrow/` | Production Linux frontend: CLI, Charm terminal interface and behavior checks. |
| `core/launch/` | Verified pinned Hovel setup, workspace launch and daemon operations. |
| `core/connection/` | The `burrow` Hovel module: retained OpenSSH connections, profiles and SSH config. |
| `core/terminal/` | Embedded terminal host for the Hovel CLI tab. |
| `core/prototype_*/` | Bounded historical proofs, preserved as evidence rather than production code. |
| `docs/site/` | Astro Pages source, book content, shared components, and assets. |
| `docs/tools/docs/` | Documentation validation and staging tools. |
| `docs/research/` | Primary-source evidence and upstream provenance. |
| `docs/agents/` | Tracker, domain, triage, and documentation conventions. |
| `.agents/skills/` | Installed project agent skills. |
| `AGENTS.md`, `CLAUDE.md` | Canonical agent instructions and a symlink to the same file. |

The Go application lives under `core/` following Hovel's organization.
`modules/` and `sdk/` are reserved for real module packages or a supported Burrow
SDK if needed. All production capabilities belong to the single base `burrow`
Hovel module. Local upstream checkouts live under ignored `.references/`.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Run `aspect burrow-check` before landing
changes. Describe what was tested and distinguish documentation/build checks
from runtime interoperability evidence.

## License

Burrow has no project-wide distribution license selected yet. The adapted Hovel
README structure, warning, agent conventions, and documentation theme retain
Hovel's [Apache-2.0 license](docs/site/public/LICENSE-HOVEL) and
[provenance/attribution](docs/site/UPSTREAM.md).
