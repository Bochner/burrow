# First Wayfinder session

This is an input brief, not a published Wayfinder map or an accepted architecture.
The owner will install Matt Pocock's skills locally. His current workflow calls
for a live discussion to name the destination, then decision tickets on the
tracker, with research conducted in parallel. This bootstrap supplies evidence
for that discussion without inventing the owner's answers.

## Proposed destination

An implementation-ready specification for replacing LazySSH's useful SSH
functionality with Burrow in Go, integrated through Hovel's supported extension
contracts, with a modern Charm terminal experience and Hovel-aligned builds.

Success means that the Hovel integration boundary, SSH transport strategy,
feature-parity acceptance criteria, session/credential ownership, UI placement,
and first implementation slices are decided. The destination is a specification;
shipping the full rewrite is the subsequent implementation effort.

## Standing preferences from the owner

- Private repository under Bochner.
- Homelab management and authorized penetration-testing engagements.
- Full useful LazySSH SSH functionality, not just a connection picker.
- Modern terminal display, including progress indicators and useful panels.
- Match Hovel conventions as it grows: public SDK/protocol, Aspect and Bazel.
- Brief SMB/WinRM research and placeholders now; implementation later.

## Initial evidence

- [Hovel integration](research/hovel-integration.md): native modules are SDK
  subprocesses with installable packages. Separate repository and native module
  are compatible choices. Module stdout cannot host a TUI. External SDK builds,
  PTY resizing, cancellation, and SSH credential mapping need spikes.
- [LazySSH parity](research/lazyssh-parity.md): source-backed inventory of the
  commands and behavior to replace, including OpenSSH socket-based plugins.
- [Library choices](research/libraries.md): Charm component/version choices,
  native Go SSH and OpenSSH interoperability, and Windows protocol candidates.

These findings are research assets; the future map should link them, not copy
them into multiple ticket bodies or claim that their recommendations are final.

## Candidate decision tickets

These questions are precise enough to discuss as tickets. Publish and assign
them during the first Wayfinder session after confirming its destination.

| Question | Type | Dependency / evidence needed |
| --- | --- | --- |
| Can an external Burrow Bazel workspace build and install a pinned Hovel Go SDK module? | Research, then prototype if needed | Verify the SDK distribution and smallest working package; independent first step. |
| Which LazySSH workflows and plugin contracts define required parity? | Grilling | Review the source inventory with the owner, including migration and known bugs. |
| Should SSH use Go transports, OpenSSH control masters, or Go channels over a control master? | Research + grilling | Parity decisions, particularly OpenSSH config/auth and plugin socket compatibility. |
| Where does Burrow's TUI run, and must it work without a Hovel daemon? | Grilling + prototype | Module lifecycle findings, operator OS requirements, PTY/resize proof. |
| Who owns connections, tunnels, secrets, and engagement state across detach/restart? | Grilling | Hovel ownership and transport choice; inspect actual credential persistence. |
| Which Charm components and interaction model fit those boundaries? | Prototype | TUI placement, Charm major version choice, concrete operator reactions. |
| What is the first tested SSH slice and subsequent parity sequence? | Grilling | Resolved architecture/transport/lifecycle decisions. |

Split combined types into separate decision tickets when charting. Use native
GitHub sub-issues and blocking relationships as described in
[the tracker conventions](agents/issue-tracker.md). Research can run in parallel;
human decisions require the owner's live input.

## Questions to bring to the live discussion

1. Which operator platforms must work initially: Linux, macOS, Windows?
2. Must Burrow run independently for homelab use, or is starting Hovel acceptable?
3. Which existing LazySSH plugins must run unchanged, including their control
   socket access? Is behavior parity enough, or is command syntax also required?
4. Which authentication paths matter first: passwords, encrypted keys, agents,
   SSH certificates, hardware-backed keys, keyboard-interactive, jump hosts?
5. Should active connections survive quitting/restarting the UI? What separates
   homelab state from engagement state, and what history may be retained?

## Not yet specified

Packaging/distribution matrix beyond the first supported operator platform;
exact migration UX; large-connection-count navigation; reconnect semantics;
which SSH forwarding capabilities should be exposed as Hovel Mesh operations.
Turn these into precise questions when the upstream decisions make them clear.

## Future protocol placeholders

### SMB

No adapter or dependency yet. Start from the [library research](research/libraries.md)
and Slinger comparison. Before implementation, define whether SMB means share
discovery/file I/O, named-pipe transport, or remote execution: those are different
capabilities and tests. Reuse Hovel's existing SMB work where its contract fits.

### WinRM

No adapter or dependency yet. Start from the [library research](research/libraries.md)
and Evil-WinRM comparison. Decide whether basic WS-Man command execution is
enough or PowerShell Remoting Protocol behavior is required, then verify the
needed authentication and file-transfer workflows against a controlled lab.

## Out of scope for this first map

Implementing SMB/WinRM, reproducing every capability of Slinger/Evil-WinRM,
modifying Hovel core without a demonstrated API gap, and shipping the complete
SSH rewrite during a research bootstrap. These are not unresolved decisions
blocking the first SSH specification.

## Suggested invocation

After installation and `/setup-matt-pocock-skills`:

> /wayfinder Read CONTEXT.md, docs/wayfinder-start.md, and the linked research.
> Help me chart a decision map for an implementation-ready SSH parity spec.
> Confirm the destination and map the open decisions with me before publishing
> the map to Bochner/burrow. Use the existing research as evidence, verify any
> upstream changes, and keep SMB/WinRM implementation outside this map.

Source: [Wayfinder at the inspected revision](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/skills/engineering/wayfinder/SKILL.md).
