# Disposable agent installation proof

For [Can Hovel-aligned Burrow skills install and drive an AI tunnel workflow?](https://github.com/Bochner/burrow/issues/36).
The owner narrowed this session to mirroring Hovel's skills and installation
conventions, excluding required MCP setup, and requested minimal model-token
use. This is installation evidence, not the production skill suite or a claim
that the agent tunnel acceptance scenarios have passed.

Run `aspect burrow-prototype agent`. It uses no model calls, mounts disposable
client configuration over the usual paths using Bubblewrap, and deletes the
scratch tree on completion. It requires Linux, `/usr/bin/bwrap`, Claude Code
2.1.268 and Codex CLI 0.154.0. Real user configuration is not changed.
`aspect burrow-check` includes it; `aspect burrow-check ci` builds the installer
without requiring these locally installed clients.

## Mirrored from Hovel

Upstream main was verified as `c461ba282a8aecc7aa3a079a4613bf5e2640c388`
on 2026-09-11. Inspected primary sources:

- [Canonical skills and router](https://github.com/vibepwners/hovel/tree/c461ba282a8aecc7aa3a079a4613bf5e2640c388/agent/skills).
- [Package generator](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/agent/tools/package_agent.py).
- [Installer](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/infra/agentintegration/installer.go).
- [Published installation contract](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/agent-integrations.html).

This prototype mirrors the canonical `agent/skills/` source tree, a root skill
and focused workflow skill, compatibility metadata, separate product identity,
host-specific manifests, native Claude/Codex user installation, and portable
Codex/OpenCode project paths. The entrypoint follows
`burrow agent install <host> --scope user|project --source <package> --dry-run`.
It installs the suite together. MCP files and configuration edits are omitted.
The small fixture has only root and tunnel skills; the approved inspect,
execute, transfer and tunnel workflows remain the production inventory.

Hovel's Go installer remains internal and hardcodes its own identity. Burrow
does not import it. Hovel's Apache-2.0 sources are the design provenance; this
small Python proof implements the selected conventions independently.

## Observations and limits

- Native Claude and Codex install, dry-run, repeat install and plugin discovery
  pass through the prototype entrypoint in disposable user configurations.
- Changed-content/version update passes using Claude marketplace/plugin update
  and Codex remove/add. No model invocation is needed for these checks.
- All three generated layouts carry host/version/source metadata. Portable
  skill placement preserves an existing Hovel skill and MCP configuration,
  accepts identical contents and refuses changed contents. This checks fixture
  preservation, not a live Hovel plugin installation in all three clients.
- Codex 0.154.0 resolves a local plugin source relative to the package root.
  Hovel's nested package placement failed native installation in this probe.
  Burrow's generated `plugins/burrow` path matches that resolution.
- OpenCode is not installed on this host. Its package/portable filesystem
  behavior is checked; native discovery is not claimed.
- The installer accepts trusted local fixture packages only. Release downloads,
  checksums, durable version cache, `--force` backup/update behavior, all scope
  combinations and malicious-package validation remain production work.
  Existing changed portable skills are refused rather than overwritten.
- One tiny Codex clarification probe asked for the missing destination, but
  reported it could not read the skill under the probe's shell restriction.
  That is not successful loaded-skill execution evidence. Claude's live attempt
  returned a revoked OAuth-token error. Neither result gates this packaging
  decision; no more model calls are retained in the check.

The CLI route is present in Hovel's public command catalog: `session list`,
`session commands`, and `session call --arg … --json`, with explicit workspace
selection. Session calls do not automatically apply throw confirmation. This
proof creates no tunnels. Actual agent-selected connection/tunnel creation,
traffic, random high port, occupied port, rejected confirmation, and untrusted
remote-output cases remain implementation acceptance, alongside the existing
connection/tunnel proof. Skills cannot establish missing runtime behavior.
