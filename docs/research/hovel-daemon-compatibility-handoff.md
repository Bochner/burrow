# Proposed Hovel daemon compatibility convention

Status: proposal for the Hovel developer, not an implemented or accepted upstream
API. The Burrow owner deferred Hovel changes on 2026-09-11 and requested this
handoff. The [setup feasibility decision](https://github.com/Bochner/burrow/issues/18)
holds the owner decision; this document specifies the proposed upstream contract.

## Problem and development impact

At Hovel `c461ba282a8aecc7aa3a079a4613bf5e2640c388` (v0.4.2), public
`GetDaemonInfo` identifies the workspace, PID, start time, health, effective
access and listeners. It does not identify the running daemon's protocol or
build. Checking a local executable's version does not identify a remote daemon,
or a local daemon that was started from different bytes.

This is **not a blocker to developing Burrow**. Its SSH transport, SDK modules,
chains, terminal frontend and lifecycle checks can be developed against the
verified pinned Hovel artifact in controlled launches. The
[preserved setup proof](https://github.com/Bochner/burrow/blob/70368aa/core/prototype_sdk/SETUP.md)
already exercises package acquisition, configuration precedence, access rules,
detach/reattach and cleanup without modifying Hovel.

The owner narrowed initial support to **Burrow-started, pinned Hovel instances**.
Attachment to other existing local or remote daemons is deferred; no Hovel change
is required for this initial scope. The proposed handshake remains a gate before
claiming verified compatibility with arbitrary existing daemons later.

Normal quit still detaches and retains Burrow's daemon and live sessions. A later
Burrow launch must safely recognize and reuse its own previously started pinned
instance. Controlled test launches do not yet establish that production mechanism:
the first implementation slice must specify and test provenance/identity checks,
including a stale record or replaced process, without treating reachability, a PID
alone, or the installed binary's version as sufficient evidence. If identity cannot
be established, report the problem without restarting the daemon or selecting a
different workspace. This bounded local reuse is distinct from general attachment.

## Minimal proposed contract

Extend the existing public `GetDaemonInfo` response, preserving its current fields:

```json
{
  "protocolVersion": 1,
  "build": {
    "version": "0.4.2",
    "revision": "c461ba282a8aecc7aa3a079a4613bf5e2640c388"
  }
}
```

This fragment is illustrative new metadata only. These fields do **not** exist
in v0.4.2, and the protocol number is proposed, not an assigned upstream version.
Final names and numbering belong to Hovel's maintainer.

- `protocolVersion` is a positive integer defining a documented compatibility
  contract. Increase it when a previously supported request, response or required
  behavior becomes incompatible. Release numbers alone do not define compatibility.
- `build.version` and `build.revision` describe the serving binary, populated at
  build time rather than by inspecting whatever executable is currently on PATH.
  Unknown development-build values must be explicit; do not fabricate provenance.
  These are diagnostics, not substitutes for the protocol contract.
- Burrow declares the protocol versions and required operations it has validated.
  Before operational mutation, it reads discovery through the selected endpoint,
  checks the protocol against that supported set, and checks effective access.
  Add capability negotiation later only if optional features require it.
- Absent, malformed or unsupported protocol information yields an actionable
  compatibility error. No mutation, silent restart, replacement, automatic upgrade
  or fallback into another workspace. Read-only discovery can still explain the
  selected endpoint, workspace and reported build to the operator.
- Document whether new fields are additive for existing clients and verify that
  behavior before release; an additive-looking JSON change is not automatically
  compatible with strict response decoders.

Suggested message: “Cannot verify compatibility with the selected Hovel daemon.
It does not advertise a supported protocol version. Use a supported Hovel release
and reconnect. The daemon and workspace have not been changed.”

## Acceptance checks for the upstream handoff

1. The actual serving process reports its compiled protocol/build metadata through
   the owner Unix socket and permitted TCP discovery routes; existing workspace
   identity and effective-access behavior remain intact.
2. Burrow accepts a supported protocol and rejects missing, malformed and unsupported
   protocol values before issuing operational mutations. An unchanged or replaced
   executable on the client's PATH cannot change the daemon's reported identity.
3. Rejection preserves the running daemon and workspace. It produces no replacement
   process, fallback workspace or automatic upgrade.
4. Existing supported clients still consume discovery as documented; tests verify
   the chosen forward/backward compatibility behavior.
5. Rerun Burrow's setup/attachment proof against a pinned upstream revision that
   implements the contract before advertising verified existing-daemon attachment.

This is compatibility metadata, **not authentication or attestation**. It does
not make plaintext TCP confidential or prove a hostile daemon's claims. Retain
Hovel's owner-socket, read-only TCP and explicitly acknowledged insecure-full
restrictions. No new authentication system is proposed here.

## Evidence

- [Public daemon documentation at the inspected pin](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/daemon-rpc.html).
- [OpenAPI discovery schema](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/spec/reference/daemon-rpc.openapi.json).
- [Serving process discovery response](https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/internal/adapters/daemonrpc/daemonrpc.go).
- [Executable provenance and passing runtime proof](https://github.com/Bochner/burrow/blob/70368aa/core/prototype_sdk/SETUP.md).

No Hovel patch, upstream issue or message to its developer has been submitted.
