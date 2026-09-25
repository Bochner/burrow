---
name: burrow-inspect
description: Inspect a selected Burrow SSH host for files, disk use, processes, ports, services, logs or reachability, and explain results with their real exit status and evidence.
compatibility: Requires the Burrow Linux CLI and an existing live SSH connection; target utilities vary by host.
metadata:
  burrow-skill-version: "0.2.0"
  burrow-cli-contract: "1"
---

# Burrow inspection

Load `burrow` first and follow its selection and operating rules. Load
`burrow-run` for the shared prepare/review/launch/output/collection sequence.
Ordinary inspection uses remote commands on the selected existing connection;
it needs no separate diagnostic service.

Read [commands.md](references/commands.md) only for the requested diagnostic.
Prepare those arguments with `run prepare NAME -- COMMAND ARG...`, check the
exact target and command recap, then use the retained run's review digest.
Existing explicit intent can authorize the requested reads. Broad scans need
an explicit root and scope recap. Missing tools and denied permissions are
observations, not authorization to install packages or escalate privileges.

Follow `burrow-run` through actual exit/capture status and evidence collection.
Report unreadable paths as partial results even when output capture is complete.
Finish with the workspace/connection, observations, actual exit/capture status,
evidence identities (or explicitly not collected), hypotheses and the narrow
next check. Do not turn a diagnostic suspicion into a repair.
