# SCP browser listing metadata

Research date: 2026-09-13. Evidence and recommendation for
[#53](https://github.com/Bochner/burrow/issues/53), not an implementation decision.
Context7 was consulted first for GNU findutils/coreutils; pinned source and
OpenSSH protocol documentation resolve the protocol details.

## Owner requirements and accepted boundaries

The owner wants remote listings resembling `ls -latr`, with eza-like formatting
using Burrow's Charm presentation standard. The owner subsequently clarified
that hard-link count is unimportant: omit Links, preserving Permissions, Owner,
Group, Size, Modified and Name. Numeric-only ownership has **not** been accepted
as the normal replacement. Scope stays at #53 browsing,
workspace roots, navigation and completion; transfers belong to #54–#56.

The owner accepted `name → target` for symlinks, explicit remote `cd` through
directory links, no traversal of directory links during recursive `tree`, and
local link traversal only when the resolved target stays inside the selected
effective root. Broken and escaping local links remain visible but cannot be
traversed. `local download PATH` / `local upload PATH` persist workspace roots;
`lcd [download|upload] PATH` navigates within the selected root and
`lls [download|upload] [PATH]` lists it, defaulting to download.

The accepted transport remains ordinary OpenSSH multiplexed subprocesses and
Go SFTP over subsystem pipes. Metadata enrichment through another channel on
that master does not replace the transfer protocol. The owner accepted clearly
labelled reduced metadata when remote enrichment is unavailable. See the
[transport decision](https://github.com/Bochner/burrow/issues/19#issuecomment-5639082720).

## What the sources establish

LazySSH's `cmd_ls` actually runs `ls -la`, splits output into lines and then nine
whitespace-separated columns, and reconstructs dates from human text. This is
the field/presentation reference, not a safe filename parser: embedded newlines,
locale-dependent dates and device entries do not fit that assumption. The
owner's `-tr` request adds modification-time ordering beyond that implementation.
Source: [pinned LazySSH scp_mode.py](https://github.com/Bochner/LazySSH/blob/9eb84452/src/lazyssh/scp_mode.py),
`cmd_ls`, locally traced at lines 1261–1365.

SFTP v3 provides structured size, numeric UID/GID, permissions, and access and
modification timestamps. Its standard attributes contain neither account names
nor hard-link count. Its human-readable `longname` is explicitly unsuitable for
client parsing. `LSTAT`, `READLINK` and `REALPATH` support link inspection and
canonicalization without deriving behavior from rendered rows.
Source: [SFTP v3 draft, sections 5, 6.8–6.11 and 7](https://www.openssh.org/txt/draft-ietf-secsh-filexfer-02.txt).

The pinned `pkg/sftp` v1.13.11 exposes those attributes as `FileStat`, including
second-resolution `Mtime`, but no `Nlink`. `ReadDirContext` discards `longname`
and excludes `.` and `..`; it supports `NewClientPipe`, `Lstat`, `ReadLink` and
`RealPath`. `HasExtension` reports server capabilities, but the client has no
public users/groups lookup method or generic public extended-request sender.
Sources: [attrs.go](https://github.com/pkg/sftp/blob/v1.13.11/attrs.go),
[client.go](https://github.com/pkg/sftp/blob/v1.13.11/client.go).

OpenSSH's `users-groups-by-id@openssh.com` extension resolves batches of numeric
IDs into names when advertised; unresolved IDs produce empty names. It does
not add link counts. `hardlink@openssh.com` creates a hard link; it does not
report their count. `statvfs` reports filesystem capacity, not per-file link
counts. Thus an extension alone does not recover the complete table.
Source: [OpenSSH PROTOCOL, sections 4.4, 4.5 and 4.12](https://github.com/openssh/openssh-portable/blob/master/PROTOCOL).

`scp` copies files; it is not a structured directory-query API. Current OpenSSH
`scp` uses SFTP by default, so switching the executable would not solve this
metadata gap. Source: [OpenBSD scp manual](https://man.openbsd.org/scp).

GNU `find -printf` can emit separate NUL-terminated fields for basename (`%f`),
type/mode (`%y`, `%m`), hard-link count (`%n`), numeric and named ownership
(`%U`, `%u`, `%G`, `%g`), bytes (`%s`), precise modification time (`%T@`), and
link target (`%l`). Named ownership falls back to IDs when unknown. With piped
output, filenames are emitted without terminal-oriented quoting. `-P` avoids
following links; `-mindepth 1 -maxdepth 1` restricts enumeration to children.
These features require compatible remote findutils, not merely SSH or POSIX.
Source: [GNU Findutils manual](https://www.gnu.org/software/findutils/manual/text/find.txt).

GNU `stat --printf` can similarly expose explicit metadata, but it does not
enumerate a directory; batching paths or combining it with `find` adds machinery
when GNU `find` already supplies the fields. `ls -t` puts newest modification
times first; `-r` reverses that, so `ls -latr` means oldest first. `-a` also
includes `.` and `..`, unlike `pkg/sftp.ReadDir`. Sources:
[GNU stat](https://www.gnu.org/software/coreutils/manual/html_node/stat-invocation.html),
[GNU ls](https://www.gnu.org/software/coreutils/manual/coreutils.html#ls-invocation).

GNU/glibc `getent passwd` and `getent group` accept multiple numeric keys and
resolve them using the remote host's configured NSS services. No keys means
enumeration, so skip empty batches. Status 2 means one or more keys were absent;
other returned matches can still be useful. This avoids reading only
`/etc/passwd`, which would miss configured directory services. Source:
[Linux getent manual](https://man7.org/linux/man-pages/man1/getent.1.html).

## Recommended implementation

With link count removed, retain SFTP for the complete file listing, navigation,
completion and subsequent transfers. Resolve only distinct UID/GID values with
bounded batches of remote `getent passwd` / `getent group` queries through the
verified existing master. Parse only validated ID/name fields and discard the
remaining account fields without logging raw output. Cache names only within
that connection with a bounded lifetime/refresh policy, never across unrelated
hosts. OpenSSH's SFTP extension is a cleaner future option if the pinned public
client API gains support; it is not a reason to fork the dependency now.

Generate lookup arguments from parsed unsigned decimal IDs, with fixed database
names; filenames and operator shell text never belong in the command. Use no
PTY, separate stderr, cancellation and bounded output. Check returned IDs
against the requested set, reject malformed records and sanitize names at the
presentation boundary. This is a recommendation, not an implemented check.

GNU `find`/`stat` remain technically valid alternatives but duplicate metadata
that SFTP already provides for the revised requirement. A remote helper would
add deployment and platform maintenance. Neither is needed solely to omit the
link-count column. No remote eza installation is needed; its role here is a
presentation reference.

Keep symlink inspection and navigation in SFTP, independently of presentation.
A listing is a changing observation, not an atomic snapshot or authorization
for later transfers; revalidate at use time. Remote exec and custom SFTP
subsystems can expose different identity namespaces; names must remain display
metadata and never change the numeric IDs or authorize operations.

## Subsequent owner decisions

- Allow browsing with clearly labelled reduced metadata when enrichment is
  unavailable; retain numeric UID/GID and do not pretend those are names.
- Sort oldest modified first, newest last. Include hidden entries, omitting
  only `.` and `..`. The owner explicitly selected that omission.
- Avoid bombarding the remote server with background discovery commands. The
  owner recalled LazySSH repeatedly issuing `find` during SCP population and
  requested restrained remote work.

Implement automatic completion using cached directory observations, debouncing,
coalescing and a single automatic request in flight per connection. Throttle
cache misses, including Tab; explicit Tab must not force a fresh remote query
on every press. Do not poll directories or recursively prefetch. Reuse explicit
listing results for completion and scope caches by workspace and connection
creation. Batch/cache account lookups; never enumerate passwd/group databases
when there are no missing IDs. Explicit `ls` refreshes and operator-requested
`tree` walks may fetch fresh data; cancellation and incomplete outcomes remain
visible. Extend behavior checks with remote request counts under sustained
typing/Tab, slow replies, repeated visits and multiple views of a connection.

Cache lifetimes and rate-limit values are implementation choices, not a newly
approved performance guarantee. The acceptance criterion is bounded automatic
remote work and responsive, correct completion without an idle request stream.

Do not infer Linux remote hosts from the accepted Linux **operator** platform.
BSD/macOS, BusyBox and SFTP-only endpoints cannot be assumed to provide the
same name-lookup command contract. If those hosts require names now, scope a
tested adapter explicitly rather than accumulating speculative fallbacks.
