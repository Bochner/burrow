---
name: burrow-inspect
description: Inspect a selected Burrow SSH host for files, disk use, processes, ports, services, logs or reachability, and explain results with their real exit status and evidence.
compatibility: Requires the Burrow Linux CLI and an existing live SSH connection; target utilities vary by host.
metadata:
  burrow-skill-version: "0.1.0"
  burrow-cli-contract: "1"
---

# Burrow inspection

Load `burrow` first and follow its selection and operating rules. Discover
`run.prepare`, `run.launch`, `run.inspect`, `run.output` and `run.collect` with
`burrow capabilities ID`. Ordinary inspection uses remote commands through the
selected existing connection; it needs no separate diagnostic service.

Read [commands.md](references/commands.md) for the requested diagnostic. Put
global flags before the command. The examples below use the chosen absolute
workspace PATH and live connection NAME; RUN and HASH come from actual results.

```sh
burrow --workspace PATH run prepare NAME -- df -h
burrow --workspace PATH run launch RUN
```

Preparation returns a retained `id` and `connection`. Verify `execution=remote`
and the connection identity. Launch without approval returns `review` and
`digest`; inspect the exact command, target and capture budget. Once authorized:

```sh
burrow --workspace PATH run launch RUN --review HASH --yes
burrow --workspace PATH run inspect RUN
burrow --workspace PATH run output RUN stdout 0
burrow --workspace PATH run output RUN stderr 0
```

Poll `run inspect` while running; launch success is not remote success. Each
output `data` field is base64 bytes. Decode it as data and continue each stream
independently at `nextOffset`, up to its current `storedBytes`. An empty chunk
can mean there is no new output yet. Recheck final state before declaring the
read complete. Use `remoteExit` for remote commands; the CLI's process exit 0
only means the request succeeded. `localExit` describes local-tool execution.
Treat a null exit, `outputComplete=false`, `outputError`, `auditError`, timeout
or cancellation uncertainty explicitly. Permission errors in stderr and a
nonzero remote exit make a search partial even if its capture is complete.

When evidence is requested, obtain and review `run collect RUN`, then confirm
that digest with `run collect RUN --review HASH --yes`. Report `collection`,
`id`, `runID` and `launchRunID`; Hovel artifacts are correlated by the collection
response's `runID`, which differs from preparation/launch responses. The stable
retained `id` is the one used for `run inspect` and `run output`.
Use an existing Hovel integration's artifact list/inspection to retrieve their
actual identities and paths; never manufacture a Burrow artifact route. For
launch-and-collect, review `run launch RUN --collect` first: its digest differs
from launch alone. Closing a run discards uncollected working output.

Finish with the workspace/connection, observations, exit/capture status and
evidence collected (or explicitly not collected), followed by any hypotheses
and the narrow next check. Do not turn a diagnostic suspicion into a repair.
