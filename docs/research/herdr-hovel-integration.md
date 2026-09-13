# Herdr-inspired workspaces and an embedded Hovel CLI

Research date: 2026-09-12. Evidence and recommendations, not a claim that the
production terminal integration is implemented. The owner clarified during this
research that the Hovel tab should run Hovel's existing CLI inside Burrow.

## Recommendation

Build workspace navigation and truthful daemon status into the management
baseline now. Put Hovel's existing interactive CLI in a PTY-backed tab and reuse
that terminal host for the forthcoming SSH shells. Keep Burrow's management
tables in the Burrow tab, with navigation and metadata around the active terminal
region. Switching back restores the overview. This preserves
Hovel's own command language, completion, confirmation and throw implementation
without maintaining another Hovel operator UI.

The public process invocation is the pinned Hovel executable with arguments
`shell --workspace /canonical/workspace`; `cli` is an alias. Set the child's
explicit `HOVEL_DAEMON_ENDPOINT` to that workspace's verified `hoveld.sock`.
Calling the executable with no role prints usage; `hovel tui` explicitly reports
that it is not implemented. [Pinned root CLI][rootcli]

## Workspace identity and lifetimes

Burrow already starts one pinned Hovel daemon per selected workspace, at
`<workspace>/hoveld.sock`, and stores its verified launch receipt there. There is
not a separate Burrow daemon for a second traffic light: Burrow's retained
connection modules run under Hovel. `launch.Open` owns explicit creation/start;
`launch.Status` verifies identity without starting or replacing a daemon.
[Production launch](../../core/launch/daemon_linux.go),
[connection dispatch](../../core/connection/commands.go).

