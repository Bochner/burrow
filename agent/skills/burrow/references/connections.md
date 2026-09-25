# Connections, profiles and recovery

Discover the relevant `connection.*`, `profile.*` or `workspace.*` capability
before acting. Examples use the selected absolute workspace PATH and name NAME.
Global flags precede commands. Resolve the host, user and workspace from the
user's selection; names are local to a workspace. With an SSH config alias,
`--user -` uses its configured user. Routine connections use normal current-user
SSH config, identities and agent; add options only for the requested override.

```sh
burrow --workspace PATH connect NAME HOST --user USER
burrow --workspace PATH connect NAME HOST --user USER --review HASH --yes
burrow --workspace PATH inspect NAME
```

Inspect the first response's actual SSH command, resolved identity and digest.
The second command applies that exact authorized recap. Poll until the owner
reports `connected`; dispatch success is not authentication success. Burrow uses
the accepted LazySSH host-trust options, not retained known-host verification.
If authentication needs a password/passphrase, let the human use private
`--prompt` in a terminal and then inspect the same connection. Keep passwords,
passphrases and key contents out of arguments and agent transcripts.

Saved settings are separate from live connections:

```sh
burrow --workspace PATH profiles
burrow --workspace PATH profile select NAME
burrow --workspace PATH profile connect NAME
burrow --workspace PATH profile connect NAME --review HASH --yes
```

`profile create NAME HOST --user USER` saves new non-secret settings;
`profile save CONNECTION` saves authenticated settings. `profile edit` replaces
all fields, so preserve intended options explicitly. Review replacement/deletion
and use the returned `--revision HASH --collection PATH --yes` binding. Discover
`profile.collection`, `profile.load` and `profile.backup` when selecting an
explicit collection or backing it up. Loading settings never connects.

When opening a workspace is requested, use `workspace open`; ordinary discovery
uses `workspace inspect` and does not initialize a missing workspace. An
unverified result is not an empty inventory. On an uncertain response, inspect
the existing connection, run, transfer or session before considering a retry.
Daemon/module loss does not restore commands or shells. Reconnect only on
explicit intent with `reconnect NAME HOST --user USER` and a fresh exact review.

For a requested connection close, review `close NAME`, then submit
`close NAME --review HASH --yes`. This closes its dependent resources, preserving
saved settings and collected evidence. For old managers, discover and review
`workspace restart` or `workspace retire`: both affect workspace resources and
require their returned digest. Uncertain ownership refuses cleanup. Preserve
the reported paths and identities for manual recovery; never remove receipts,
sockets or kill unidentified processes to force reuse. A new connection cannot
reconstruct a lost shell, and cancelling a wait does not undo dispatched work.
