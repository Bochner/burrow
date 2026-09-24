# Follow-up: publish a verified Linux amd64 archive

Identified by #92 on 2026-09-23. This is a local follow-up specification, not
authorization to publish a release or change repository visibility.

The v0.1.0 GitHub release targets
`0515137b76b86e54bdccd40eada479bca5243eeb` and its `assets` list is empty.
An authorized source checkout can produce `package.tgz`; a prospective operator
cannot download a prebuilt Burrow archive. The book now documents that boundary.

Deliver one Linux amd64 archive from an owner-selected release commit:

- Build the existing `//core/cmd/burrow:package` target through Aspect and pass
  the required release gates for that exact commit, retaining the #86 exception
  explicitly and the separate #65 owner acceptance requirement.
- Give the archive a versioned name; provide SHA256, source commit, platform,
  bundled contents and pinned Hovel provenance with the release.
- Verify extraction into a fresh operator-owned directory, packaged `--help`,
  managed first launch, cached offline launch and bundled skill installation
  using disposable local state. Preserve installation/upgrade refusal behavior.
- After separate owner publication approval, attach the archive and checksum
  to the intended release. Verify access using the intended audience's access
  level; repository privacy does not establish public download availability.
- Replace the book's source-only statement with the exact verified asset URL,
  checksum and extraction/update steps. Keep source builds documented.

No package-manager integration, automatic updater, additional platform or
visibility change is required by this follow-up. It remains unimplemented.
