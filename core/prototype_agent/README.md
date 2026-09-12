# Disposable direct-skill installation proof

The owner selected direct skill installation, deferring plugin packaging until
its distribution features are needed. This supersedes the native plugin setup
in the [original installation decision](https://github.com/Bochner/burrow/issues/36).
The canonical product skills and CLI-oriented operating guidance remain.
This is a bounded installer proof, not the complete production skill suite.

```sh
aspect burrow-prototype agent-install -- agent install codex --scope project --dry-run
aspect burrow-prototype agent-install -- agent install codex --scope project
aspect burrow-prototype agent
# Optional, with Claude Code, Codex and Linux Bubblewrap installed:
aspect burrow-prototype agent-discovery
```

`burrow agent install <host>` installs the bundled skills. `--source` accepts a
trusted local directory containing `burrow*/SKILL.md` folders and their supporting
files. It no longer accepts generated plugin packages. The default scope is user;
project scope installs beneath the current directory.

| Client | User skills | Project skills |
| --- | --- | --- |
| Claude Code | `~/.claude/skills` | `.claude/skills` |
| Codex | `~/.agents/skills` | `.agents/skills` |
| OpenCode | `~/.config/opencode/skills` | `.opencode/skills` |

Claude's `CLAUDE_CONFIG_DIR` and OpenCode's `XDG_CONFIG_HOME` override their user
config roots. The installer follows the documented standalone locations:
[Claude skills](https://code.claude.com/docs/en/skills),
[Codex skills](https://learn.chatgpt.com/docs/build-skills#where-codex-loads-local-skills),
and [OpenCode skills](https://opencode.ai/docs/skills/), inspected 2026-09-11.

Installation validates skill folder names and required discovery metadata,
refuses symlinked sources/destinations, and preflights the suite before writing.
An installed skill's `.burrow-installed.json` records its installed file hashes.
Identical content is a no-op. An update replaces only skills whose current files
still match that baseline; edited or differing unmanaged skills are preserved
and reported as conflicts. Move a conflicting skill aside and reconcile it
manually before retrying. There is no force-overwrite switch.

Updates preserve the previous directory in `burrow-skill-backups/` beside the
`skills/` directory, outside the agents' skill discovery trees. Review/remove
those backups manually when no longer needed. Each replacement is staged first;
a failed replacement attempts to restore its backup. This is per-skill recovery,
not an all-or-nothing suite transaction or a lock against concurrent editors.
Skills omitted from a newer source remain installed until removed manually.
Unrelated skills, existing Hovel integrations and client configuration are kept.
Installation creates no plugin manifests, marketplaces or MCP configuration and
never launches an agent client.

## Checks and discovery

`aspect burrow-prototype agent` tests all six documented locations, bundled CLI
installation, dry-run/no-op behavior, changed-content update, prior-version
preservation, edited/unmanaged conflict refusal, invalid metadata and symlink
refusal, and coexistence with Hovel/client configuration. This portable test runs
in both `aspect burrow-check` and `aspect burrow-check ci`. Neither gate requires
Claude, Codex, OpenCode, Bubblewrap or model access for the installer checks.

The separate opt-in discovery check starts installed Claude and Codex clients in
scratch configurations with networking disabled. Codex's `skills/list` response
and Claude's initialization command list verify both standalone skills in user
and project scopes. No prompt is submitted and no model call is made. There is
no exact client-version assertion in the default gate. Native discovery was
observed with Codex 0.154.0 and Claude Code 2.1.269; the opt-in probe exercises
client interfaces that may change independently of the installer.

OpenCode is absent on this host: its documented file layout is checked, native
loading is not claimed. On an OpenCode installation, verify both names in the
available skill tool list. Claude users can inspect `/skills`; Codex users can
inspect its skill selector. Restart a running client if it has not refreshed.
The skill files retain the same names and instructions, without plugin namespaces.

The production inspect/execute/transfer/tunnel inventory, release downloads and
checksums, and actual agent-driven SSH acceptance remain implementation work.
Skills cannot establish runtime operations or bypass Hovel confirmations.
Historical plugin evidence is preserved in Git history; plugin distribution is
deferred, not a requirement on the runtime or default validation gate.
