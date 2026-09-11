# Research sources and bootstrap provenance

Inspected on 2026-09-11. Source snapshots were cloned and read; no upstream
application code is vendored into Burrow during this bootstrap.

| Repository | Inspected commit | Purpose |
| --- | --- | --- |
| [vibepwners/hovel](https://github.com/vibepwners/hovel/tree/c461ba282a8aecc7aa3a079a4613bf5e2640c388) | `c461ba282a8aecc7aa3a079a4613bf5e2640c388` | SDK, module/session/daemon contracts, dependencies and build conventions. |
| [Bochner/lazyssh](https://github.com/Bochner/lazyssh/tree/9eb84452c31cb527bf8e938e23ffc92974fb91cb) | `9eb84452c31cb527bf8e938e23ffc92974fb91cb` | Behavior, docs, tests and migration requirements. |
| [mattpocock/skills](https://github.com/mattpocock/skills/tree/3cca18b368ae95cdbdebbff572ccafa662551015) | `3cca18b368ae95cdbdebbff572ccafa662551015` | Wayfinder workflow and project configuration conventions. |

The library report cites the individual first-party sources it checked. Links
to moving library documentation must be rechecked when dependencies are pinned.
Recommendations are not proof of compatibility: the listed build, terminal,
transport and authentication spikes remain to be run.

## Hovel conventions adopted now

- Aspect `2026.33.3`, from [`.aspect/version.axl`](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/.aspect/version.axl).
- Bazel `9.1.1`, from [`.bazelversion`](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/.bazelversion).
- AXL configuration, `MODULE.bazel`, `BUILD.bazel`, `.bazelrc`, and workspace-root
  `aspect run` behavior, with only the research metadata target wired initially.
- Commit-pinned GitHub checkout and Aspect setup actions from Hovel's
  [CI workflow](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/.github/workflows/ci.yml)
  and [setup action](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/.github/actions/setup-hovel/action.yml).
- One entry point for builds/checks; application behavior separated from terminal
  rendering; public module/daemon boundaries; structured module logging.

Go `1.26.5`, `rules_go` `0.61.1`, and Gazelle `0.51.3` are the corresponding
upstream pins to evaluate for the first Go target. They are not unused installed
dependencies in this research workspace.

Hovel's BuildBuddy/NativeLink endpoints, release jobs, polyglot toolchains, and
daemon internals are not copied. Burrow uses no project-specific remote cache or
Aspect API credential during bootstrap.

## Source reuse

Hovel is Apache-2.0; LazySSH and Matt Pocock's skills are MIT at these revisions.
The reference checkouts retain their upstream license files. Keep required
license/copyright/NOTICE attribution with any future copied or adapted code;
research links alone are not a license notice for future vendored code.

The local `.references/` directory is ignored and will not be pushed. Research
documents and exact revisions are tracked so a fresh clone can reproduce the
inspection without relying on the original machine's checkout paths.
