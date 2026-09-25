# Verified Linux amd64 release archive

Identified by #92 on 2026-09-23. The owner authorized merge, Pages and a new
tagged development release on 2026-09-25. v0.2.0 uses the existing package target;
repository visibility stays unchanged and #65's owner walkthrough is deferred.

The v0.1.0 GitHub release targets
`0515137b76b86e54bdccd40eada479bca5243eeb` and its `assets` list is empty.
For v0.1.0, an authorized source checkout can produce `package.tgz`, but that
release has no prebuilt Burrow archive. v0.2.0 addresses this distribution gap.

Deliver one Linux amd64 archive from an owner-selected release commit:

- Build the existing `//core/cmd/burrow:package` target through Aspect and pass
  the required release gates for that exact commit, retaining the #86 exception
  explicitly and the separate #65 owner acceptance requirement.
- Give the archive a versioned name; provide SHA256, source commit, platform,
  bundled contents and pinned Hovel provenance with the release.
- Verify extraction into a fresh operator-owned directory, packaged `--help`,
  managed first launch, cached offline launch and bundled skill installation
  using disposable local state. Preserve installation/upgrade refusal behavior.
- Under the recorded owner approval, attach the archive and checksum
  to the intended release. Verify access using the intended audience's access
  level; repository privacy does not establish public download availability.
- Replace the book's source-only statement with the exact verified asset URL,
  checksum and extraction/update steps. Keep source builds documented.

The v0.2.0 assets are `burrow-v0.2.0-linux-amd64.tar.gz` and `SHA256SUMS`.
The release notes identify the successful main commit, checks and actual archive
digest. The launch guide links those exact assets and documents extraction and
deliberate upgrades. Publishing is contingent on the required main gate;
documentation alone is not proof that the assets have been uploaded.

No package-manager integration, automatic updater, additional platform or
visibility change is required.
