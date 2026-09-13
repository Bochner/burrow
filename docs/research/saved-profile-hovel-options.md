# Saved connection storage: Hovel facilities

Research date: 2026-09-12. **Research evidence; the owner subsequently selected file-backed JSON collections.**
This supplements the #47 decision matrix; runtime acceptance is covered by the
implementation checks. Hovel baseline is the checked-out, pinned
`c461ba282a8aecc7aa3a079a4613bf5e2640c388`. Findings describe that revision,
not an assumption about newer upstream releases.

## What Hovel actually provides

**Persistent chain configuration is available through the public daemon RPC.**
`SetChainConfig` and `UnsetChainConfig` operate on one string key at a time,
under an operation and chain. The server serializes each request, publishes an
event containing the key rather than the value, and persists the operator state.
`PersistedChain` includes its configuration map and logs. These APIs can store a
selected collection path or encoded non-secret settings, but offer neither a
profile domain object nor a conditional revision in their request contract.
An entire collection in one config value therefore has last-writer-wins risk
when two clients perform independent read/modify/write operations. One record per
key avoids losing unrelated records but does not detect edits to the same record.
[RPC implementation](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonrpc/daemonrpc.go#L2307),
[persistence shape](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/app/operatorsession/session.go#L90),
[public request schema](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/spec/reference/daemon-rpc.openapi.json#L5773).

**Chain KV is not durable profile storage.** It supports revisions and atomic
mutation batches, but its values intentionally disappear on restore. Although
the pinned RPC source accepts omitted throw IDs for a session-scoped store,
the persisted state omits KV, and `TestChainKVIsEphemeralAndRejectsStaleBatches`
explicitly verifies that export/import restores an empty store. The public
OpenAPI also marks `throwId` required. Neither the session fallback nor the
throw-owned SDK KV should be used to claim durable saved profiles with CAS.
[Lifecycle documentation](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/chain-kv.html),
[ephemerality check](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/app/operatorsession/session_test.go#L42),
[RPC fallback](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonrpc/daemonrpc.go#L2331),
[request schema](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/spec/reference/daemon-rpc.openapi.json#L5713).

**Native chain save/load is an execution-definition format.** Configured exports
include module steps, chain config and target settings; template exports omit
the settings. Loading validates the file, deletes an existing same-name chain,
recreates it and applies steps/configuration in separate calls. Shape validation
requires at least one step. A dedicated profile-only bucket is consequently not
a round-trippable native chain file without adding execution content. Reusing
this loader for profiles would also replace the chain that owns its logs and
would not provide an atomic profile import. Exported chain metadata names a
chain, not a workspace: loading targets the operator session's current operation.
These are useful execution-chain features, not a ready-made connection library.
[Chain format](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/chains-runs.html),
[validation, export and import implementation](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/app/commands/catalog.go#L5417).

**Hovel's startup YAML is a different configuration surface.** Its documented
precedence is defaults, global, workspace, then an explicit config file, and a
running daemon rejects changed effective configuration until restart. It should
not be treated as a hot-reloadable saved connection library. A path stored through
chain configuration uses the mutable operator-state surface instead.
[Configuration contract](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/configuration-distribution.html#L20).

## Implications for the owner decision

The following comparisons are design inferences from the facilities above.

| Option | Fits well | Work and limits |
| --- | --- | --- |
| Editable collection file; selected path retained by Hovel | User-chosen path, copy/share/version-control, edit the selected source directly | Burrow needs strict non-secret schema, atomic replacement, concurrent-writer handling, path validation and explicit overwrite review. Backing up Hovel alone retains the pointer, not an external file. |
| Non-secret profile records in Hovel chain config; explicit Burrow export/import | Daemon-owned persistence and existing config-change logs; no second authoritative profile file | Burrow still needs a portable schema/import policy. No revision-checked edits through config RPC. A reserved chain is an application convention, not a typed profile service. An imported file becomes a snapshot, so later edits do not update that original path. |
| Native Hovel chain files as the profile format | Actual runnable Hovel chains with modules and targets | Requires execution steps and uses destructive same-name chain replacement; poor fit for passive settings and independent history. |
| Hovel chain KV as profile storage | Temporary handoffs during execution | Reject for saved profiles: not persisted. |

For the accepted separation between saved settings and live resources, both of
the first two designs must keep profiles non-secret, loading passive and
reconnection explicit. Neither storage choice supplies these behaviors
automatically. A profile bucket must remain separate from operational connection
chains so deleting or loading settings cannot replace live-resource ownership or
evidence. This follows Burrow's current
[context](../../CONTEXT.md) and
[recorded ownership direction](../wayfinder-start.md).

**Recommendation for the matrix:** retain file-backed collections if selecting
and continuing to edit a specific file is the intended #11 workflow. Prefer
Hovel-owned records only if the owner instead wants an import/export workflow
whose external files are snapshots. There is no demonstrated native durable KV
or profile export/import facility that eliminates this tradeoff at the pinned
Hovel revision. The owner subsequently selected file-backed JSON collections,
with an automatically created template and explicit active-connection saving.
