# Burrow

![Burrow](docs/site/public/assets/burrow.png)

Burrow is a Go SSH manager for homelab operations, authorized red-team
emulation, controlled lab exercises, defensive validation, and operator workflow
automation. Burrow transparently manages a pinned Hovel dependency, initially
supporting only Hovel instances it starts. Normal quit reviews live connections
and can keep them running in the retained daemon or close them explicitly.
General existing-daemon attachment is deferred.

The project is implementing useful LazySSH SSH parity in five approved
milestones. Linux amd64 is the initial operator platform. **MVP 1–4 are
implemented: verified workspace setup, saved connections, retained SSH,
independent shells, forwarding, file transfers, commands and scripts, live
output, local automation, Ubuntu Markdown reports, and Hovel chains. MVP 5
(operator skills and final daily-use acceptance) remains.** Full LazySSH parity
and a distribution release are not yet claimed.

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

Continue with [commands and scripts](docs/site/src/content/spec/runs.html),
[local automation](docs/site/src/content/spec/automation.html),
[Ubuntu reports](docs/site/src/content/spec/reports.html), and
[SSH and tunnel chains](docs/site/src/content/spec/chains.html). Chains can
connect to an already-running SSH server or consume a selected live tunnel;
server deployment and generic routing remain outside this implementation.

The [prototype evidence guide](docs/research/prototype-evidence.md) indexes the
preserved proofs, pins, results and commands that preceded the production code.

## Develop

For human use, the Makefile offers shortcuts (GNU Make must be installed):

```sh
make run                         # defaults to ~/burrow-test
make run WORKSPACE=/absolute/path # choose another workspace
make restart                     # close connections and shells, then reopen without prompting
make clean                       # build outputs only; not runtime cleanup
make check                       # portable checks; no Docker required
```

Quit existing Burrow frontends before `make restart`. The shortcut passes
`--yes`, ending the selected workspace's retained Burrow manager and all its
connections/shells without another prompt. For a confirmation review, use
`aspect burrow run -- --workspace /absolute/path restart` instead.
Saved settings, evidence and the Hovel daemon remain;
reconnect explicitly afterward to create an owner from the current build.
Restart refuses ambiguous owners and unknown stale reservations rather than
deleting them. `make clean` clears this checkout's Bazel build outputs, which
are regenerated on the next build; it neither fixes stale running owners nor
removes module caches, saved workspaces or source files.

These are human-only conveniences: **agents use Aspect directly**. The Makefile
delegates every workflow to Aspect rather than defining a second build system.

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
| `aspect burrow-check preflight` | Run the required release gate, repeating only setup and terminal checks three times; no cached test results or failed-test retries. Hovel #86 diagnostics run separately. |
| `aspect burrow-check preflight SUITE` | Run one GitHub partition: portable, lifecycle, files, reverse, shell, chains, reports, automation, follow or runs. The Hovel follow-up proofs are excluded from these required partitions. |
| `aspect burrow-check preflight hovel` | Run the advisory WAL-lock regression and two Docker-backed manager proofs tracked in #86. |
| `aspect burrow-check ci` | Build production packages and proofs and run portable checks without Docker. |
| `aspect burrow package` | Build the production Linux amd64 package. |
| `aspect burrow check` | Run the production frontend, launch, terminal and profile checks. |
| `aspect burrow ssh-check` | Run all nine production SSH acceptance partitions against digest-pinned disposable OpenSSH Docker servers. |
| `aspect build //:research` | Check the research metadata build graph; not SSH behavior or prose. |
| `aspect burrow-site build` | Build the hermetic Astro documentation book. |
| `aspect burrow-site check` | Validate generated pages, internal links, assets, and search. |
| `aspect burrow-site stage` | Materialize the documentation site under `_site/`. |

PR and main CI run ten independent `aspect burrow-check preflight SUITE`
jobs, each capped at 15 minutes. The required `repository` check runs even
after failure and succeeds only when every partition succeeds. Full and
preflight gates allow at most two local test processes; preflight repeats
setup and terminal checks three times, while the SIGTERM regression already
exercises 40 cycles inside its unit check. Guided field transitions repeat three
times inside lifecycle acceptance; each SSH partition runs once.
A separate **Hovel compatibility** workflow runs the three `hovel-followup`
targets and reports real failures without blocking the required release gate.
This is a scoped exception for [#86](https://github.com/Bochner/burrow/issues/86);
restore those checks to the required gate after an official Hovel fix is pinned
and verified. `aspect burrow-check` remains the strict full gate, including
these diagnostics. The portable job stages and uploads `docs-site`. Every job retains logs, XML,
terminal captures and available phase/daemon logs as `test-results-SUITE` for
14 days, including after failure. Pages promotes that exact site after
successful main CI and checks that its commit is still current. Manual Pages
dispatch also requires a successful Repository run for that exact main commit;
it downloads the existing artifact without rebuilding or rerunning tests.
Main requires an up-to-date PR and the passing `repository` check, including for
admins; the owner separately authorizes merging. See the
[development guide](docs/site/src/content/spec/development-guide.html) and
[CI audit evidence](docs/research/mvp4-ci-preflight.md).
The historical transport proof keeps its own
host-binary prerequisites documented in
[its README](core/prototype_transport/README.md).

## Known upstream limitation

The pinned official Hovel v0.4.2 runtime can crash while concurrent clients
access its SQLite workspace. This can interrupt active connections and runs;
reconnect remains manual. The release exception does not fix that defect.
Evidence, a proposed upstream patch, and the required follow-up are tracked in
[#86](https://github.com/Bochner/burrow/issues/86). Burrow does not ship a custom
Hovel runtime.

## Repository layout

The repository follows Hovel's separation of application, documentation, tooling,
and agent instructions. Only areas with actual content are created.

| Path | Purpose |
| --- | --- |
| `.aspect/` | Repository workflows backed by declared Bazel targets. |
| `core/cmd/burrow/` | Production Linux frontend: CLI, Charm terminal interface and behavior checks. |
| `core/launch/` | Verified pinned Hovel setup, workspace launch and daemon operations. |
| `core/connection/` | The `burrow` Hovel module: connections, profiles, forwarding, files, retained runs and chains. |
| `core/reports/` | Markdown report registration and verified reading through Hovel artifacts. |
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
