# Saved connection collections: decision matrix

Date: 2026-09-12. Status: **owner selected option A with JSON**, prompted during
implementation of #47. The owner agreed to portable JSON collections and requested automatic on-disk templates and active-connection saving. The live implementation and Pages documentation carry the concrete contract; comparison alternatives remain research.
Burrow inspected at `261d42ce0d14228c724ada3ca5031e36ddaf0a26`; LazySSH at
`9eb84452c31cb527bf8e938e23ffc92974fb91cb`. Supporting Hovel investigation:
[saved-profile-hovel-options.md](saved-profile-hovel-options.md).

## Recommendation

Use **portable named collection files, with profiles able to reference OpenSSH
aliases**, and Hovel retaining the selected collection path and management history.
This preserves the useful LazySSH workflow and the accepted load-path contract,
while making the edited source explicit. Avoid mirroring profile contents into a
second authoritative store. Treat Hovel-native records with export/import as the
alternative if workspace-contained copies matter more than direct file editing.
This recommendation is an engineering judgement from the evidence below, not a
claim that upstream supplies a complete Burrow profile manager.

## Main decision — plain English

A **profile** is a saved recipe for connecting, such as “NAS: alice at
192.168.1.20”. A **collection** is a group of those recipes, such as “Homelab”.
A **workspace** is where Hovel keeps a particular lab or engagement's work and
evidence. Opening recipes does not connect to any machine.

| Choice | What you would do | Where your edits go | Good part | Catch |
|---|---|---|---|---|
| **A. Open and edit a collection file — recommended** | Open `homelab.json`, choose “NAS”, then press Connect. | Change NAS's port to 2222: Burrow updates that same `homelab.json`. | Easy to keep, copy, back up and reuse. | If two workspaces open the same file, both use its updated settings next time. |
| **B. Copy profiles into each workspace** | Import `homelab.json` into “Lab”. Choose “NAS”, then press Connect. | Change NAS's port to 2222: only Lab's copy changes. The original file stays the same. | Good for keeping customer or engagement settings separate. | Copies drift: importing into Lab and Client-A gives two copies you must update separately. |
| **C. Manage your existing SSH configuration directly** | Choose the `nas` entry from `~/.ssh/config`, then press Connect. | Change its port: ordinary `ssh nas` uses the change too. | One place for settings used by several SSH tools. | SSH settings can be inherited from patterns and other files. “Edit NAS” may not correspond to one clean record. |

**Example: using two workspaces.** With A, Lab and Maintenance can both open the
same Homelab file. Updating a saved address from Lab changes the next connection
from Maintenance too; neither workspace's current connections are interrupted.
With B, Maintenance keeps its old imported address until you update or reimport
it. With C, changing the SSH alias affects other SSH tools that use that alias.

**Example: backups.** With A, make `homelab-before-changes.json`, then explicitly
open that file if you need its settings later. With B, export Lab's profiles to a
backup file, then explicitly import the backup into the chosen workspace. With C,
a file backup may be incomplete if `~/.ssh/config` includes settings from other
files; Burrow would need to define what it copies and restores.

**Example: moving computers.** A lets you copy the collection; B lets you export
and import it. C means moving the relevant SSH configuration files. In all cases,
a reference such as `/home/alice/.ssh/id_ed25519` may need changing on the new
computer. Profiles do not copy private keys or passwords.

A best preserves the already agreed “open this collection and keep using it”
behavior. B is worth choosing if you prefer each workspace to own an independent
copy. C is useful as a source of connection settings, but I do not recommend
turning Burrow into an editor for arbitrary SSH configuration.

## Proposed everyday behavior, with examples

| Action | Example | What happens |
|---|---|---|
| Open a collection | Open Homelab containing NAS and Router. | Both appear in the saved table. Neither connects. |
| Select a profile | Highlight NAS. | Show its saved details; wait for Connect. |
| Connect | Press Connect on NAS. | Review the destination, then authenticate. |
| Save after success | Connect manually to a new Pi. | Offer “Save as Pi in Homelab?” Declining leaves the connection working. |
| Reuse a saved profile | Connect to Pi again without changing settings. | Avoid asking to save the same unchanged settings again. |
| Edit | Change Pi's saved address. | Future connections use the new address; the current connection continues. |
| Delete | Delete Pi while it is connected. | Remove the recipe. Keep the live connection and its evidence. Close is a separate action. |
| Back up | Save Homelab as `homelab-before-changes.json`. | Create a separate settings copy; do not overwrite an older backup silently. |
| Open a broken file | Open a collection with invalid contents. | Explain the error and keep the current collection. Do not pretend the broken file is empty. |
| Failed save | Disk fills while changing Pi's port. | Report failure and preserve the previous saved data. Keep any successful connection working. |
| Another editor changes a profile | Two windows edit Pi at once. | Refuse the stale edit and ask for a fresh review; do not silently erase the newer change. |
| Use an SSH alias | Save `nas` plus an explicit username override. | Keep the alias and override. Evaluate its current SSH settings when connecting. |
| Use an SSH agent | Save “use my current agent”. | Find the agent for that connection attempt; do not accidentally save today's temporary socket as a permanent address. |
| Run a second connection | Connect the NAS profile as `nas-maintenance`. | Reuse the recipe with a distinct live connection name. |
| Review history | Look up yesterday's profile changes. | Use Hovel's retained management history, without authentication secrets. |

All rows describe proposed behavior, not features already shipped.

## Behavior recommended regardless of storage

- Keep saved profiles distinct from live connection instances. A saved name can
  default the live connection name, but an explicitly chosen alternate live name
  should be possible without duplicating the saved profile. Editing/deleting
  settings affects future connects, never existing transports or evidence.
