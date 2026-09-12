# Throwaway SSH transport proof

The investigation below records historical pins and observations. The accepted
transport is ordinary OpenSSH multiplexed subprocesses with Go SFTP over subsystem
pipes. Current dependencies and new live-server validation are recorded in the
[terminal design reference](../../docs/research/terminal-design.md). Run
`aspect burrow-prototype live -- HOST USER` for that optional authenticated check;
its password is read by OpenSSH and its dedicated remote scratch files are removed.

Question: [Can OpenSSH master sockets and Go channels satisfy the transport contract?](https://github.com/Bochner/burrow/issues/19)

**Observed answer: not safely with these pins. Owner decision pending.** Go's
control-socket bridge works for successful commands, PTYs and SFTP, but a rejected
channel leaves the OpenSSH master vulnerable when that bridge disconnects. Go
remote-listener requests time out. Ordinary OpenSSH multiplexed subprocesses
pass the comparison checks. Hovel's public session boundary separately lacks
terminal resize. This is evidence, not production implementation or approval.

## Run

```sh
aspect burrow-prototype transport
aspect burrow-check
# Optional authorized VM, interactive password; existing known-host trust required:
aspect burrow-prototype lab -- <host> <user>
```

The local fixture requires the exact Ubuntu OpenSSH binaries whose SHA-256 pins
are in `check.py`, Python via the declared toolchain, and a writable owner-only
home cache directory. It launches an unprivileged loopback sshd with temporary
keys/configuration, an isolated password-only Go server, and a temporary Hovel
daemon. It does not change SSH configuration, accounts, services, or known hosts.
The OpenSSH binaries and their system libraries are host prerequisites, **not a
hermetic OpenSSH toolchain**. No claim is made about a general installation matrix.

The optional VM probe uses the operator's existing known-host entries and asks
OpenSSH to read the password from the terminal. It never records credentials.
It runs harmless commands and a temporary in-memory Python socket fixture;
no remote files are created. It deliberately terminates only its own temporary
master to reproduce the failure. It must not be pointed at an existing master.

## Pins and provenance

- Go 1.26.5, `rules_go` 0.61.1, Gazelle 0.51.3: inherited archived SDK proof.
- `golang.org/x/crypto` v0.53.0; `github.com/pkg/sftp` v1.13.10 and its
  `github.com/kr/fs` v0.1.0 dependency: module checksums from Go's checksum database.
- Local OpenSSH client/server: `10.2p1 Ubuntu-2ubuntu3.6`, OpenSSL 3.5.5.
  SHA-256 checks cover ssh, sshd, ssh-keygen, ssh-agent and ssh-add.
- Hovel SDK `c461ba282a8aecc7aa3a079a4613bf5e2640c388` via unchanged source and
  inherited BUILD overlay; published Hovel v0.4.2 Linux amd64 wheel digest
  `7edccdabe04e30098e417d27ab48064c11a8612149124b7df0797251e88a0933`.
- Local host: Linux `6.18.33.2-microsoft-standard-WSL2`.
- Supplemental Ubuntu VM: Linux `6.8.0-139-generic`, reported OpenSSH
  `9.6p1 Ubuntu-3ubuntu13.19`. The master and Go client remained on WSL2;
  the VM is an independent server-side check, not a native Linux client test.

## Observations

| Check | Observed result |
| --- | --- |
| Host trust | Unknown key rejected; deliberate trust of generated fixture key succeeds; changed key rejected. |
| Authentication | Password, encrypted-key and agent authentication; cancellation and unavailable agent fail. Password master uses a fixed-response Go SSH server, without host account/PAM changes. |
| Aliases/jumps | Config alias and ProxyJump pass; jump and target use separate transports to the controlled server. |
| Socket boundary | Owner-only named path displayed; permissive socket and symlink rejected; collision preflight rejects existing name and leaves connection usable. |
| Concurrency | External OpenSSH command and Go command share identical server-observed `SSH_CONNECTION`; external shell exit retains master. |
| SSH terminal | Two PTY open/resize/close cycles each verify remote `stty size` at 80x24 then 120x40; also passed against VM. |
| SFTP | Browse, upload/download and byte comparison of 983,040 bytes; explicit cancellation leaves 30,720 acknowledged bytes marked partial. |
| Subprocess SFTP | Same Go SFTP operations pass using `NewClientPipe` over `ssh -S ... -s target sftp`, without `NewControlClientConn`. |
| Successful Go forwarding | `direct-tcpip` echo succeeds. This alone does not establish safe forwarding lifecycle. |
| Refused Go forwarding | Bound-but-not-listening fixture port returns a Go error promptly. Closing the bridge then makes OpenSSH exit 255 with `chan_send_close2: ... no remote_id`. Reproduced against VM. |
| Go remote listener | `Client.Listen("tcp", "127.0.0.1:0")` does not return within the 8-second probe bound. Reproduced against VM. |
| Ordinary OpenSSH forwarding | Local, remote and SOCKS data exchange and independent removal pass; parent remains usable. Local/remote bind failures are visible. Normal `ssh -W` refusal preserves master. VM confirms normal refusal and remote-listener allocation/removal. |
| Hovel boundary | Actual packaged public SDK session through published daemon: open, read/write, frontend request disconnect/reconnect, close/reopen; initial remote PTY geometry 80x24. No public resize method. |
| Loss | External `ssh -O exit` causes transfer write/bridge Wait errors and the active Hovel session becomes closed. Missing socket cannot silently create a fresh login. |
| Cleanup | Fixture processes are terminated/reaped and temporary local resources removed. |

The full prototype gate checks these observations, including the **expected
upstream failures**. A green gate is not a successful transport acceptance verdict.

## Failure explanation and comparison

At OpenSSH V_10_2_P1, proxy channels receive a remote ID only after open
confirmation. A refused open is forwarded to the Go client, but the proxy state
is not retired by normal failure cleanup. Disconnecting the proxy converts its
remaining channels into ordinary open channels; closing one without a remote ID
hits OpenSSH's fatal check. This also exposes pending-open cancellation. By source
inference the same rejected-open problem applies to session channels, so limiting
Go to shells/SFTP would not eliminate the class of failure. Rejection of an
exec/subsystem request on an already-open channel is a different case.

Sources: [proxy teardown](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/channels.c#L772-L784),
[proxy state updates](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/channels.c#L3374-L3434),
[normal failure cleanup bypass](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/channels.c#L3689-L3722),
[fatal condition](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/nchan.c#L206-L225).

Go remote listen waits for a global reply. The proxy path records and forwards
`tcpip-forward` without registering a global-confirm callback; the master drops
the reply when its callback queue is empty. That path also rejects
`cancel-tcpip-forward`. A timed-out request can therefore have created a remote
listener whose allocated port the caller never learns; closing that fixture's
entire master is the cleanup boundary used here.

Sources: [Go listen](https://github.com/golang/crypto/blob/v0.53.0/ssh/tcpip.go#L125-L150),
[proxy global requests](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/channels.c#L3289-L3316),
[global reply dispatch](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/clientloop.c#L471-L483).

Normal multiplexed session/forward commands use different request paths, with
explicit refusal handling and forward confirmation callbacks. The smallest
demonstrated alternative is to keep the OpenSSH master and use its subprocess
interfaces: shell/exec and forwarding controls, with structured Go SFTP over
the subsystem process's pipes. This preserves user-named master sockets without
adding a second native-authentication runtime.

Sources: [normal session setup/refusal](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/mux.c#L1348-L1400),
[normal forwarding confirmation](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/mux.c#L809-L825).

An early experiment used destination port 1, which stalls under the local
environment. Its hang is **not** evidence of mishandled connection refusal.
The retained runnable refusal test reserves a high port without listening;
both local and VM clean refusals reproduce the master teardown defect.

## Limits and next decision

- Recommend replacing the preferred Go control-proxy direction with ordinary
  OpenSSH multiplexed subprocesses, pending owner feedback. No upstream fix or
  newer-version search is implied by this bounded, pinned proof.
- Hovel resize still needs a supported frontend-to-module operation. Fixed initial
  sizing and transport-only resize do not satisfy end-to-end terminal parity.
- This is not a production owner/supervisor: the harness starts the master and
  the module attaches to it. Atomic multi-process naming, daemon restart recovery,
  inventory of externally created resources and final paths belong to the
  [live-state ownership decision](https://github.com/Bochner/burrow/issues/11).
- Cancellation is exercised between acknowledged file chunks and loss before the
  next write. Arbitrarily stalled I/O, throughput, huge directories, production
  buffering, installer portability and a complete subprocess frontend remain
  outside this proof. Direct external socket commands do not pass through Hovel
  audit/confirmation. Hovel-mediated execution uses its existing run path.
- The Go session adapter has a bounded 64-chunk output queue for this fixture;
  production retention/backpressure policy remains undecided.

The source is based on `archive/prototype-hovel-setup` and now lives on `main`
with the later ownership and script-boundary proofs. Historical observations
above retain their original scope; consult the linked decisions for acceptance.
# Existing-file collection check

`aspect burrow-prototype live -- HOST USER /absolute/file [/absolute/another-file]`
also collects the selected existing files into temporary local storage, displays
measured byte/rate/time/ETA progress and verifies remote/local SHA-256. It follows
symlinks for size metadata to match SFTP copying. Source files are left untouched;
temporary downloads and test resources are removed on exit. Existing trusted host
keys and terminal authentication are required. The owner-authorized large-file
result is recorded in [MVP coverage](../../docs/research/terminal-mvp-coverage.md).
