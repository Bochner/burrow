# Burrow

![Burrow](docs/site/public/assets/burrow.png)

Burrow is a Go SSH manager for homelab operations, authorized red-team
emulation, controlled lab exercises, defensive validation, and operator workflow
automation. It brings saved connections, retained shells, forwarding, file
transfers, commands and scripts, live output, local automation, Markdown reports,
and SSH chains into one terminal interface.

Burrow manages a pinned [Hovel](https://github.com/vibepwners/hovel) runtime for
workspace state and retained sessions. Operators and agents use the same
capabilities through the terminal interface and CLI. Quitting the frontend lets
you keep live connections running or close them explicitly.

> **Authorized red-team emulation only.** Use Burrow only in environments you own
> or are explicitly authorized to assess, with written scope and approvals. See
> [SECURITY.md](SECURITY.md).

## Documentation

The canonical documentation is the GitHub Pages book:

## [bochner.github.io/burrow](https://bochner.github.io/burrow/index.html)

Start with the [User Guide](docs/site/src/content/spec/user-guide.html) and
[Install and launch](docs/site/src/content/spec/launch.html), then continue with
[Connections](docs/site/src/content/spec/connections.html),
[Files](docs/site/src/content/spec/files.html), and
[Commands and scripts](docs/site/src/content/spec/runs.html).
The generated [API reference](https://bochner.github.io/burrow/api/) documents
the CLI operation contract; `burrow capabilities` prints the same inventory.
Contributors should read the
[Development Guide](docs/site/src/content/spec/development-guide.html).
The source for the book lives under [`docs/site/src/content/`](docs/site/src/content/).

The [agent workflows](docs/site/src/content/spec/agent-skills.html) include
Burrow skills for Claude Code, Codex, and OpenCode:

```sh
burrow agent install claude --scope user
burrow agent install codex --scope project
burrow agent install opencode --scope user
```

## Install

The public [GitHub releases](https://github.com/Bochner/burrow/releases/latest)
provide a Linux amd64 archive, a pipx wheel, and SHA256 checksums. The wheel
contains the Go executable; Python 3.10+, pipx, and OpenSSH client tools are
required. Install the current release with:

```sh
pipx install https://github.com/Bochner/burrow/releases/download/v0.2.2/burrow_ssh-0.2.2-py3-none-manylinux_2_28_x86_64.whl
burrow --workspace "$HOME/burrow-lab"
```

First launch downloads and verifies the pinned Hovel runtime. No separate Go or
Hovel installation is needed. For archive installation, checksum verification,
offline use, and upgrades, see the
[launch guide](docs/site/src/content/spec/launch.html).
PyPI publication is pending; use the GitHub release assets.

## Develop

Aspect CLI is the single entry point for building, testing, linting, formatting,
release artifacts, and local runs.

```sh
aspect help
aspect burrow run -- --workspace "$HOME/burrow-dev"
aspect burrow-check ci
```

Useful commands:

| Command | Description |
| --- | --- |
| `aspect burrow-check ci` | Build packages and run portable checks without Docker. |
| `aspect burrow-check preflight` | Run the complete uncached gate, including SSH and Hovel compatibility checks. Requires Docker and OpenSSH tools. |
| `aspect burrow package` | Build the Linux amd64 application archive. |
| `aspect burrow-release bundle` | Build and smoke-test the archive and wheel in `dist/`. |
| `aspect burrow-site check` | Validate the documentation, links, search, and report UI structure. |
| `aspect burrow-site stage` | Materialize the documentation under `_site/`. |

See the [Development Guide](docs/site/src/content/spec/development-guide.html)
for CI, formatting, and local shortcuts, and
[Release preparation](docs/site/src/content/spec/releasing.html) for publishing.

## Repository layout

| Path | Purpose |
| --- | --- |
| `core/` | Go application, Hovel integration, behavior checks, and release tooling. |
| `docs/` | Pages book, report tooling, research, and development conventions. |
| `agent/` | Canonical Burrow agent skills and packaging tools. |
| `.aspect/` | Repository workflows backed by declared Bazel targets. |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Changes should pass the relevant Aspect
checks; `aspect burrow-check preflight` runs the complete gate.

## License

Burrow has no project-wide distribution license selected yet. Adapted Hovel
material retains its [Apache-2.0 license](docs/site/public/LICENSE-HOVEL) and
[provenance/attribution](docs/site/UPSTREAM.md).
