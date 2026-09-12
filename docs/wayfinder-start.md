# First Wayfinder session

This is the input brief for the [published Wayfinder map](https://github.com/Bochner/burrow/issues/1),
not an accepted architecture. The tracker is canonical for decisions.
Matt Pocock's skills are installed locally. His current workflow calls
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

- AI support and a Burrow-owned skill set following Hovel's conventions are critical
  requirements, added on 2026-09-11. The
  [AI and skills decision](https://github.com/Bochner/burrow/issues/33) investigates
  the workflows, agent interfaces and Hovel-aligned packaging/install/update
  contract before final slice planning. The owner subsequently selected external
  agents driven by CLI-oriented skills, with MCP optional: mirror Hovel's skill
  layout and installation conventions without requiring its MCP integration.
  The owner subsequently selected direct skill-directory installation and updates,
  preserving user edits; plugin packaging/distribution and native plugin-install
  checks are deferred. Native standalone-skill discovery remains an opt-in check.
- The [transport proof decision](https://github.com/Bochner/burrow/issues/19#issuecomment-5639082720)
  selects ordinary OpenSSH subprocesses over a shared, user-named master, with
  structured Go SFTP over subsystem pipes. It supersedes the initial Go
  control-proxy preference. The [terminal resize decision](https://github.com/Bochner/burrow/issues/24)
  selects on-demand Burrow-local shells through that master initially; Hovel-mediated
  resize and retained shells follow later. See the [preserved proof](research/prototype-evidence.md).
- Hovel integration is the top priority, confirmed on 2026-09-11. Resolve its
  supported lifecycle, logging, state ownership, and UI boundaries before
  selecting Charm dependencies or designing standalone behavior. Standalone
  SSH management should feel standalone to the operator; the owner clarified
  that transparently installing and managing Hovel is acceptable. Prefer one
  daemon-backed design over separate runtime/build variants. The owner subsequently
  limited initial support to Burrow-started, pinned Hovel instances; general
  existing local/remote daemon attachment is deferred. See the
  [setup feasibility decision](https://github.com/Bochner/burrow/issues/18) and
  [proposed upstream compatibility convention](research/hovel-daemon-compatibility-handoff.md).
- Initial authentication/configuration baseline: passwords, encrypted keys,
  SSH agents, host-key verification, SSH config aliases, and jump hosts.
  Inspect Hovel for additional supported capabilities before deciding whether
  certificates, hardware-backed keys, or keyboard-interactive/MFA need scope.
- Unchanged LazySSH plugins and their legacy API are not required. Map user
  scripting as three separate questions: remote script execution, local scripts
  invoking Hovel operations, and local tools using SSH tunnels. Prefer Hovel's
  existing execution and Mesh contracts where they fit.
- The [setup and workspace decision](https://github.com/Bochner/burrow/issues/9#issuecomment-5636553707)
  supersedes the initial standalone close-on-exit preference: normal quit detaches
  and retains the daemon and daemon-owned resources. The
  [terminal resize decision](https://github.com/Bochner/burrow/issues/24) makes
  frontend-local interactive shells an explicit exception: those end on Burrow
  exit, while the background connection and tunnels remain available. The
  [ownership decision](https://github.com/Bochner/burrow/issues/11) requires
  explicit reconnect after restart or loss; loading saved settings never connects.
- Initial operator platform: Linux, approved on 2026-09-11.
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

## Original candidate decision tickets

These questions seeded the published map. Query the map's child issues for
current ticket scope, ownership, resolutions, and blocking relationships.

| Question | Type | Dependency / evidence needed |
| --- | --- | --- |
| Can an external Burrow Bazel workspace build and install a pinned Hovel Go SDK module? | Research, then prototype if needed | Verify the SDK distribution and smallest working package; independent first step. |
| Which LazySSH SSH workflows define required parity? | Grilling | Review the source inventory with the owner, including migration and known bugs; unchanged legacy plugins are excluded. |
| Should SSH use Go transports, OpenSSH control masters, or Go channels over a control master? | Research + grilling | Parity decisions, particularly the accepted OpenSSH configuration/authentication baseline. |
| How should users execute scripts on an SSH target through Burrow and Hovel? | Grilling | Hovel execution, confirmation, output, and audit contracts. |
| How should local user scripts invoke Hovel operations? | Grilling | Supported Hovel automation interfaces and credential boundaries. |
| How should local tools use tunnels through Burrow SSH connections? | Grilling | Hovel Mesh contracts and connection/listener ownership. |
| Where does Burrow's TUI run, and must it work without a Hovel daemon? | Grilling + prototype | Module lifecycle findings, operator OS requirements, PTY/resize proof. |
| Who owns connections, tunnels, secrets, and engagement state across detach/restart? | Grilling | Hovel ownership and transport choice; inspect actual credential persistence. |
| How do diagnostics, progress, and audit events reach Hovel and the operator UI? | Research | Trace SDK logging through the daemon; verify correlation, persistence, redaction, and terminal-safe rendering before considering another logger. |
| Which Charm components and interaction model fit those boundaries? | Prototype | TUI placement, Charm major version choice, concrete operator reactions. |
| What is the first tested SSH slice and subsequent parity sequence? | Grilling | Resolved architecture/transport/lifecycle decisions. |

Split combined types into separate decision tickets when charting. Use native
GitHub sub-issues and blocking relationships as described in
[the tracker conventions](agents/issue-tracker.md). Research can run in parallel;
human decisions require the owner's live input.

## Original discussion prompts

1. Which operator platforms must work initially: Linux, macOS, Windows?
2. Must Burrow run independently for homelab use, or is starting Hovel acceptable?
3. Which script invocation, output, cancellation, and routing behaviors are
   required for the three scripting questions above?
4. Does Hovel offer authentication capabilities beyond the accepted baseline
   that Burrow should expose initially?
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

## Continue the map

The first map is published. Continue with:

> /wayfinder https://github.com/Bochner/burrow/issues/1
> Pick the next available decision, read its linked research, and work through
> it with me. Keep SMB/WinRM implementation outside this map.

Source: [Wayfinder at the inspected revision](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/skills/engineering/wayfinder/SKILL.md).
