# Burrow context

Burrow is the Go successor to Bochner's LazySSH, for homelab SSH management
and authorized penetration-testing engagements. It fits Hovel's extension and
development conventions as Hovel evolves. MVP 1 (setup, profiles and
connections), MVP 2 (shells and forwarding), and MVP 3 (file workflows)
are merged. MVP 4 (scripts, reports and chains) is implemented; MVP 5 remains
follow-on work. The owner approved releasing MVP 4 with Hovel compatibility
issue [#86](https://github.com/Bochner/burrow/issues/86) as a known limitation:
three upstream diagnostics are advisory, while all production acceptance,
normal startup and documentation checks remain required. Merge remains
owner-controlled.

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
| Connection | An authenticated SSH transport to a host; can exist without an interactive shell and support shells, transfers, or tunnels on demand. |
| Master socket | The local control endpoint through which operations reuse an established SSH connection. Its existence does not imply an open interactive shell. |
| Interactive shell | An on-demand terminal channel using a connection; closing it does not close the connection. Current production shells are frontend-local; the accepted [MVP 5 shared-session decision](docs/adr/0002-shared-interactive-sessions.md) retains them under Hovel. |
| Background shell | An interactive shell that keeps running while Burrow displays management or another shell. The accepted MVP 5 policy also retains it after frontend detach/Keep running; this does not promise survival after daemon or owning-module loss. |
| Controller | The single human or agent attachment authorized to send a shared shell input and change its terminal dimensions; takeover explicitly replaces that authority. |
| Observer | An attachment with an independent output position that cannot send shell input or change terminal dimensions. A missed-output gap makes its view out of sync until resynchronized. |
| Session | An interactive channel exposed to an operator; distinguish Burrow SSH channels from Hovel's session records. |
| Script run | A noninteractive command or script execution whose lifetime is independent of a viewer; local-tool results and remote-command results are distinct. |
| Collection | Explicitly register a run's output as workspace evidence; viewing live output alone is not collection. |
| Detach | Leave an operator frontend or attachment while daemon-owned resources remain available. Burrow quit reviews connections in opened workspaces: the operator can keep them running, explicitly close them, or cancel. Current frontend-local shells end on exit; accepted MVP 5 shared-shell detach releases control and retains the shell. |
| Close | Explicitly end a live resource, distinct from detaching an operator. Closing a connection ends all its shells, transfers and tunnels and removes its master socket; saved settings and evidence remain. |
| Tunnel | Local, remote, or dynamic forwarding owned by a connection. Initially, the operator establishes it in Burrow before a Hovel chain can select and use it. |
| Saved connection | Non-secret connection settings that can recreate a connection; not a live transport. |
| Hovel module | A separately launched program packaged for Hovel and speaking its module protocol through the SDK. |
| LazySSH plugin | Existing Python/shell automation consuming LazySSH environment variables and OpenSSH control sockets; not a Hovel module. |
| Standalone use | Launching Burrow without having to operate Hovel separately; a transparently installed and managed Hovel dependency is acceptable. |
| Workspace | Hovel's daemon-owned operational state boundary, separating homelab or engagement records and artifacts. Connection names are local to a workspace; a workspace is not a security boundary against processes running as the same user. |

See `docs/wayfinder-start.md` for the proposed destination and initial questions.
