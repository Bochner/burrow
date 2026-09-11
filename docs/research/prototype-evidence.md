# Reusing the preserved prototypes

The prototypes remain useful evidence and starting points for implementation.
Their complete source, dependency locks, provenance and runnable checks are kept
under archive tags or immutable commit links. Main retains the research, decisions, handoff documents and
maintained repository tooling. Generated output and local runtime state are ignored.

| Snapshot | Reuse it for | Canonical decision |
| --- | --- | --- |
| [External SDK](https://github.com/Bochner/burrow/tree/archive/prototype-external-sdk-lifecycle) (`851cadd`) | Unchanged Hovel SDK source with a pinned BUILD overlay, package manifest and framed protocol check. | [Can the external SDK package and session lifecycle be demonstrated?](https://github.com/Bochner/burrow/issues/6) |
| [Terminal placement](https://github.com/Bochner/burrow/tree/archive/prototype-terminal-placement) (`7fd8f31`) | Real Linux PTY input, geometry observations, local frontend restoration and session lifecycle fixture. | [Where should the terminal UI run?](https://github.com/Bochner/burrow/issues/8) |
| [Hovel setup](https://github.com/Bochner/burrow/tree/archive/prototype-hovel-setup) (`70368aa`) | Latest combined fixture, including verified published Hovel acquisition and actual daemon configuration/access checks. | [Can Hovel setup, daemon attachment, and persistent quit behavior be demonstrated?](https://github.com/Bochner/burrow/issues/18) |
| [SSH transport](https://github.com/Bochner/burrow/tree/c1bbbf55a4d18cec6bff808a6fe37e2a44fa33ba/core/prototype_transport) (`c1bbbf5`) | OpenSSH/Go bridge failure reproductions, ordinary subprocess SFTP/forwarding comparison, SSH PTY and Hovel session checks, and optional VM check. | [Can OpenSSH master sockets and Go channels satisfy the transport contract?](https://github.com/Bochner/burrow/issues/19#issuecomment-5639082720) |
| [Connection ownership](https://github.com/Bochner/burrow/blob/94e46e1/core/prototype_transport/OWNERSHIP.md) (`94e46e1`) | Daemon-owned master cleanup, local terminal handoff/resize, workspace separation, log-limit reproduction and bounded workaround. Full-screen redraw remains unproven; owner review pending. | [Can connection ownership and complete teardown be proven through Hovel?](https://github.com/Bochner/burrow/issues/25) |
| [Script execution boundary](https://github.com/Bochner/burrow/blob/ad04fce/core/prototype_transport/SCRIPT_BOUNDARY.md) (`ad04fce`) | Confirmed streamed execution, bounded binary output, missing-master refusal, and reproductions of lost completion/evidence after caller disconnect and direct-API confirmation differences. Full script matrix and workaround remain pending. | [Can remote script runs satisfy Hovel execution and cleanup contracts?](https://github.com/Bochner/burrow/issues/26) |

Start with the setup snapshot: it includes the SDK and terminal fixtures, so
there is no need to combine all three histories. The earlier tags preserve the
evidence used for their individual decisions.

For transport work, use the SSH transport snapshot. Run
`aspect burrow-prototype transport` or its expanded `aspect burrow-check`.
Read `core/prototype_transport/README.md` first: it requires explicitly hashed
host OpenSSH binaries and asserts known bridge defects. A passing observation
check is not full transport acceptance. The owner subsequently accepted ordinary
OpenSSH subprocesses with Go SFTP over subsystem pipes; the archived README's
pending-decision language predates the linked resolution. The optional VM check
reproduced the defects with a separate server, while the client remained on WSL2.

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
