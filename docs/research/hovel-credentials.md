# Hovel credential and workspace contracts

Research date: 2026-09-11. Source pin and verified current Hovel default-branch head: `c461ba282a8aecc7aa3a079a4613bf5e2640c388`. Investigation for [What credential and workspace contracts can Burrow reuse?](https://github.com/Bochner/burrow/issues/5). Source review only: no credentials supplied, authentication attempted, or remote workflows executed.

## Finding

Reuse Hovel's daemon-owned workspace, public input/schema contracts, session/result boundaries and structured logging. Do not treat a secret-marked requirement as an SSH vault: generic operator configuration is persisted as ordinary JSON, while Hovel's encrypted credential custody is specifically X.509 workspace PKI. The accepted SSH baseline therefore still needs a credential delivery/retention decision and an explicit SSH host-trust policy. [Schema][schema], [operator state][operator], [SQLite persistence][sqlite], [PKI guide][pki].

## Secret input and persistence: traced boundary

The SDK exposes `Requirement.Secret` and plain `Context.Inputs`, `TargetConfig`, and `ChainConfig` maps. Input precedence is run inputs, target config, then chain config. These are values available to module code, not opaque encrypted credential handles. Hovel's Huh configuration form applies password echo mode when `Secret` or secret type is set, then sends the entered string through `SetTargetConfig` or `SetChainConfig`. Masking the form does not change the stored value. [SDK context][context], [schema][schema], [Huh input][huh].

The generic persistence path is concrete:

1. Operator-session setters store chain and target values in `map[string]string`; their log entry records the key rather than the value.
2. Operator-session export clones target configs and chain configs into `PersistedState` without a requirement-aware secret filter.
3. The daemon runtime wires persistence to the workspace store. `SaveOperatorSession` JSON-marshals that state and inserts it directly into SQLite `operator_sessions.state_json`; this is not the separate PKI key-envelope path.
4. The chain-file writer also serializes the config maps it receives for non-template exports, requests mode `0644` and has no encryption or secret classification of its own. Actual permissions depend on umask/existing files; this is not a claim that every upstream export caller necessarily includes every secret.

Sources: [operator state][operator], [daemon persistence wiring][runtime], [SQLite persistence][sqlite], [chain writer][chainfile]. This source path establishes a real persistence risk for passwords/passphrases placed in generic config; it does not require assuming every log or artifact leaks. A per-run input field alone is also not proof of non-persistence elsewhere in run planning/audit. Verify the complete chosen path using a synthetic canary before accepting transient delivery.

## Workspace and process ownership

Hovel scopes operational state through the daemon/workspace. Workspace storage resolves the selected path, retains a stable ID in `workspace.json`, and delegates operational records to a workspace SQLite store. Artifact materialization uses protected content-addressed files; general workspace directories are not universally secret-only (layout requests `0755`, artifacts `0700`). A workspace is an operational isolation boundary, not a sandbox preventing trusted module code from accessing the operator account. [Workspace implementation][workspace], [module-development trust model][development].

Global/workspace/explicit configuration precedence and module install scope already belong to Hovel. Modules installed for a workspace take precedence over global installed packages; this does not create a separate SSH-agent identity. Burrow should reference the active Hovel workspace and emit outputs/artifacts through supported contracts, not maintain a second copy of daemon session, audit or engagement inventory. The standalone mode's profile/secret storage remains Burrow's separate decision. [Configuration contract][configuration], [module development][development].

The module process runner explicitly assigns `cmd.Env = os.Environ()` for executable modules. Consequently a module can inherit `SSH_AUTH_SOCK` if it exists in the daemon/runner environment and the socket is accessible to that process. No automatic bridge from the currently attached frontend's shell environment is established. A daemon started before an agent change, in a service/container, or on another host may see a missing/stale/inaccessible socket. This is an inference from the launch code, not a tested SSH-agent feature. [Runner command construction][runner]. Agent forwarding to a remote host is a distinct capability and must not be enabled merely because local agent authentication is accepted.

## What encrypted credential delivery actually covers

Workspace PKI owns X.509 certificate generations, trust/assignment/revocation and encrypted local private keys. The documented implemented envelope uses AES-256-GCM with owner-protected master-key files. It is not an OpenSSH user/host certificate authority, known-host database, generic password store, or SSH-agent service. [TLS and workspace PKI][pki].

Public Go SDK credential-provider interfaces describe optional runtime/files/encoding/stamping operations and redacting secret wrappers. The current public daemon credential selector accepts only runtime delivery of certificate DER/public, public-key SPKI/public, or private-key PKCS#8/private-bytes for four Mesh mutations. General non-Mesh consumption and other external selections are deferred in the documentation. Generic extensible types do not mean those delivery paths already work for an SSH connection module. [Credential-provider guide][provider], [credential SDK][credential].

The implementation's resolver can internally construct a credential bundle, but rejects signer-reference and several other projection cases as unimplemented. Private-key byte delivery rejects externally held keys. The PKI guide explicitly reports no bundled HSM/KMS/PKCS#11 backend. Thus custom-purpose, external-key and signer-reference vocabulary does not establish hardware-backed SSH authentication. [Runtime resolver][resolver], [PKI guide][pki].

## Mapping the accepted SSH baseline

| Accepted requirement | Reusable Hovel surface | Remaining SSH decision/proof |
| --- | --- | --- |
| Password authentication | Secret schema/input UX, structured execution | Transient delivery that avoids generic config persistence; prompt cancellation; no password in argv/history/logs/results. |
| Encrypted private keys | Non-secret key-file reference can be configuration | Decryption/passphrase prompt and lifetime; supported formats; no claim that PKCS#8 PKI delivery accepts encrypted OpenSSH keys. |
| SSH agents | Runner environment inheritance | Actual daemon socket reachability, identity selection, unavailable-agent behavior; no automatic frontend socket forwarding. |
| Host-key verification | Hovel confirmation/audit framework can record operator decisions | SSH known-host lookup, deliberate unknown-host approval, changed-key failure, policy scope/storage; X.509 trust is separate. |
| SSH config aliases | Normal string inputs | Which OpenSSH config directives are honored and where the config is read; Hovel has no demonstrated parser for them. |
| Jump hosts | Existing Mesh bridge can supply a stream when explicitly selected | OpenSSH ProxyJump/ProxyCommand semantics, jump-host authentication/trust, and whether bridging is an alternative or composition. Mesh is not implicit SSH config support. |

Sources: [schema][schema], [context][context], [runner][runner], [SDK Mesh bridge](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/mesh_bridge.go), [existing LazySSH baseline](lazyssh-parity.md). SSH behavior choices remain transport-dependent; this table does not approve a transport.

Bounded source inventory found no implemented SSH client in the public SDK or bundled examples. Searches for `golang.org/x/crypto/ssh`, `SSH_AUTH_SOCK`, `known_hosts`, `ProxyJump`, and `keyboard-interactive` found no corresponding integration. Documentation names `ssh-memory` and `ssh-survey` as illustrative descriptors (including `example.invalid` package URLs), not shipped SSH modules. Dependency presence elsewhere is not evidence of SSH capability. This does not inventory all third-party Hovel packages. [SDK tree][sdktree], [example tree][examples], [descriptor examples][descriptors].

Additional decisions worth asking about are keyboard-interactive/MFA, OpenSSH user/host certificates, hardware-backed keys, exact config-directive coverage and agent forwarding. They are not newly discovered Hovel-provided features, and remain outside the accepted baseline unless the owner adds them.

## Recommended next decision and acceptance boundary

Prefer non-secret saved configuration and existing key/agent references. Before enabling password/passphrase persistence, choose an explicit protected custody mechanism supported by the chosen mode; do not silently route those strings through ordinary saved Hovel config. For ephemeral input, prove where prompting occurs without writing terminal data to module protocol stdout, how headless use fails or receives credentials, and what ends the secret lifetime.

The smallest later check uses a synthetic secret canary in an isolated workspace, exercises the chosen input path and a failed/cancelled authentication, then verifies selected config/export/state/log/artifact surfaces do not retain it. Add agent present/missing/stale cases and separate unknown/changed host-key checks in the first SSH integration fixture. No live engagement or real credentials are needed. These checks are proposals, not executed results.

## Validation

Research artifact only. The Aspect `build //:research` metadata gate passed, using the cached Aspect CLI with its Bazel shim directory on PATH; it cannot prove credential safety, SSH behavior or prose correctness. No upstream behavior tests were run.

[schema]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/module.go
[context]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/context.go
[huh]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/cli/interactive_config_huh.go
[operator]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/app/operatorsession/session.go
[sqlite]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/storage/sqlite/store.go
[runtime]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/infra/daemonruntime/runtime.go
[chainfile]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/commandmode/chain_file_store.go
[workspace]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/storage/filesystem/workspace_store.go
[runner]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/moduleruntime/pythonrpc/runner.go
[configuration]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/configuration-distribution.html
[development]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/module-development.html
[pki]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/tls-pki.html
[provider]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/credential-provider-development.html
[credential]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/credential_delivery.go
[resolver]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/app/pki/credential_resolution.go
[sdktree]: https://github.com/vibepwners/hovel/tree/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel
[examples]: https://github.com/vibepwners/hovel/tree/c461ba282a8aecc7aa3a079a4613bf5e2640c388/modules/examples
[descriptors]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/reference/descriptors.html
