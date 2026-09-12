# LazySSH table and metadata reference

Date: 2026-09-12. Source reviewed locally at `.references/lazyssh`, pinned commit
`9eb84452c31cb527bf8e938e23ffc92974fb91cb` from `Bochner/lazyssh`.
This is source evidence and a proposed Burrow mapping, not an independent feature specification.

## Actual tables and semantic colors

LazySSH uses a distinct foreground role per data column, a shared purple header,
rounded borders, and two spaces of horizontal cell padding. These are the complete
production command-mode table fields, not the reduced demonstration monitor.
[Table implementations](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/ui.py#L91-L201)

| Table | Columns in order, with upstream data role |
|---|---|
| Saved configurations | Name (table.header); Host (highlight); Username (success); Port (warning); SSH Key (accent); Shell (info); Proxy (info); No-Term (dim) |
| Active SSH | Name (table.header); Host (highlight); Username (success); Port (warning); Dynamic Port (info); Terminal Method (accent); Active Tunnels (error); Socket Path (dim) |
| Tunnels | ID (table.header); Connection (info); Type (highlight); Local Port (success); Remote host:port (warning) |

Saved keys longer than 30 characters retain their final 27 characters with an
ellipsis prefix. Missing fields show N/A; no-terminal is Yes/No. Active names
come from the control socket basename and tunnel counts from the connection's
actual tunnel collection. Terminal method is a display-wide argument, not a
verified per-connection property. These distinctions matter when mapping to
Burrow-owned configuration and Hovel-owned live state.
[Saved/active row construction](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/ui.py#L91-L184)

## Suggested Catppuccin Mocha translation

Keep the role distinction while using Burrow's accepted dark palette. Upstream
Dracula mappings are centralized, including separate keyword/operator/string/
variable/number/comment roles.
[Theme source](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/console_instance.py#L21-L57)

| Meaning | Upstream role/color | Proposed Mocha role/color |
|---|---|---|
| Identity, table headers | table.header, purple | Lavender `#b4befe` |
| Hostnames, tunnel type, keywords | highlight/keyword, pink | Pink `#f5c2e7` or Mauve `#cba6f7` for keywords |
| Usernames; successful/connected state | success, green | Green `#a6e3a1` |
| Port values and warnings | warning, yellow | Yellow `#f9e2af` |
| Key paths, proxy/dynamic ports, shell | accent/info, cyan | Teal `#94e2d5` or Sky `#89dceb` |
| Counts and byte values | number, orange | Peach `#fab387` |
| Socket paths and absent optional data | dim, comment | Subtext0 `#a6adc8` |
| Failed/disconnected state | error, red | Red `#f38ba8` |
| Text and labels | foreground | Text `#cdd6f4`, muted labels Subtext0 |

These are recommendations, not claims that every upstream role is ideal:
active tunnel *count* is red upstream even when healthy; Burrow should reserve
red for actual failure/disconnection and use a numeric accent for counts.
Likewise preserve text labels alongside status colors, so color is not the only
signal. LazySSH already provides a text-plus-symbol status helper.
[Accessible status helper](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/ui.py#L562-L573)

## Syntax references

SSH command previews distinguish the command (string), flags (operator), values
(number), username (variable), host (highlight), and shell (keyword).
Help distinguishes commands (highlight), argument placeholders (number), and
explanations (normal text). These role boundaries transfer well to Burrow's
command help and structured metadata without adopting upstream command behavior.
[SSH preview](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/ssh.py#L329-L343),
[help markup](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/command_mode.py#L990-L1008)

## Metadata: accurate ownership and totals

The separate upstream live-monitor helper writes literal `Connected` for every
listed connection; its comment acknowledges this is not measured status. Burrow
must derive CONNECTED/DISCONNECTED from its actual runtime snapshot and keep
Hovel daemon health distinct from remote SSH state.
[Monitor helper](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/ui.py#L501-L516)

LazySSH's `update_transfer_stats` is **not reliable downloaded-file metadata**:
it shares upload and download counters, resets total_files to 1 for each
single-file transfer, and accumulates bytes independently. The reset is even
asserted by an upstream test. Copying this logic would mislabel uploads as
downloads and undercount sequential downloads. Burrow should count successful
completed downloads and their bytes from authoritative transfer
records when that capability is implemented; failures, running transfers, and uploads do not count. Display the
scope explicitly (e.g. selected connection/workspace) and avoid guessing daemon
health from those transfer counts.
[Counter logic](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/logging_module.py#L315-L352),
[upload caller](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/scp_mode.py#L1028-L1039),
[download caller](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/scp_mode.py#L1234-L1251),
[counter test](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_logging_module.py#L753-L800)

## Evidence limits and Burrow checks

Upstream UI tests exercise non-default SSH ports, dynamic forwarding, forward
and reverse tunnels, every saved-configuration option, and long key paths.
Most are smoke calls rather than assertions about cell colors or retained
columns. Burrow needs its own rendered behavior check for those values and
semantic colors, including narrow widths and no-color mode.
[UI tests](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/tests/test_ui.py#L44-L174)

Research-only verification: inspected source and test expectations at the pinned
commit; no upstream tests were run and no implementation was changed for this note.

## Applied in Burrow

The shared table renderer assigns Catppuccin colors by field meaning and retains
the reference column inventory. Saved configurations and tunnels remain labeled
demo data until implemented. Live proxy, terminal and tunnel metrics are
unavailable in the current state API. The sidebar separates observed SSH state,
workspace download totals and daemon health. Download totals remain Unavailable
in live mode; demo totals are samples.

The owner chose a 160×40 minimum terminal to simplify layout behavior. Below that
size Burrow shows a resize notice, preserves drafts/state, blocks hidden input
and permits Ctrl+C to exit. Supported sizes retain both sidebars and full tables;
compact table/section variants have been removed.

The required future implementation and presentation checks are recorded in
[the project TUI standard](../agents/tui.md), linked from AGENTS.md.
