# Manual connection approval and LazySSH authentication behavior

Accepted operator behavior, 2026-09-12, from the owner's live discussion of
[issue #70](https://github.com/Bochner/burrow/issues/70). The runtime and
orchestration decision in that ticket remains open; this record does not
authorize a production manager migration or claim that these changes are shipped.

The operator enters connection settings, reviews the recap, and presses Enter
to approve connecting. Loading saved settings does not connect; choosing Connect
from those settings presents the same recap. That recap is the approval: do not
add another approval screen or a new workspace permission scheme. Future
chain-originated automation retains Hovel's supported approval rules.

The recap must include the actual SSH command that will run, with token-level
syntax highlighting following [the TUI standard](../agents/tui.md). Keep resolved
connection details visible. The owner accepted showing the actual generated
SSH configuration in the recap alongside the command that uses it, rather than
requiring an expanded command with every setting as a flag. Render the actual
arguments with unambiguous quoting; do not substitute an illustrative LazySSH
command. Highlighting must preserve the underlying text and respect NO_COLOR.
Passwords and key passphrases never belong in the command or recap.

The owner explicitly chose LazySSH's host-trust behavior:
`-o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no`, superseding the
earlier requirement for host-key confirmation on ordinary connections. These
options suppress normal host-trust confirmation and discard user known-host
writes; they do not authenticate the server against a retained user trust record.
This is an intentional compatibility decision, not a conclusion inferred from
research. Lowercase `-o` supplies SSH options; uppercase `-O` controls an existing
master and is unrelated to this choice.

With no explicit key path, use the current operator's normal OpenSSH identity
selection, including default key files, SSH configuration and agent identities.
Do not hard-code one default filename. An explicit path supplies `-i`, with `~`
resolved for that operator. An accepted usable key authenticates without another
prompt; an encrypted key may need its passphrase unless available through an
agent. If no usable key is accepted, allow the account-password prompt when the
server and client settings permit it. Merely finding a key file is not proof
that authentication will succeed. Keep credential entry private and ephemeral.

## Evidence and remaining decision

LazySSH at `9eb84452c31cb527bf8e938e23ffc92974fb91cb` constructs the command,
asks for confirmation, and launches OpenSSH in
[`src/lazyssh/ssh.py`](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/ssh.py).
Saved-config connects route through that same function in
[`command_mode.py`](https://github.com/Bochner/lazyssh/blob/9eb84452c31cb527bf8e938e23ffc92974fb91cb/src/lazyssh/command_mode.py).
Its optional-argument test checks `-p`, `-D` and `-i`; those mocked tests do not
prove live password or passphrase behavior. Identity selection is delegated to
[OpenSSH](https://github.com/openssh/openssh-portable/blob/master/ssh_config.5).

Burrow's inspected baseline is `8ad2d46003e4d4f0c28542f4ffd0eda21c80bb32`:
`core/connection/commands.go` returns a textual recap without a command;
`core/connection/connection_linux.go` launches SSH with a generated config;
`core/connection/ssh_config.go` still enforces the earlier host-trust policy.
These are implementation gaps, not completed acceptance checks.

Still unresolved in #70: the supported persisted Hovel plan/confirmation and
launch-constraint path; manager activation and the complete action matrix;
extension of #34's pinned workaround; acceptance of a shared manager failure
domain and aggregate log ceiling; measured cold/warm responsiveness targets;
and the final fallback contract. No manager failure trade-off or bypass of
Hovel's orchestration guarantees was approved by accepting this operator flow.
