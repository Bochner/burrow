# Consolidated shell observation (#98 row 20)

Owner-requested implementation of the bounded discovery/inspection follow-up in
[#98, row 20](https://github.com/Bochner/burrow/issues/98), on
`audit/general-use-architecture`, starting at `34e79fe3ad61dcccc8eb17ce82a8dfe019601e4a`.
The [#107 measurements](shell-attachments-107.md) identified repeated inspection
as a request multiplier; [#109](hidden-shell-polling-109.md) already reduced
hidden-tab cadence. This change preserves that cadence.

## Change and boundaries

The shared CLI/TUI `observe` and `snapshot` path now discovers the exact shell
and validates the shell state carried by its observation. Previously it first
requested a separate inspection of the same provider. Both observations already
contain the complete shell record under the provider's existing lock.

Discovery still freshly matches session ID, public module, shell kind and
connection name. Observation decoding now checks workspace and connection name
as well as session ID, retaining the identity checks formerly supplied by the
inspection. Every remaining RPC uses the unchanged fresh daemon/Unix-peer
validation. No identity, session or connection cache was introduced.

If observation fails, one inspection distinguishes an unavailable owner from
a rejected cursor or failed observation. It never retries the observation or
substitutes an inspection for a recovered screen. Discovery failure remains an
error; missing/replaced IDs remain unavailable. Private input/control, generation
fencing, explicit takeover, independent cursors, lifecycle approval and cleanup
are unchanged. Control commands still use their existing inspection.

## Runnable checks and call counts

`aspect burrow sessions-check` exercises the real public CLI/daemon/provider
boundary. New assertions require one discovery and one observation command for
each healthy `observe`/`snapshot`, and validate returned connection identity.
They reject wrong connection names, a manager ID, missing/closed IDs and invalid
cursors; both observation routes now exercise module/master/manager/daemon loss
and foreign-workspace refusal alongside the existing controller tests.

The new regression failed before implementation: `observe` performed one
`ListSessions`, two `RunSessionCommand` and seven `GetDaemonInfo` calls.
After implementation both routes performed one, one and five respectively:
**10 → 7 total RPCs per fresh CLI observation**. One daemon-info call belongs to
CLI startup. Within the shared observation itself the reduction is **9 → 6**:
one owner inspection and its two fresh identity checks disappear. The two checks
on each remaining public call are retained. These are RPC counts, not measured
latency or CPU savings.

The [focused lifecycle lab](shell-discovery-row20/burrow-row20-green.log) passed after implementation, including independent
readers, concurrent takeover, stale authority, geometry, bounded output, screen
recovery, backpressure, close/input races, selected/sibling cleanup and loss.
Go format, Python format, frontend interaction and terminal host checks passed;
their logs and the initial red result are retained beside this report.

## TUI measurement and limits

Reproduce the bounded diagnostic through the existing lab:

```sh
rtk proxy mkdir -p /tmp/burrow-row20-after
rtk proxy env TEST_UNDECLARED_OUTPUTS_DIR=/tmp/burrow-row20-after \
  aspect burrow attachment-cost -- --phases --samples 1
```

One five-second sample per case covers 1/2/4 retained attachments, idle/output,
and visible/hidden/background-workspace placement. The raw report records pins,
source/binary hashes, per-case RPC and payload counts, CPU ticks, and failures.
Counts include ordinary inventory work; command counts are subcategories of RPC
counts. This small run checks that consolidation reaches the real TUI; it is
not a latency benchmark, CPU attribution or population-tail estimate.

The initial measurement stopped because its older harness required an `inspect`
call in every window. Its first window correctly contained no such call.
That obsolete requirement was removed; fresh identity, discovery, snapshot and
retained-shell behavior assertions remain. This failed probe is excluded from
successful measurements.

The [completed raw report](shell-discovery-row20/after.json) contains **18
successful samples, zero failures/timeouts, and zero `command:inspect` calls
in every sample**. Visible snapshot rates were 19.97–20.00/s with one attachment,
20.99–21.08/s with two, and 22.94–22.99/s with four (one visible, others hidden).
Hidden and background-workspace cases remained approximately 1/2/4 snapshots/s.
Stable control generations, return-to-tab input/output, release, retained lifetime
and final cleanup passed. No other builds or gates ran during these windows.

## Final verification

`aspect burrow-check preflight` passed **45/45 targets** in **9m 46s**, uncached
and without failed-test retries. It built production/proof packages and ran the
portable checks, format checks, documentation checks, all nine SSH partitions,
and three attempts each of setup/terminal checks. Report collection succeeded.
The [gate log](shell-discovery-row20/burrow-row20-preflight.log),
[suite metadata](shell-discovery-row20/preflight.json) and
[full shell/TUI log](shell-discovery-row20/shared-shell.log) retain the evidence.
This final run includes all added wrong-target, closed-ID and loss assertions.

The shared-TUI checks also passed hidden-mode freshness, takeover in both
directions, independent observer geometry, gaps/scrollback, detach/reattach,
replacement, secret exclusion and terminal restoration. The composite switch
and command during 4,000 background lines took 0.767 s; it is not a new per-key
latency comparison. No disposable lab containers remained after verification.

## Standards review

Zero findings. The diff preserves public Hovel routing, identity validation,
control fencing and loss reporting, and reuses existing acceptance tracing.
No actionable baseline smells were identified.

## Spec review

Zero findings. Healthy observations eliminate the separate inspection while
retaining discovery and fresh daemon/peer checks. Failure handling never presents
inspection data as a screen. Independent evidence review recomputed the CLI
counts and all 18 TUI cases; no latency or CPU saving is inferred.

Review totals: Standards 0; Spec 0.

The exact three-diagnostic #86 release exception and deferred #65 owner
walkthrough remain unchanged. Advisory diagnostics were not rerun for this
change; a required-preflight pass does not claim the strict gate or upstream WAL
issue is fixed. Row 21 (bounded screen-change waiting) remains a
separate conditional follow-up requiring its own wakeup, cancellation, authority
and lock-safety proof; this work does not start it.
