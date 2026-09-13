# Connection-control, approval and responsiveness contract

Accepted by the owner on 2026-09-12 in the live discussion of
[issue #70](https://github.com/Bochner/burrow/issues/70), including the complete
action matrix, conditional shared failure domain, responsiveness criteria and
fallback. This resolves the decision; #71 and #72 must still prove the candidate
before #73 production migration. It does not claim those runtime changes shipped.

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

## Evidence

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

The operator subsequently approved the conditional shared failure trade-off:
one manager per workspace is acceptable if the proof passes. Its failure may
lose all its connections; ordinary close must preserve siblings and reconnect
remains manual. That accepts the candidate for proof, not its production use.

## Control contract for the proofs

Prefer one Burrow manager retained as an ordinary Hovel session per workspace,
subject to #71 and #72. It owns live SSH masters, connections and forwarding.
All capabilities remain in the single public `burrow` module. Frontends and
short-lived adapters project that owner's observed state, never another live
connection registry. Hovel continues to own workspace state, process/session
supervision, run records, plans, confirmations and collected artifacts.

Transparent hosting, verified pinned runtime and actual Unix socket peer,
workspace-local names, private credential prompts, explicit reconnect and
refusal of uncertain resource ownership remain required. These are existing
correctness boundaries, not a new workspace permission system. A workspace is
not a security boundary against another process running as the same operator.

| Action | Authorization and review | Cancellation | Persisted evidence |
| --- | --- | --- | --- |
| Activate manager | Internal prerequisite to the first approved operation, through a supported confirmed Hovel throw; no separate operator approval dialog. Activation alone opens no SSH connection and grants no blanket permission to connect. Reuse only a verified matching retained session. | Abandon undispatched startup; if startup may have completed, reconcile its exact identity before cleanup. Never start a duplicate owner on a timeout. | Hovel activation plan, confirmation, run and ordinary retained session record. |
| Manual connect/reconnect, including saved settings | Recap approval authorizes the exact resolved non-secret settings. Submit through Hovel's throw coordinator; its module invocation forwards the approved request to the selected manager. No second recap, no raw frontend connect command substituted for a throw. Re-review if settings changed. | Before dispatch: no SSH attempt. After dispatch: cancel only that attempt, wait for its exact identity and report confirmed cleanup or uncertainty. A prompt stalled in one attempt must not block list or sibling close. | Real Hovel plan/confirmation/run, correlated with owner generation and connection creation identity. Run launch success is distinct from subsequently observed authentication success. No invented payload or audit records. |
| Host trust and authentication | Apply the accepted LazySSH options and current-operator key/config/agent selection above. Only password/passphrase challenges require additional input; they are not a second operation approval. | Esc/Ctrl+C cancels the attempt; do not treat dismissal as successful authentication. | Non-secret launch settings and Hovel run/session outcome; secrets and prompt replies never enter argv, configuration, history, diagnostics or artifacts. Later status alone is not a durable authentication transcript. |
| List/inspect | Read the exact retained owner through public session operations. No recap or new throw for a read. Failed observation stays unknown/unverified, never an invented empty or disconnected inventory. | Stop the read; leave resources untouched. | Existing Hovel run/session records remain. Live inventory reads do not promise a separate persisted plan, confirmation or audit event. |
| Close selected connection | Existing close recap approves the selected creation identity and its dependent shells/transfers/tunnels. Send the bounded close command to that owner; re-review if its identity changed. Do not close the whole manager session to close one connection. | Before sending: no close. After sending: reconcile; cancellation/transport failure cannot promise rollback. Preserve siblings, saved settings and evidence. | Existing Hovel session state and bounded SDK diagnostics; this is not a separately persisted throw approval or a complete close audit trail. Report acknowledgement/cleanup uncertainty honestly. |
| Chain-originated execution | Normal Hovel planning, confirmation and launch-key constraints apply. A fresh adapter selects the exact existing owner/resource or refuses; no fallback login, invented installed payload or implicit connection recreation. | Preserve Hovel cancellation semantics and the selected operation's explicit cleanup contract; consumer cancellation must preserve the shared connection. | Hovel plan, confirmation, run/results and explicitly collected artifacts, with non-secret owner/resource correlation. Live viewing is not collection. |

Quit retains the subsequently accepted keep-running / verified-close / cancel
review across opened workspaces (#76). Frontend-local shells end on exit; the
daemon remains. The earlier blanket quit-retention wording is superseded.

## Supported methods and pinned limits

Use the public daemon RPC methods `CreateOperation`, `CreateChain`, `AddModule`,
`AddTarget` and `SetChainConfig` for setup where they replace repeated CLI
launches. Keep the supported public CLI `throw --now --allow-dangerous --json`
as the execution coordinator: `--now` translates the already accepted recap
into Hovel's single-operator confirmation, not a second user question. Existing
launch-key restrictions still apply and can refuse the operation. Keep module
installation on its supported path and revalidate the live catalog.

The proof may prepare reusable operation/chain structure, but must bind each
review to its own immutable resolved request. Two frontends must not overwrite
shared mutable chain configuration between review and throw. If the supported
API cannot provide that binding cheaply, retain isolated per-request chains;
removing those chains is a performance hypothesis, not permission to race them.

Extend #34's accepted pinned `ListSessionCommands` / `RunSessionCommand`
workaround to bounded manager controls only within this matrix. The frontend
does not directly use an unplanned connect command. A real confirmed module run
may forward the request to the retained owner; #71 must prove exact routing,
correlation, refusal and result semantics. No claim is made that SDK session
commands themselves carry a verified throw approval. The public SDK does not
supply a new signed approval capability; do not build another authorization
service to manufacture one. If public routing cannot satisfy this contract,
use the fallback. Ordinary `ListSessions` and session operations remain Hovel's
registry and control surface; no private `core/internal` import is allowed.

Raw `ExecuteModule` and launch-key `PendingThrow` APIs are not replacements for
the throw coordinator's persisted plans and confirmations. Fresh Mesh provider
processes do not inherit manager memory. #72 proves the existing bounded
consumer and investigates one real stream; a generic-stream failure can be
documented and deferred without expanding MVP scope.

Hovel v0.4.2 at `c461ba282a8aecc7aa3a079a4613bf5e2640c388` has a cumulative
256-notification module log ceiling (#30). Reuse the bounded diagnostic policy
at manager-process scope: at most 200 ordinary milestones and one suppression
warning, including activation, requests and all connections in the budget.
Never reset it per request/connection or claim dropped diagnostics were saved.
Status and control must continue after suppression; required Hovel run evidence
must not depend on those diagnostics. No second audit database or log service.

Source details and public method references:
[connection-control evidence](../research/fast-connection-control-contract.md).

## Measurement and acceptance for #71

Use the same declared Docker/OpenSSH fixture, pinned Hovel, synthetic credentials,
machine and build mode for baseline and candidate. Record process starts rather
than only surviving process counts. Record machine/load, sample count and p50/p95;
never infer a phase measurement from a total command duration.

- **Submission:** monotonic timestamp when recap approval is submitted, before
  setup or orchestration. CLI noninteractive submission uses the equivalent
  approved-operation entry point. Human time spent reviewing is excluded.
- **Dispatch:** the real owning handler starts processing that approved request,
  before SSH network/authentication work. Measure submission-to-dispatch as local
  control overhead, including any required manager activation.
- **Authentication prompt:** the first password/passphrase input becomes usable
  in the frontend. Measure dispatch-to-prompt separately; key-only authentication
  reports this as not applicable. This phase includes network/server latency and
  must not be described as purely local overhead.
- **Connected:** the owner verifies the authenticated master and publishes its
  observed connected state. Measure submission-to-connected separately. Record
  synthetic prompt-response delay; human typing is never part of a machine SLA.

Cold means the first approved connection after a clean pinned daemon start,
with packages already installed and no preactivated manager; include manager
activation in submission-to-dispatch. Warm means subsequent approved connections
with the verified daemon/manager and module ready. Report fresh package
installation/download time and verified daemon startup separately. Collect at
least 20 cold and 20 warm samples per variant; exercise both key success and a
private challenge, plus cancellation.

Accepted criterion: warm submission-to-dispatch p95 at most 1 second and median
at least 50% below the freshly measured equivalent baseline. Cold p95 must not
regress from that baseline by more than 10%; report startup separately so warm
numbers cannot hide its cost. List and sibling close must remain responsive
during a deliberately stalled prompt. These are proof gates, not a network or
password-entry SLA. If correctness passes but these thresholds do not, publish
the negative performance result and use the fallback unless the owner revises
the criterion with the measurements in view.

Historical #73 evidence measured 6.041/6.198 seconds for key-auth connect and
3.137/3.149 seconds for a reverted public-setup-RPC experiment. Those are totals
from an earlier path, not the phase samples required above. Current baseline
timings from the declared lab will be recorded in the evidence note; neither
historical nor new CLI-return times prove a subsecond dispatch path.

## Fallback, rejected alternatives and downstream work

If #71 or #72 cannot satisfy the contract without unsupported behavior or a
second runtime, keep the current retained per-connection owners and supported
throws. Replace redundant setup CLI launches with documented RPC where proven;
retain request isolation, installation/catalog checks and real Hovel evidence.
Report the remaining latency honestly. Do not adopt a manager solely because a
spinner appears sooner.

Rejected: approval of manager startup as blanket connection authorization;
direct connect via raw session commands/ExecuteModule instead of a real throw;
fabricated payload/plan records; a second daemon, connection registry or audit
database; unproven shared mutable chain state; and migration before proof.

#71 owns the manager/control/concurrency/authentication/log-limit/latency proof.
#72 owns exact fresh-consumer routing and the bounded Mesh investigation.
#73 owns production integration only after those results are accepted, including
the LazySSH host-trust override, current-user identities and actual highlighted
SSH command/config recap. The color infrastructure shipped in local `a725e7d`;
the SSH policy and generated preview remain implementation work in #73.
