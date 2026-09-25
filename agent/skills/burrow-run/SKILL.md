---
name: burrow-run
description: Execute Burrow commands, scripts or local tools, track actual results, collect evidence and read reports.
compatibility: Requires the Burrow Linux CLI and an explicitly selected workspace and connection.
metadata:
  burrow-skill-version: "0.2.0"
  burrow-cli-contract: "1"
---

# Execution and evidence

Load `burrow` and its operating rules first. Discover `run.prepare`, `run.launch`,
`run.inspect`, `run.output` and `run.collect`. Use the selected PATH and NAME;
RUN and HASH below come from results, never from guessed IDs.

```sh
burrow --workspace PATH run prepare NAME -- df -h
burrow --workspace PATH run launch RUN
burrow --workspace PATH run launch RUN --review HASH --yes
burrow --workspace PATH run inspect RUN
burrow --workspace PATH run output RUN stdout 0
burrow --workspace PATH run output RUN stderr 0
```

Preparation returns retained `id` and `connection`. Verify the target and
`execution=remote`. Launch without approval returns the exact `review` and
`digest`; apply them only for the authorized action. Arguments after `--` are
quoted individually on the target. Shell syntax requires explicit `/bin/sh -c`
and safely quoted data, not concatenated remote filenames.

Launch success is not command completion. Poll `run inspect`; CLI exit 0 only
means the request succeeded. Report `remoteExit` for remote execution and
`localExit` for local tools. Each output `data` is base64 bytes; decode as data
and advance stdout and stderr independently to `nextOffset`, through their final
`storedBytes`. An empty chunk can mean no new output yet. Check final state,
`outputComplete`, `outputError`, `auditError`, timeout and cleanup uncertainty.
A permission error/nonzero exit can make results partial despite complete capture.
Use defaults unless the task needs a different timeout or capture budget.

For scripts or daemon-host tools, read [script and automation modes](references/scripts.md).
For an independent human viewer, use the TUI's run tab/`run follow RUN`, or the
workspace `follow` command. Discover their routes first; viewing never collects
or cancels. A lost response needs inspection of RUN before retrying. `run now`
already prepares a retained run: approve its returned launch command instead of
preparing a second run by repeating `run now`.

When evidence is requested, review `run collect RUN`, then apply
`run collect RUN --review HASH --yes`. Alternatively review
`run launch RUN --collect` before approving that distinct digest. Check
`collection=succeeded`; report retained `id`, `launchRunID` and collection
`runID`. The collection's Hovel runID differs from the preparation/launch runID.
Keep that response: later inspect/list do not retain its collection field/runID.
Discover artifacts through the existing Hovel integration's artifact list/read
routes and correlate that collection runID; Burrow has no generic artifact CLI.
Partial output can be valid collected evidence without a successful command.

For structured Ubuntu inventory, discover `report.survey`, `report.list` and
`report.read`. `run survey NAME --os ubuntu` prepares its fixed preset; review
and approve the returned launch-with-collection command. Use `reports`, then
`report ID` for original Markdown, verified metadata and capture limits. The
human's Reports tab reads the same artifacts. Do not run the Ubuntu preset on
an unverified OS or silently install missing target tools.

On explicit cancellation, review `run cancel RUN` and apply its digest. Ordinary
process-group cleanup cannot prove escaped descendants stopped. Keep null exit,
unknown remote outcome and failed cleanup visible. `run close RUN` also needs
review: it discards working output from a stopped run; collect requested evidence
first. Closing a viewer or leaving the frontend retains execution.