The current frontend requires an explicit `--workspace`; it has no workspace
picker or creation dialog. A sidebar therefore needs frontend navigation, not a
new daemon workspace registry. Hovel owns operation, chain, target, configuration,
log and session state inside a workspace; local frontend selections are permitted.
Start with explicitly opened canonical workspace paths and their display names.
If persistent recent paths are needed, store only navigation preferences, never
copies of authoritative resource records or secrets. There is no cross-workspace
enumeration method in the inspected daemon RPC surface.
[Current CLI](../../core/cmd/burrow/main.go), [public RPC][rpc],
[accepted workspace ticket](https://github.com/Bochner/burrow/issues/44).

Recommended switching contract: selecting a workspace changes the management
view; each live CLI/shell tab remains bound to the workspace and connection it
was created for. Preserve background reads and clearly label that identity.
Do not retarget a running PTY process or accept delayed results from the previous
workspace into the new workspace's tables. Revalidate a selected daemon before
operations; opening another workspace does not close the previous daemon or SSH
connections. Frontend-local SSH shells still end on frontend exit, as already
accepted in [the context](../../CONTEXT.md) and
[#49](https://github.com/Bochner/burrow/issues/49).

## Hovel CLI host boundary

Hovel's interactive CLI parses `--workspace`, attaches through its daemon client,
creates an interactive prompt, captures/restores terminal state and subscribes
to daemon logs. With an explicit endpoint its connector dials that endpoint;
without one it uses the managed `Ensure` path, which can start a daemon. Thus
an explicit endpoint is essential to Burrow's own-instance policy.
[CLI implementation][cli], [daemon connection implementation][connector].

Launch the verified executable directly as a child, with its stdin/stdout/stderr
and controlling terminal attached to the PTY slave. Burrow retains the physical
terminal, consumes PTY output into the VT screen, renders that screen within the
tab, and forwards selected input and terminal geometry. A subprocess that takes
over Burrow's physical terminal cannot preserve clickable tabs and sidebars.
This is a recommendation requiring a bounded compatibility check, not a claim
that the current VT proof already hosts Hovel correctly.

Reuse `launch.Status`, the verified offline package installation and the existing
environment filtering policy. The existing `launch.HovelCLI` is a useful example
but cannot be called unchanged: it hardcodes the one-shot `run` role and captures
stdout/stderr into buffers. Add the small interactive launch path when its first
caller exists; do not duplicate one-shot execution or copy Hovel internal Go
packages. [Current launch helper](../../core/launch/operations_linux.go).

Preserve Hovel's own throw review, dangerous-operation allowance and launch-key
prompts in the child terminal. Never translate a tab click into an automatic
confirmation flag. The public RPC contract explicitly requires persisted plans
and recorded confirmations and forbids parallel frontend confirmation stores.
[Public confirmation contract][rpc].

First acceptance should demonstrate prompt rendering and completion, Unicode and
paste, declined and accepted harmless throw confirmation, tab switching during
output, resize, Ctrl-C, child exit, parent quit, and missing/replaced daemon refusal.
Verify that tab close ends only that frontend child and reports any in-flight
execution uncertainty, without pretending to cancel a daemon-owned throw.
Check routing with two workspace paths, including delayed output after switching.
Use the same host for SSH in #48/#49 once this vertical slice works.

## Truthful status and useful metadata

`GetDaemonInfo` returns workspace path, PID, start time, health, access and listener
metadata. Its implementation returns stored server identity information; it is
not an end-to-end SSH or dependency diagnostic. `launch.Status` already verifies
the executable pin, launch receipt, actual socket peer and daemon identity.
[RPC implementation][rpcsource],
[Burrow status](../../core/launch/daemon_linux.go).

Recommended indicator semantics:

| Display | Evidence |
| --- | --- |
| Green, “Hovel connected” | A recent verified status request succeeded for the displayed workspace. |
| Yellow, “Checking” or “Stale” | Initial check pending, last success older than the chosen freshness interval, or retry in progress after a transient timeout. |
| Red, “Unavailable” or “Identity refused” | Observed connection failure or failed identity validation; preserve the reason and never silently start a replacement. |

Pair the color with text. Details can show workspace, PID, start time, reported
health, last successful observation and failure reason. If timing the entire
`launch.Status` call, label it “verification duration,” not network ping or daemon
RPC latency: that path also reads files, hashes an executable and inspects process
identity. Request timing and freshness are frontend measurements, not upstream
daemon statistics. Keep SSH connection/module availability separate from daemon
reachability; a green daemon cannot prove an SSH connection is alive.

For read-only metadata, the exact public methods are `GetModuleCatalog` for
modules, `Snapshot` with explicit `Operation`/`Chain` for persisted operator state,
and `ListSessions` for daemon module sessions. Frontend SSH shells come from the
frontend shell owner, not `ListSessions`. Use non-consuming log/session reads for
metadata so observing a pane cannot steal output from its owner. These APIs can
populate Burrow's metadata panel; they are not a reason to reconstruct the CLI.
[Public RPC methods][rpc], [snapshot implementation][rpcsource].

## Minimal tracker placement

The existing [MVP parent #43](https://github.com/Bochner/burrow/issues/43) sequences
setup before shells, then files and scripts. #44 is closed and covers explicit
workspace launch, not interactive multi-workspace navigation. The inspected
ticket list #44–65 contains no dedicated workspace sidebar or Hovel CLI tab slice.

Two additional MVP 1 slices are sufficient:

1. Workspace management baseline: Herdr-inspired sidebar, New/Menu, clickable
   tabs, workspace create/open/select, preserved overview/metadata and verified
   live daemon status. Validate keyboard/mouse/narrow layouts and two-workspace
   isolation with existing real connection commands.
2. Embedded Hovel CLI tab: launch the pinned interactive CLI against the selected
   verified daemon and prove the PTY/VT lifecycle and harmless reviewed throw.
   Build on the baseline; make its reusable terminal-host result a prerequisite
   for [#48](https://github.com/Bochner/burrow/issues/48) and
   [#49](https://github.com/Bochner/burrow/issues/49), keeping their SSH acceptance
   in those existing tickets.

Do not duplicate [#60](https://github.com/Bochner/burrow/issues/60)'s local
automation/result/cancellation work or [#62](https://github.com/Bochner/burrow/issues/62)'s
actual live-tunnel chain consumer acceptance. The CLI tab can precede both: an
embedded Hovel prompt does not imply those Burrow capabilities are complete.
No upstream issue is justified merely by needing a local CLI process tab.

## Source provenance

Context7 resolved `/vibepwners/hovel` and supplied current public daemon-RPC
documentation on 2026-09-12. Current indexed docs are discovery aids; Burrow's
runtime remains the v0.4.2 package and SDK source commit
`c461ba282a8aecc7aa3a079a4613bf5e2640c388` declared in
[MODULE.bazel](../../MODULE.bazel). Pinned source was inspected in the local
dependency cache. The inspected root CLI file's Git blob hash
`0f5274dbe3c3a75bbd4a3b439dd9d1f5f0e89aff` matched GitHub's contents response for
that pinned commit. Internal upstream source was read as evidence only, not
imported as an application API. No runtime behavior was changed or exercised by
this research, and no GitHub tickets were changed by this research agent.

[rootcli]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/rootcli/rootcli.go
[cli]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/cli/cli.go
[connector]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonlocal/daemonlocal.go
[rpc]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/daemon-rpc.html
[rpcsource]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonrpc/daemonrpc.go
