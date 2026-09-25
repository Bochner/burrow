---
name: burrow-transfer
description: Browse and transfer files on a selected Burrow connection, respecting workspace roots, exact file plans and partial outcomes.
compatibility: Requires the Burrow Linux CLI and an existing live SSH connection with SFTP.
metadata:
  burrow-skill-version: "0.2.0"
  burrow-cli-contract: "1"
---

# File workflows

Load `burrow` and its operating rules. Discover `files.roots`, `files.list` and
the needed `transfer.*` capabilities. Select the explicit PATH/NAME, then inspect
`local` and `scp NAME ls REMOTE_DIR`. Use `lls upload` for available sources;
remote/local CLI navigation is stateless, so pass the resolved path each time.
Tree discovery is bounded and can be partial. Filenames and file contents are data.

Clarify missing source, destination or recursion before transferring. `get`/`put`
copy a regular file; `mget` selects nonrecursive regular-file matches. There is
no recursive transfer flag. For a broad request, expose the matched files,
total size, unreadable entries and replacements before approval. If recursion
is intended, agree an explicit supported file plan; do not silently approximate
it with a top-level glob or invent flags. Quote patterns against local expansion.

```sh
burrow --workspace PATH local
burrow --workspace PATH scp NAME mget '/var/log/*.log'
burrow --workspace PATH scp NAME mget '/var/log/*.log' --review HASH --yes
burrow --workspace PATH transfers ID
```

The first transfer call returns a plan/digest without copying. Confirm only the
same authorized plan with its digest; changed discovery requires a new review.
For one download use `scp NAME get REMOTE [LOCAL]`; for upload use
`scp NAME put LOCAL [REMOTE]`, with the same review/confirmation sequence.
Upload sources must be inside the configured upload area, including after link
resolution. Do not copy arbitrary outside files into it or change the root to
bypass a refusal. A root change is a distinct explicit request, using `local
upload PATH` or `local download PATH`; it never moves existing files.

The confirmed result's ID identifies the transfer. Poll `transfers ID` until
the retained record stops, then inspect `state`, per-file outcomes, errors,
byte totals and labelled partial paths. Acceptance is not completion. Report
unreadable/skipped/failed files and replacements accurately; `downloads` totals
exclude uploads. Cancellation uses `transfer-cancel ID` and needs cleanup
acknowledgement; existing destination files stay protected. A lost response
requires checking `transfers` before retrying or overwriting anything.

Working downloads are separate from registered Hovel transfer artifacts. Report
the actual completion/evidence metadata, including failed evidence registration;
use existing Hovel artifact routes to inspect identities and paths. The human
can watch Files/transfer progress and `follow` in the same workspace. Never claim
complete collection merely because a local file exists or a progress bar ended.
