# Burrow

![Burrow: a warm underground terminal hideaway surrounded by cyan and magenta circuitry.](docs/assets/burrow.png)

A planned Hovel-native SSH manager in Go: LazySSH's workflows, a modern Charm
terminal interface, and room for SMB and WinRM later.

**Status: research and repository bootstrap. No SSH executable exists yet.**

## Start here

1. Read the [Wayfinder starting brief](docs/wayfinder-start.md).
2. Install Matt Pocock's skills in this project as described in his
   [official repository](https://github.com/mattpocock/skills):

   ```sh
   npx skills@latest add mattpocock/skills
   ```

   Include `setup-matt-pocock-skills`, `wayfinder`, `research`, `grilling`,
   `domain-modeling`, and `prototype`. Installation is left to the owner.
3. Run `/setup-matt-pocock-skills` to review the existing GitHub tracker and
   single-context configuration, then `/wayfinder` with the starting brief.

The first session should turn the research into a shared decision map, followed
by an implementation-ready SSH parity specification.

## Research

- [Hovel integration and build conventions](docs/research/hovel-integration.md)
- [LazySSH feature parity and migration](docs/research/lazyssh-parity.md)
- [Charm, Go SSH, and future SMB/WinRM options](docs/research/libraries.md)
- [Source revisions and bootstrap conventions](docs/research/sources.md)

## Development

Use [Aspect CLI](https://github.com/aspect-build/aspect-cli) as Hovel does.
The repo pins Aspect `2026.33.3` and Bazel `9.1.1` to the inspected Hovel revision.

```sh
aspect help
aspect build //:research
```

The current target collects the research documents and checks that the Bazel
workspace loads. It does not test application functionality or validate prose.
CI runs the same command. Go/SDK dependencies and executable targets will be
added after the external SDK build approach is resolved. No Hovel remote cache,
credentials, or release infrastructure is inherited.

Use `AGENTS.md` for contributor conventions. Keep upstream reference checkouts
under ignored `.references/`; pin their revisions in the research source index.

## Upstream

[Hovel](https://github.com/vibepwners/hovel) is the integration reference;
[LazySSH](https://github.com/Bochner/lazyssh) is the behavior reference.
Upstream license obligations must accompany any future copied source. Burrow
has no public distribution license selected during this private bootstrap.
