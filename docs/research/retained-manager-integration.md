# Production retained-manager integration — issue #73

Implementation evidence, not a replacement for owner acceptance. The accepted
contract is [#73](https://github.com/Bochner/burrow/issues/73) and
[ADR 0001](../adr/0001-manual-connection-approval.md). The owner accepted the
#71/#72 proof and, during implementation, explicitly requested removal of the
obsolete known-hosts setting from the program.

## Operator behavior

The single base `burrow@0.1.0` module retains one manager per workspace. Cold
connect activates it through a real confirmed throw; each cold or warm connect
then uses its own immutable request chain and confirmed throw. List and selected
close use bounded public session commands. Activation alone grants no connection
approval. Session controls are privileged same-user APIs, not signed proof of
approval. Workspace, generation, session, creation, exact request/config and run
correlation are checked; ambiguous outcomes require inspection, not retries.

The existing CLI and Huh frame share the exact command/config recap and one
manual approval. Long recaps scroll without hiding confirmation. Explicit keys,
current default keys/config/agent, password fallback, encrypted keys and jumps
use the existing private authentication transport. Every hop follows the chosen
LazySSH host policy; no extra host-trust prompt is presented.

The known-hosts form field, `--known-hosts`, `--trust`, and the `knownHosts` saved
setting were removed. Old collections containing that field are rejected by the
existing strict schema; remove the obsolete field before loading. Burrow does
not rewrite or delete old collections or known-host records. The exact SSH recap
still shows `UserKnownHostsFile=/dev/null`, because it is an actual execution
option, not an editable saved setting.

Earlier per-connection owners remain separate and inspectable/closeable; they
are never interpreted as managers. Frozen pre-integration sources at
`17b1746c4c730ee596954bcf20ff7a65bd466f86` keep historical proof comparisons
runnable. The transition fixture uses their uninstrumented behavior for both
earlier catalog identities. Manager loss shares the workspace failure domain;
unidentified reservations are preserved. Quit supports keep, verified close
and cancel across opened workspaces. No new generic Mesh consumer, tunneling
capability, public module, daemon or dependency is included; #62 remains separate.

## Reproduce

```sh
aspect burrow-check
aspect burrow ssh-latency
aspect run --run-in=./bazel-burrow //core/cmd/burrow:connection_lab -- --prompt-check
```

All SSH checks use declared LinuxServer OpenSSH amd64 image digest
`sha256:47f82202bcbffe214cea77dc7f5ed24b875e81ce6ebf0e0368e2954727bf5116`,
Hovel v0.4.2 at `c461ba282a8aecc7aa3a079a4613bf5e2640c388`, temporary workspaces,
random loopback ports and synthetic credentials. Operator servers and keys are
not used. Private SQLite audit evidence is read only after daemon shutdown.

## Validation

`aspect burrow-check` passed: all 18 portable tests, production SSH/PTY acceptance
(263.2 seconds), retained-manager proof (60.7 seconds), and retained-consumer proof
(51.2 seconds), plus the declared production/proof package builds and book checks.
The production lab verified independent frontends, immutable recap binding,
default and tilde-expanded keys, both earlier-owner identities, authentication
and cancellation, saved profiles, unsafe-resource refusal, reconnect, cross-workspace
quit, EOF/process loss, real throw evidence, and 200 shared logs plus one warning.

With the private passphrase still unanswered, full production CLI list completed
in 0.702 seconds and selected sibling close in 0.980 seconds; the original sibling
remained connected. The focused `--prompt-check` run measured 0.795/0.981 seconds.
These include CLI and identity-verification overhead. The check bounds completion
at five seconds while withholding the secret, rather than reinstating the retired
one-second SLA or measuring an animation. The historical proof keeps its own
documented measurement envelope.

Presentation checks verify final VT cell roles for generated config numbers,
paths and booleans, unchanged NO_COLOR text, visible approval controls and complete
scroll access. Actual captures were inspected at 160x40 and 200x50 (empty and
populated), 120x30 and 80x24, including recap top/end and the form without the
removed field. Real guided CLI input also passed with `--no-color` and no initial
NO_COLOR environment variable. Standards and Spec reviews have zero outstanding
findings after the recap color correction.

Final owner acceptance of the
integrated application remains pending; acceptance of the setting removal alone
is not recorded as acceptance of the whole ticket.

## Production phase measurements

Measured on 2026-09-12 with `aspect burrow ssh-latency`; all 40 untraced samples
and four separate process traces completed. [Raw samples and provenance](retained-manager-integration-measurements.json)
record the actual binary SHA-256
`f5d544c0e1e44263547b408df54b215174407579597bd792279c9d2f3d395ff7`.
Build mode: Aspect fastbuild. Machine: Linux WSL2 6.18.33.2 x86_64, glibc 2.43;
initial load averages 2.21/3.90/3.30. This is a shared development machine, not a
dedicated performance host. No Burrow builds ran during sampling.

Each pair starts with a newly verified daemon and installed packages. Cold
submission includes manager activation; warm submission reuses that manager.
The CLI receives explicit `--yes`, so human recap-reading time is excluded.
Both submission and owner phases use CLOCK_MONOTONIC. Password samples observe
the first private PTY prompt with echo disabled and supply a synthetic answer
after 20 milliseconds; real human/network authentication times will differ.
Setup is recorded separately in every sample, not folded into dispatch.

| Phase, seconds | Cold p50 | Cold p95 | Warm p50 | Warm p95 |
| --- | ---: | ---: | ---: | ---: |
| Submission to owner dispatch (20 each) | 5.236 | 5.253 | 3.720 | 3.735 |
| Submission to observed connected (20 each) | 5.475 | 5.584 | 3.953 | 4.065 |
| Owner dispatch to private password prompt (10 each) | 0.219 | 0.223 | 0.220 | 0.222 |

Separate traced runs counted successful `execve` starts across the frontend and
daemon descendants, including observer-induced SSH checks. There is one trace
per temperature/authentication combination, not a statistical distribution:

| Authentication | Cold starts | Warm starts |
| --- | ---: | ---: |
| Key | 18 | 14 |
| Password | 22 | 18 |

Tracing inflated submission-to-connected to approximately 18–24 seconds; those
times are deliberately excluded from the latency table. CLI-return stage times
from `aspect test` are also a different envelope from these standalone runs.
Neither the old proof's accepted 38% improvement nor older CLI-return baselines
are treated as a newly measured production comparison.

Most measured delay precedes owner dispatch. That interval includes Burrow's
repeated workspace/daemon verification, SSH-config evaluation, module inventory,
request-record creation, confirmed Hovel throw and adapter startup. These data
do not separate each cost or prove that Hovel IPC itself is the bottleneck.
Startup reuse versus focused per-operation verification remains a profiling and
design follow-up; no checks or approvals were removed based on that hypothesis.
