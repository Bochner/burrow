# Dropbear as a Hovel/Burrow chain fixture

Research date: 2026-09-12. Evidence and recommendations only; no deployment
or chain implementation was performed. The owner requested this investigation
alongside [#45](https://github.com/Bochner/burrow/issues/45). Automated provisioning
and callback/session handoff remain follow-on work under the
[accepted map](https://github.com/Bochner/burrow/issues/1).

**Recommendation:** use Dropbear for a small Linux deployment-and-connect proof.
It can provide the SSH endpoint after a Hovel throw, while Burrow keeps its
existing OpenSSH client/master design. Retain an OpenSSH disposable lab as the
broader compatibility reference. A deployed listening SSH server proves inbound
SSH handoff; it does not by itself prove the later reverse-tunnel/callback chain.
This is an architectural recommendation, based on the server and chain contracts
below, not an already working integration.

## Source baseline and packaging

The upstream site lists **2026.94**, released 2026-07-23. The GitHub tag
`DROPBEAR_2026.94` resolves to
`28216cd9af822732a1549b78621c2ea9a76a0fe5`; source inspection below uses that
revision. Context7 resolved `/mkj/dropbear`; its documentation results were
cross-checked against the pinned source. [Release site][site], [tag][tag].

The commit archive was downloaded and hashed during this research:

| Input | Observed value |
| --- | --- |
| Archive | `https://codeload.github.com/mkj/dropbear/tar.gz/28216cd9af822732a1549b78621c2ea9a76a0fe5` |
| SHA-256 | `e3dbb92dfe44a439ff6d371eaf0120410e23e0d0aae0c6c365c3489a54b55165` |
| Size | 2,924,016 bytes |

This checksum records retrieved bytes, not a verified upstream signature. The
official release tarball download returned HTTP 403 in this environment; its
digest was not verified. Before implementation, pin the selected archive,
compiler/libc and build flags in declared targets entered through Aspect.

Upstream supports static linking with `--enable-static`, selecting only required
programs, and a combined multi-call binary. Start with `dropbear` plus
`dropbearkey`; a multi-call package is optional. Keep upstream hardening and
current cryptography. A musl build is a candidate for reducing target dynamic
library requirements, but still needs actual Linux/architecture testing.
[Build instructions][install], [small builds][small], [multi-call build][multi].

Static does not mean “anywhere”: the executable must match CPU/OS, find usable
account data and a shell, and have the necessary entropy and PTY facilities.
Do not promise upstream's historical minimum binary-size example for a modern
build. Preserve the complete upstream license notices and bundled-library
notices when distributing the artifact. [Platforms][site], [account checks][auth],
[PTY build notes][install], [licenses][license].

## Smallest useful target configuration

Use a private per-run directory, an explicit high-port bind, a unique host key,
and only the operator's public authentication key. The operator's private key
stays local. Generate the host key on the target through the already trusted
bootstrap execution path, then return its public key/fingerprint through that
path before Burrow connects. Do not package one shared host private key into
every deployed binary. These are proposed custody rules; Dropbear provides
host-key generation and public-key extraction. [Key handling][readme].

For a non-root fixture account, the intended argument shape is:

```text
dropbear -F -E -s -w -j -k \
  -r <private-run-dir>/host-key \
  -D <private-run-dir>/auth \
  -P <private-run-dir>/dropbear.pid \
  -p <explicit-lab-address>:<high-port>
```

This is a proposed configuration, not a tested launcher. `-F` retains foreground
operation; `-E` selects stderr; `-s` disables passwords; `-w` rejects root;
`-j/-k` disable forwarding for the initial connect-only proof. The auth directory
contains `authorized_keys`, with restrictive ownership/modes. An address-less
port listens on all addresses, so always specify the bind. Do not enable `-e`
environment inheritance. [Server manual][manual], [auth path source][pubkey].

Run as the intended existing non-root user. Dropbear refuses a different UID
when unprivileged and validates the account's shell. Merely setting `USER` or
`HOME` does not create an account or change its identity. Root-enabled multiuser
deployment is a separate requirement: an absolute shared `-D` path is not a
per-user key mapping and must not accidentally grant the same key access to
multiple accounts. [Account validation][auth], [authorized-key lookup][pubkey].

Non-root PTY allocation is explicitly caveated upstream: prove it on the actual
target before claiming interactive-shell support. More significantly, Dropbear
does **not** provide an SFTP server; it invokes an external one, by default at
`/usr/libexec/sftp-server`. A single Dropbear executable can exercise #45's
shell-free connection lifecycle, but not the accepted SFTP-based transfer
workflow without that extra dependency/path configuration. Legacy SCP is not a
substitute for the chosen Burrow subsystem interface. [Non-root notes][readme],
[SFTP configuration][options], [subsystem execution][channel],
[Burrow transport decision](https://github.com/Bochner/burrow/issues/19).

## Hovel handoff and ownership

Inspection uses Burrow's pinned Hovel revision
`c461ba282a8aecc7aa3a079a4613bf5e2640c388`, not a claim about an unpinned latest
daemon. Public `StepProvider` exposes prepare, execute and cleanup, with typed
`PayloadArtifact`, `PayloadInstance`, `TransportEndpoint` and `CleanupHandle`
capabilities. These are suitable seams for a future provider and Burrow consumer;
their existence does not prove that the current consumer binds them automatically.
[Public steps][step].

Hovel's versioned payload API distinguishes ELF execution from other load
contracts and permits providers to advertise only supported operations. Its
installed-payload record stores provider-owned reconnect/cleanup descriptors;
Hovel does not infer successful installation by probing the target. Therefore
publish an installed Dropbear instance only after launch and readiness evidence,
with a non-secret endpoint and cleanup reference. Ordinary SSH connections to
pre-existing servers remain connection records. [Versioned payload API][payloadv1],
[installed-payload ownership][payload].

Recommended sequence for the later proof:

1. Prepare a reviewed target/account, artifact digest, staging directory, bind,
   port, authentication public key and cleanup plan. Use Hovel's normal exact-plan
   confirmation and launch-policy path; shelling out directly is not proof of a
   confirmed Hovel throw. [Hovel throw workflow][throw].
2. Use an already authorized bootstrap execution/delivery capability to stage and
   start the fixture. Dropbear supplies no initial access. Capture startup errors
   separately from SDK JSON-RPC stdout and report correlated diagnostics through
   the SDK. [Module context][context].
3. Return the observed SSH endpoint and host public key over the trusted bootstrap
   channel. Verify the key through Burrow's normal host-trust path, then establish
   a real named shell-free OpenSSH master. An unauthenticated network key scan is
   discovery, not independent identity verification. Preserve unknown-host review
   and changed-key rejection required by [#45](https://github.com/Bochner/burrow/issues/45).
4. Confirm reachability from the Burrow execution side. A loopback target bind
   needs an existing approved route; a LAN bind needs matching network policy.
   Publishing a `TransportEndpoint` is not NAT traversal or a callback. Later
   tunnel demonstrations must explicitly enable and test forwarding.
5. Assign remote listener/process/file ownership to the deploying provider and
   local master ownership to Burrow. Closing Burrow alone must not imply remote
   uninstall. Keep a bootstrap cleanup path independent of the SSH connection
   being removed, and report uncertainty if that path is lost.

The cleanup proof must include accepted client children as well as the listening
parent. Dropbear forks and creates session process groups; a parent PID or one
process-group kill is insufficient evidence of full teardown. For a disposable
lab, container/cgroup ownership is a useful containment boundary. Remote cleanup
must validate ownership before removing the run directory, then observe that
listener and sessions are gone. Keep public evidence; remove private host keys
and temporary authorization material. [Server process lifecycle][main],
[provider cleanup contract][payload].

## Relationship to the disposable SSH lab

The owner confirmed OpenSSH Docker remains #45's disposable lab. Add Dropbear
later as an optional variant using the same production-path acceptance runner. Containers
help provision isolated users and PTYs; a foreground Dropbear process can also
serve a small unprivileged Linux test without Docker. Neither format alone proves
reproducibility: pin the server/build inputs, generate unique keys per run, bind
locally, and clean up owned processes. These are recommendations, not additions
to #45's implementation scope.

## Placement in the MVP cycle

The earliest useful insertion is **late MVP 1, after #45's OpenSSH gate works**:
run the same connection/trust/loss/close checks against a pinned Dropbear target.
This is additional server interoperability evidence, without changing Burrow's
OpenSSH client engine or introducing deployment functionality. It is a proposed
extra check, not an existing mandatory acceptance item. Keep it separate from
#45 completion unless the owner explicitly adds it to that ticket.
[Connection acceptance](https://github.com/Bochner/burrow/issues/45),
[five-milestone specification](https://github.com/Bochner/burrow/issues/43).

| Stage | Useful Dropbear coverage | Boundary |
| --- | --- | --- |
| MVP 1: #45–#46 | Key/agent authentication, host trust, retained master and loss/close; later selected password/config cases. | Optional second server fixture. No throw-based installation. |
| MVP 2: #48–#52 | Target PTY viability and actual L/R/SOCKS interoperability. | Enable forwarding deliberately for these cases; do not assume the connect-only configuration permits it. |
| MVP 3: #53–#56 | Missing-SFTP error reporting, then byte-correct transfers with an explicitly packaged/configured SFTP server. | A bare Dropbear binary cannot pass the full file-workflow matrix. |
| MVP 4: #57–#62 | Commands and explicitly staged scripts on a Dropbear-backed connection; compatible chains consuming already-established tunnels. | #58 supplies staging primitives; #62 explicitly forbids consumer provisioning/reconnect. Neither ticket implements a deployment provider. |
| MVP 5: #65 | Include whatever Dropbear combinations actually passed in compatibility documentation. | Avoid advertising untested platforms or full feature parity from a connection smoke test. |

Stage numbers and ticket grouping come from the
[MVP specification and child tickets](https://github.com/Bochner/burrow/issues/43).
The explicit staging and existing-tunnel limits are in
[#58](https://github.com/Bochner/burrow/issues/58) and
[#62](https://github.com/Bochner/burrow/issues/62); final coverage and remaining
exclusions are part of [#65](https://github.com/Bochner/burrow/issues/65).

**The “throw → deploy Dropbear → Burrow connects” product capability belongs in
the first post-MVP chaining slice**, after the existing connection, transfer,
script and confirmation paths are complete. #43 expressly excludes automated
provisioning and the callback handoff. Pulling the provider into MVP 4 would
change that approved scope and needs a specific new decision/ticket; the user's
request here is to investigate placement, not silently rewrite #62. No tracker
changes were made. [Scope exclusions](https://github.com/Bochner/burrow/issues/43).

Before claiming the later chain works, run one controlled Linux demonstration of
confirmed delivery → readiness → verified Burrow connection → explicit close →
provider cleanup, including wrong key, changed host key, occupied port, rejected
account, provider loss, and surviving-child checks. Measure binary size and test
static portability, PTY and SFTP separately. This research fetched and inspected
sources and hashed an archive; it did not build Dropbear, contact a lab host,
exercise Hovel deployment or establish any of those runtime outcomes.

[site]: https://matt.ucc.asn.au/dropbear/dropbear.html
[tag]: https://github.com/mkj/dropbear/tree/DROPBEAR_2026.94
[install]: https://github.com/mkj/dropbear/blob/28216cd9af822732a1549b78621c2ea9a76a0fe5/INSTALL.md
[small]: https://github.com/mkj/dropbear/blob/28216cd9af822732a1549b78621c2ea9a76a0fe5/SMALL.md
[multi]: https://github.com/mkj/dropbear/blob/28216cd9af822732a1549b78621c2ea9a76a0fe5/MULTI.md
[license]: https://github.com/mkj/dropbear/blob/28216cd9af822732a1549b78621c2ea9a76a0fe5/LICENSE
[readme]: https://github.com/mkj/dropbear/blob/28216cd9af822732a1549b78621c2ea9a76a0fe5/README.md
[manual]: https://github.com/mkj/dropbear/blob/28216cd9af822732a1549b78621c2ea9a76a0fe5/manpages/dropbear.8
[auth]: https://github.com/mkj/dropbear/blob/28216cd9af822732a1549b78621c2ea9a76a0fe5/src/svr-auth.c
[pubkey]: https://github.com/mkj/dropbear/blob/28216cd9af822732a1549b78621c2ea9a76a0fe5/src/svr-authpubkey.c
[options]: https://github.com/mkj/dropbear/blob/28216cd9af822732a1549b78621c2ea9a76a0fe5/src/default_options.h
[channel]: https://github.com/mkj/dropbear/blob/28216cd9af822732a1549b78621c2ea9a76a0fe5/src/svr-chansession.c
[main]: https://github.com/mkj/dropbear/blob/28216cd9af822732a1549b78621c2ea9a76a0fe5/src/svr-main.c
[step]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/step.go
[payloadv1]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/payload_v1.go
[payload]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/payload.go
[context]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/context.go
[throw]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/agent/skills/hovel-throw/SKILL.md
