# Burrow context

Burrow is the planned Go successor to Bochner's LazySSH, for homelab SSH
management and authorized penetration-testing engagements. It should fit
Hovel's extension and development conventions as Hovel evolves.

## Established scope

- The repository is private under `Bochner/burrow`.
- SSH comes first: reach full useful LazySSH workflow parity, using a modern
  Charm-based terminal experience and Go implementation.
- Follow Hovel's Aspect/Bazel build approach and public integration contracts.
- Investigate SMB and WinRM now only enough to leave informed future work.
- The [Wayfinder map](https://github.com/Bochner/burrow/issues/1) tracks the
  implementation-ready specification. Architecture, UX, transport, and
  implementation milestones remain decisions; initial research is resolved.

## Vocabulary

| Term | Meaning here |
| --- | --- |
| Connection | An authenticated SSH transport to a host; may support several shells, transfers, or tunnels. |
| Session | An interactive channel exposed to an operator; distinguish Burrow SSH channels from Hovel's session records. |
| Tunnel | Local, remote, or dynamic forwarding owned by a connection. |
| Saved connection | Non-secret connection settings that can recreate a connection; not a live transport. |
| Hovel module | A separately launched program packaged for Hovel and speaking its module protocol through the SDK. |
| LazySSH plugin | Existing Python/shell automation consuming LazySSH environment variables and OpenSSH control sockets; not a Hovel module. |
| Workspace | Hovel's daemon-owned operational state boundary; how homelab and engagement data map to it remains to be decided. |

See `docs/wayfinder-start.md` for the proposed destination and initial questions.
