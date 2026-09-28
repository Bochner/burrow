# Changelog

## [0.2.2] - 2026-09-28

### Changed

- Automatically tag and publish the tested GitHub release bundle after a version
  increase passes main CI. Use this changelog for release notes; PyPI remains manual.
- Run setup and terminal acceptance once per full gate, with repeated runs reserved
  for explicit troubleshooting. Execute the six production coverage targets once
  and use those same results for behavior evidence.
- Keep report behavior checks in one process with an external CLI smoke check.
- Reuse verified PR evidence on main, adopt Hovel's report layout and enforcement,
  and refresh the README's installation and development guidance.

### Verification

- Full uncached Aspect preflight: all 53 targets passed, including nine SSH
  partitions, three Hovel compatibility checks and measured production coverage.
- Release guards cover unchanged/increased/decreased versions, missing notes,
  conflicting tags and promotion from the exact successful main run.

## [0.2.1]

Earlier release notes: [v0.2.1](https://github.com/Bochner/burrow/releases/tag/v0.2.1).
