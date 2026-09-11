# Security Policy

## Authorized use

Burrow is intended for homelab management, authorized security testing,
adversary emulation, controlled lab research, and defensive validation.
Use it only on systems you own or have explicit written authorization to assess,
and remain within the agreed scope.

The maintainers do not condone unauthorized access to systems or data.

## Safety model

Burrow currently has no SSH runtime. Implementation must validate remote inputs,
verify SSH host keys, keep credentials out of source control and diagnostic
output, and preserve Hovel's supported confirmation and audit contracts.
These are requirements for the upcoming implementation, not delivered safeguards.

## Reporting a vulnerability

Report vulnerabilities in this repository privately through
[GitHub security advisories](https://github.com/Bochner/burrow/security/advisories/new).
Include the affected commit, reproduction steps with synthetic data, and any
suggested remediation. Do not include credentials or private target data.
Report vulnerabilities in third-party systems to their owners.

## Supported versions

Burrow is in specification planning. There are no supported runtime releases.

Adapted from Hovel's policy; see [upstream provenance](docs/site/UPSTREAM.md).