- Use a visible collection selector above the saved table and show its exact
  source in metadata/edit/backup review. Selecting a row only selects it. A
  separate Connect action reviews the endpoint and starts authentication.
- Offer Save after successful authentication when settings are new or changed.
  Default to the connection name, allow a different profile name, and review any
  replacement. A save failure must preserve the successful connection and report
  that settings were not saved. Avoid prompting repeatedly for unchanged profiles.
- Save the operator's intent: alias or explicit endpoint, deliberate overrides,
  key references and an agent policy. Do not silently convert an alias to today's
  resolved address or freeze an inherited agent socket. Resolve the current agent
  at explicit connect unless the operator deliberately saved a fixed reference.
- Open/load parses and validates settings only. It must not run `ssh -G`, inspect
  keys by invoking helpers, authenticate or recreate resources. Resolve trusted
  SSH configuration only during explicit connect/review; show inherited values
  as inherited/unresolved in the saved table instead of inventing resolved data.
- Keep profile files free of passwords, passphrases, private keys, prompt sockets,
  live process/socket IDs and one-time approvals. Trust remains in known_hosts.
  A backup contains references, not a portable credential bundle.
- Reject malformed collections visibly, keep the previous selection after a failed
  load, preserve prior bytes after a failed replacement, and refuse stale edits
  rather than silently overwriting another writer. Create uniquely named or
  explicitly chosen non-overwriting backups; opening a backup is explicit.
- Use one production command path for TUI, CLI and Hovel callers. Hovel retains
  management events/history under its existing retention contract. Do not equate
  log history with a replayable database of successful mutations.

These are proposed refinements of #47, not a claim they are already implemented.
The reviewed shell/proxy/no-terminal table fields still need truthful treatment:
show their current capability meaning, never persist unsupported options as if
implemented. Future shell and tunnel slices own their runtime behavior.

## File format if A is selected

This changes how the saved file looks, not where edits go.

| Choice | Example | Benefit | Tradeoff |
|---|---|---|---|
| **JSON — recommended when using Burrow to edit** | `{"name": "nas", "host": "192.168.1.20"}` | Straightforward for Burrow, scripts and agents to read/write. | No comments inside the file. |
| **TOML — good for regular hand-editing** | See below. | Readable settings with comments. | Requires another Go library; preserving comments during Burrow edits needs explicit support. |

Illustrative TOML, not the final schema:

```toml
[nas]
host = "192.168.1.20" # Storage server in the cupboard
```

If you normally change settings through Burrow's forms, JSON is sufficient.
If you want to maintain annotated files in your editor, TOML has a real benefit.
Either format still needs safe saves and conflict handling.

## Evidence

1. [Issue #47](https://github.com/Bochner/burrow/issues/47) requires profile CRUD,
   save-after-success, backup/reopen, persistent secret-free history, shared
   command/Hovel/TUI behavior and zero authentication on load. [Ownership decision
   #11](https://github.com/Bochner/burrow/issues/11) explicitly describes `--load PATH`
   populating saved entries and the selected collection governing later operations.
   [Parity #7](https://github.com/Bochner/burrow/issues/7) excludes legacy imports
   and exact command spelling. The owner asked for this comparison before choosing.
2. LazySSH [configuration implementation](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/config.py)
   supports TOML, 0600 files and temporary-file replacement. Its read API accepts
   a custom path, but save/delete/get helpers use the default path; parse errors
   return an empty map; backups replace one `.backup`; textual section substitution
   and string interpolation are used instead of a TOML serializer.
3. LazySSH [command flow](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/command_mode.py)
   implements explicit saved-profile connect and offers save/name/overwrite after
   successful wizard authentication. [Configuration tests](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_config.py)
   cover ordinary save/update/comments/failure cleanup, often replacing the default
   path helper; those tests do not establish end-to-end custom-path consistency.
4. OpenSSH's [official ssh_config manual](https://man.openbsd.org/ssh_config)
   documents Host patterns, conditional Includes, first-value precedence,
   Match exec and IdentityAgent/SSH_AUTH_SOCK. These are configuration rules rather
   than a ready-made profile database. Context7 `/openssh/openssh-portable` also
   traced [ssh -G](https://github.com/openssh/openssh-portable/blob/master/ssh.1)
   and [Match evaluation](https://github.com/openssh/openssh-portable/blob/master/readconf.c).
   Inference: automatic effective-config enumeration is incompatible with a strict
   side-effect-free load guarantee when arbitrary trusted Match commands can run.
5. Burrow's current [SSH resolution](../../core/connection/ssh_config.go) calls
   OpenSSH for trusted config evaluation, then copies only accepted endpoint,
   identity and jump settings. It replaces `owner.config` with resolved settings,
   so save-after-success must deliberately retain original input if preserving
   aliases/inheritance is the chosen behavior. This is a local source observation.
6. The [XDG specification](https://specifications.freedesktop.org/basedir/latest/)
   distinguishes configuration, persistent state and runtime data. A durable
   user collection belongs in configuration or an explicit operator path, not
   LazySSH's temporary-directory default. Exact default placement should follow
   the selected authority model and Hovel conventions.

## Decision and validation boundary

This is source research, not runtime acceptance. Before the owner requested this
comparison, one command-level test was run and failed as expected because profile
commands were not yet connected. The incomplete draft was preserved under
`.scratch/issue-47-draft/` and removed from active build inputs. No implementation
commit, push, PR, tracker publication or accepted-design update was made.

The owner selected A: edit the opened JSON collection. Runtime acceptance belongs
to the implementation checks; this source comparison is supporting evidence.

Owner follow-up: preserve strict JSON using inactive `_comment`/`_example` documentation fields, create the workspace template automatically, retain explicit active-connection saving, and update Pages with all workflows/examples.
