# External Hovel Go SDK consumption

Research date: 2026-09-11. Hovel baseline and verified current default-branch head: `c461ba282a8aecc7aa3a079a4613bf5e2640c388`. This resolves the source investigation in [How can Burrow consume the public Hovel SDK externally?](https://github.com/Bochner/burrow/issues/2); it does not claim an external build or installation has passed.

Subsequent evidence: the [external SDK/package proof](prototype-evidence.md)
demonstrated the unchanged-source BUILD overlay and inert package/session lifecycle.
The candidate wording below records the earlier investigation; consult the linked
proof and decision before repeating work or selecting the full-source dependency.

## Finding

Hovel explicitly supports out-of-tree module packages and exposes a public Go SDK. Its source distribution is not a demonstrated standalone Go-module distribution. The smallest candidate integration is a pinned source dependency exposing its public Bazel SDK target; if the full Hovel workspace proves unsuitable, use an unchanged-source archive with a minimal Burrow-owned BUILD overlay. Both candidates still require an external build/install proof. This is a recommendation, not acceptance of an untested dependency recipe. [Module development][development], [SDK README][sdk], [SDK BUILD][build].

## Exact public boundary

| Item | Value at the pin |
| --- | --- |
| Go import | `github.com/vibepwners/hovel/sdk/go/hovel` |
| Upstream Bazel target | `//sdk/go/hovel:hovel`, public visibility |
| Upstream Bzlmod module | `hovel_slices`, declared version `0.2.4`; not proof of registry publication |
| Go SDK external library dependency | `golang.org/x/sys/unix` on Linux/Android; upstream pins `golang.org/x/sys v0.47.0` |
| Build baseline | Bazel `9.1.1`, Aspect CLI `2026.33.3`, Go toolchain `1.26.5`, language `1.26.0`, rules_go `0.61.1`, Gazelle `0.51.3` |
| Module implementation | `Info()`, `Schema()`, `Run(*hovel.Context)` and `hovel.Serve(module)` |
| Protocol | Content-Length-framed JSON-RPC over stdin/stdout; progress through `ctx.Log` |

Sources: [SDK BUILD][build], [root module][module], [core Go dependencies][gomod], [Bazel version][bazelversion], [Aspect version][aspectversion], [SDK README][sdk]. No `core/internal` package is needed. The upstream [mock survey example][example] demonstrates the SDK target and runtime shape inside Hovel; it is not evidence of an independent-workspace build.

## Distribution gap and candidate dependency paths

No `go.mod` exists at the repository root, `sdk/go`, or `sdk/go/hovel`. `core/go.mod` declares `github.com/vibepwners/hovel`, but the SDK is physically outside that module directory. The special `go.work.bazel` references `core` and `third_party/go_deps`; it does not package SDK source for ordinary external `go get`. Do not fabricate a published Go SDK version or assume adding a replace directive alone resolves that layout. [Root tree][tree], [core Go module][gomod], [Bazel Go workspace][gowork].

1. **Full-source Bzlmod candidate.** Pin the Hovel source commit/archive, preserve its `hovel_slices` module identity, and consume `@hovel_slices//sdk/go/hovel:hovel` (or the explicitly configured repository alias). Record archive integrity and exact override configuration. Hovel's module declares many unrelated Go, C/C++, Rust, Python, Node and payload-toolchain dependencies, a local `core` repository and custom extensions. Declaration alone does not prove all tools execute, but resolution/repository mapping/toolchain effects must be measured in Burrow. No registry availability or successful external override has been established here. [Root module][module].
2. **Minimal archive overlay fallback.** Use the exact pinned upstream SDK source files and license, with a Burrow-owned BUILD defining one public `go_library` under the original import path. Reproduce `HOVEL_SRCS` and platform-specific `x/sys/unix` dependency from the upstream BUILD, not the test files or branch-instrumented variant. A full archive with an overlay must account for existing nested BUILD packages: place/replace the SDK-package BUILD deliberately, or extract the SDK subtree with license supplied separately; a root BUILD glob cannot silently cross upstream package boundaries. Keep upstream Go files unchanged and record overlay provenance. Resolve rules_go and x/sys through Burrow's declared dependency graph. This is an integration candidate, not a maintainer-published SDK distribution. [SDK BUILD][build].

Copying the existing SDK BUILD by itself is insufficient: it loads `//tools/coverage:go_branch.bzl` and declares a branch-instrumented library/test surface. The plain SDK does not need branch instrumentation, but its BUILD package still loads that rule. The overlay should not pull in coverage machinery just to compile the public library. [Coverage rule][coverage], [SDK BUILD][build].

Prefer a maintainer-supported external distribution if one becomes available. Neither a Go module release nor a working standalone SDK archive was located in the inspected distribution sources. Recheck this when executing the proof rather than treating the current layout as permanent.

## Supported package and install path

Produce a real built binary and a package root containing `hovel-module.yaml`. The manifest requires `apiVersion: hovel.dev/v1alpha1`, `kind: ModulePackage`, identifying metadata, `runtime.protocol: jsonrpc-stdio`, and a launch entry whose host selector points at that binary. The initial Linux proof needs only the agreed operator architecture; do not confuse operator host with remote SSH target. Metadata returned by handshake/schema remains authoritative. [Packaging contract][development].

Development uses `hovel module install --link /absolute/package/root`; distribution uses a `.tgz` with the manifest at its root and real launch files. Preserve executable permissions. A Go-only package needs no Python runtime or installation script. Use declared archive targets (upstream rules_pkg `1.2.0` when needed) and enter build/test/package workflows through Aspect. [Packaging contract][development], [root module][module].

Run `hovel module check --warnings-as-errors /absolute/package/root` against the runnable package to inspect launch and RPC metadata. Archive checks alone inspect package/selector shape and do not execute the archived binary, so an archive check cannot establish runtime compatibility. Successful install alone also cannot prove launch support: Hovel permits install without a matching host launcher. [Module checking and installation][development].

## Smallest proof still required

A separate bounded implementation/prototype should leave:

1. A clean independent Burrow checkout with pinned source integrity, dependency configuration, one minimal inert survey module and Aspect-declared build/test/package targets. The check must work without `.references/hovel`, ambient source symlinks or importing Hovel internals.
2. One framed behavior check for handshake/schema and an inert successful Run, including a structured log notification and valid result. `Info`/`Schema` must have no SSH/network side effects; stdout must contain only protocol frames.
3. One Linux package linked into an isolated Hovel workspace, checked, discovered and executed through Hovel. Repeat using the built `.tgz` to verify packaged paths/permissions rather than relying only on a development link.
4. Recorded exact commands, dependency pin/checksum, selected architecture and outcomes. If full-source dependency fails or proves disproportionate, record the failure and repeat only the dependency piece with the minimal overlay.

A localhost SSH session, detach/close, cancellation, resize and leak checks belong to the dependent lifecycle/first-SSH proof. This investigation does not implement transport or claim those gates passed. The existing mock survey's simulated reachability is not a real connection test. [Example source][example], [existing integration research](hovel-integration.md).

## Validation and limits

Source and documentation inspected at the exact pin; GitHub authenticated HEAD verification matched it. No SDK compilation, package install, module execution, or upstream test suite was run. The existing `aspect build //:research` gate passed for this artifact through the cached Aspect CLI binary (`/tmp/burrow-research.cW39N9/aspect-cli-x86_64-unknown-linux-musl`, with its directory added to PATH for the Bazel shim). It checks only research metadata/build inputs, not these architectural conclusions or runtime behavior.

[development]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/docs/site/src/content/spec/module-development.html
[sdk]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/README.md
[build]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/sdk/go/hovel/BUILD.bazel
[module]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/MODULE.bazel
[gomod]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/core/go.mod
[gowork]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/go.work.bazel
[tree]: https://github.com/vibepwners/hovel/tree/c461ba282a8aecc7aa3a079a4613bf5e2640c388
[coverage]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/tools/coverage/go_branch.bzl
[example]: https://github.com/vibepwners/hovel/tree/c461ba282a8aecc7aa3a079a4613bf5e2640c388/modules/examples/go/mock_survey
[bazelversion]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/.bazelversion
[aspectversion]: https://github.com/vibepwners/hovel/blob/c461ba282a8aecc7aa3a079a4613bf5e2640c388/.aspect/version.axl
