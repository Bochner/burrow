# Reusing the preserved prototypes

The prototypes remain useful evidence and starting points for implementation.
Their complete source, dependency locks, provenance and runnable checks are kept
under archive tags. Main retains the research, decisions, handoff documents and
maintained repository tooling. Generated output and local runtime state are ignored.

| Snapshot | Reuse it for | Canonical decision |
| --- | --- | --- |
| [External SDK](https://github.com/Bochner/burrow/tree/archive/prototype-external-sdk-lifecycle) (`851cadd`) | Unchanged Hovel SDK source with a pinned BUILD overlay, package manifest and framed protocol check. | [Can the external SDK package and session lifecycle be demonstrated?](https://github.com/Bochner/burrow/issues/6) |
| [Terminal placement](https://github.com/Bochner/burrow/tree/archive/prototype-terminal-placement) (`7fd8f31`) | Real Linux PTY input, geometry observations, local frontend restoration and session lifecycle fixture. | [Where should the terminal UI run?](https://github.com/Bochner/burrow/issues/8) |
| [Hovel setup](https://github.com/Bochner/burrow/tree/archive/prototype-hovel-setup) (`70368aa`) | Latest combined fixture, including verified published Hovel acquisition and actual daemon configuration/access checks. | [Can Hovel setup, daemon attachment, and persistent quit behavior be demonstrated?](https://github.com/Bochner/burrow/issues/18) |

Start with the setup snapshot: it includes the SDK and terminal fixtures, so
there is no need to combine all three histories. The earlier tags preserve the
evidence used for their individual decisions.

## Run or extend a proof

From a clean repository checkout, create a disposable worktree:

```sh
git fetch origin --tags
git worktree add --detach ../burrow-proof archive/prototype-hovel-setup
cd ../burrow-proof
aspect burrow-prototype setup
aspect burrow-check
```

The setup check requires Linux amd64 and downloads declared, pinned dependencies.
It runs inert fixtures in temporary workspaces and cleans up its processes.
Read `core/prototype_sdk/SETUP.md` and `README.md` in that snapshot for the exact
pins and measured limits. The snapshot's `burrow-check` includes SDK checks;
main's gate checks maintained metadata and documentation tooling.

Archived prose describes the decision state when captured. For current scope,
follow the decision links above and the
[deferred compatibility handoff](hovel-daemon-compatibility-handoff.md).
The frontend is a throwaway Python probe, not the chosen production language;
the setup probe is not a production installer. Passing assertions about known
upstream limitations do not establish full SSH parity.

When implementing an approved capability, bring forward the specific validated
source, pins and behavior checks it needs. Avoid merging a whole snapshot over
main: snapshots predate later documentation, ignore rules and the repeat-staging
fix. Keep active checks in the relevant Aspect gate. Dependency locks such as
`MODULE.bazel.lock`, `pnpm-lock.yaml` and a used Go dependency manifest belong in
version control; build output, Python bytecode, credentials and installer-local
bookkeeping do not.
