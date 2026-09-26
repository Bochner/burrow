# pipx distribution feasibility

Investigated 2026-09-26. Research recommendation, not an approved implementation
specification or authorization to publish. Separate from issue #103.

Follow-up in the same session: the owner authorized preparing the complete
local release path, leaving PyPI registration for another day. The implemented
path is documented in the [release guide](../site/src/content/spec/releasing.html).
The GitHub `pypi` environment was created with protected-branch deployments;
the workflow restricts publishing to main. No credentials were copied and no
artifacts were published. Focused Aspect checks passed for offline pinned-pipx
installation, upgrade/uninstall preservation, real CLI/Hovel/embedded skills,
deterministic wheels and RECORDs, static Linux amd64 executable, release-tag
guards, workflow syntax, Python formatting and the generated site. Standards
and Spec reviews both reported zero findings. Final full preflight follows.

## Recommendation

Yes: investigate packaging now, then implement it as a bounded distribution
follow-up. Keep the existing Go application and package a prebuilt Linux amd64
binary in a Python wheel with a small `burrow` console entry point. pipx accepts
wheels and exposes declared console applications; Python packaging explicitly
allows platform-specific distributions containing native executables.
Sources: [pipx](https://pipx.pypa.io/stable/),
[compatibility tags](https://packaging.python.org/en/latest/specifications/platform-compatibility-tags/).

This needs no Python rewrite, second Hovel module, new daemon owner, or custom
updater. Start with a locally installed wheel, then attach it to the existing
private release channel. Public PyPI can follow once the owner chooses that
release audience and a distribution name.

## Fit with Burrow today

- [The package target](../../core/cmd/burrow/BUILD.bazel) already assembles the
  Go executable, agent skill bundle, module manifest and Hovel SDK license.
  Reuse those declared inputs instead of compiling Go during pipx installation.
- [The launcher](../../core/launch/package.go) supports Linux amd64 only. It
  already downloads Hovel v0.4.2, verifies both wheel and executable SHA256,
  caches it, and supports an exact local wheel through `--hovel-package`.
  Keep this lifecycle; declaring an independently managed Python Hovel
  dependency would not satisfy the launcher's pinned executable contract.
- [Module caching](../../core/launch/operations_linux.go) reads `os.Executable()`
  and publishes the binary with the embedded manifest into a digest-addressed
  cache. A Python entry point that replaces itself with the packaged Go
  executable fits that existing path; verify this in the installed-wheel check.
- [The README](../../README.md#install) identifies the current v0.2.0 archive as
  a private-repository release. pipx installation does not imply public PyPI
  publication: it also accepts local wheels, and supports a private index.
  Source: [pipx private indexes](https://pipx.pypa.io/stable/how-to/use-private-index.html).

## Smallest packaging slice

1. Add a declared wheel target consuming the existing production executable
   and required assets, exposed through Aspect. Pin any packaging tools and
   preserve upstream licenses. Use the normal Python console entry-point
   mechanism for `burrow`; its short stdlib launcher should execute the Go
   binary directly so terminal streams, arguments, signals and exit status
   retain their existing behavior.
   Sources: [pipx package compatibility](https://pipx.pypa.io/stable/explanation/making-packages-compatible.html),
   [entry points](https://packaging.python.org/en/latest/specifications/entry-points/).
2. Produce one Linux x86-64 wheel. A native executable makes `*-none-any.whl`
   incorrect. Select a manylinux tag only after verifying the actual binary
   and runtime baseline; Hovel's current artifact uses
   `manylinux_2_28_x86_64`, but copying that tag alone proves nothing about
   Burrow. Packaging does not add macOS, Windows, ARM or musl support.
   Sources: [wheel format](https://packaging.python.org/en/latest/specifications/binary-distribution-format/),
   [platform tags](https://packaging.python.org/en/latest/specifications/platform-compatibility-tags/),
   [current Hovel pin](../../core/launch/package.go).
3. Add an Aspect acceptance check that installs the wheel into an isolated
   pipx environment, launches the installed command outside the checkout,
   verifies CLI discovery, opens a workspace with the pinned Hovel package,
   and exercises installed agent assets. Check upgrade/uninstall without
   deleting workspace state or the deliberately separate Hovel cache. Run the
   existing required preflight before a release. No packaging was built or
   tested during this research.

## Naming and release choices

The public PyPI name `burrow` is already occupied by an unrelated Python client
for the Nest Mobile API (version 0.0.4 at the live check). Do not advertise
`pipx install burrow` for this project. Choose a distinct distribution name;
the installed command can still be `burrow`. `burrow-ssh` and `burrow-cli` both returned HTTP 404 from the live PyPI JSON
API on 2026-09-26. They have no visible project at this check; this does not
guarantee registration availability or reserve either name. Sources:
[burrow-ssh metadata endpoint](https://pypi.org/pypi/burrow-ssh/json),
[burrow-cli metadata endpoint](https://pypi.org/pypi/burrow-cli/json). Sources: [existing PyPI project](https://pypi.org/project/burrow/),
[live PyPI metadata](https://pypi.org/pypi/burrow/json),
[entry-point naming](https://packaging.python.org/en/latest/specifications/entry-points/).

A public PyPI upload would make the uploaded wheel available outside the
private GitHub repository. Keeping the source private does not keep the
published binary private. A private downloadable wheel is enough to establish
pipx support without that audience change. Public release naming, audience,
version mapping and minimum supported Linux/Python environment are unresolved
choices, not blockers to a local packaging proof.

If public PyPI is selected, use PyPI Trusted Publishing with a dedicated
GitHub release job and an explicitly configured publisher, reusing the
existing required release gate. PyPI documents the official publishing action
and job-scoped `id-token: write` permission; pin the action to a reviewed
commit per repository conventions. Source:
[PyPI Trusted Publishing](https://docs.pypi.org/trusted-publishers/using-a-publisher/).

## Actual Hovel approach and credentials

Inspected clean local Hovel checkout at
`541e78ada0af48165aef932732145c418aa2d290`, rather than inferring its approach
from the wheel consumed by Burrow.

- [core/tools/release/build_hovel_wheel.py](https://github.com/vibepwners/hovel/blob/541e78ada0af48165aef932732145c418aa2d290/core/tools/release/build_hovel_wheel.py)
  is a Python stdlib wheel writer: `zipfile`, fixed timestamps, executable
  modes, SHA256 `RECORD`, metadata, and console entry point. It puts the Go
  binary at `hovel/bin/hovel`, writes `hovel/__main__.py`, and calls `os.execv`
  on POSIX. It declares Python >=3.10, `Root-Is-Purelib: false`, and a
  `py3-none-<platform>` tag. Hovel does not need Hatch or setuptools for this
  application wheel.
- [core/tools/release/BUILD.bazel](https://github.com/vibepwners/hovel/blob/541e78ada0af48165aef932732145c418aa2d290/core/tools/release/BUILD.bazel)
  declares `build_hovel_wheel_linux_amd64` consuming the prebuilt
  `//cmd/hovel:hovel_linux_amd64` and selecting `manylinux_2_28_x86_64`.
  [.aspect/release.axl](https://github.com/vibepwners/hovel/blob/541e78ada0af48165aef932732145c418aa2d290/.aspect/release.axl)
  exposes `aspect hovel-release hovel`, runs declared wheel builders in the
  `core` workspace with optimized compilation, and writes `core/dist`.
  Hovel has five platform builders; Burrow needs only its supported Linux
  amd64 slice.
- [.github/workflows/release.yml](https://github.com/vibepwners/hovel/blob/541e78ada0af48165aef932732145c418aa2d290/.github/workflows/release.yml)
  runs verification, builds wheels, runs an installed-wheel smoke check,
  uploads artifacts, then publishes in a separate `pypi` environment job.
  That job grants `id-token: write` and invokes pinned
  `pypa/gh-action-pypi-publish` v1.14.0 with attestations, then checks PyPI
  artifact hashes. There is no PyPI password/token argument: this is Trusted
  Publishing. The BuildBuddy secret is build-cache configuration, not a
  PyPI credential.

**Following Hovel means no reusable PyPI API key is required.** The owner
configures a PyPI pending publisher for the selected package name, GitHub
owner `Bochner`, repository `burrow`, the eventual release workflow filename,
and matching `pypi` environment. First authorized publication creates the
project. Pending publisher setup does not reserve the name. Source:
[PyPI project creation through Trusted Publishing](https://docs.pypi.org/trusted-publishers/creating-a-project-through-oidc/).

`Bochner/lazyssh` uses the older token approach:
[python-publish.yml](https://github.com/Bochner/lazyssh/blob/main/.github/workflows/python-publish.yml)
passes `secrets.PYPI_API_TOKEN` or `secrets.TEST_PYPI_API_TOKEN` as
`HATCH_INDEX_AUTH`. A read-only secret-name listing confirmed both names
exist; no values were retrieved or printed. GitHub's secret GET/list API
returns names and timestamps without the encrypted values, so its stored
secret cannot be read back and copied through that API. Reusing that token
is unnecessary for Hovel's approach. Source:
[GitHub Actions secret API](https://docs.github.com/en/rest/actions/secrets#get-a-repository-secret).

For Burrow, adapt the upstream stdlib wheel writer with pinned provenance and
license attribution, one platform and Burrow's existing package assets;
reuse the verified build -> installed-wheel smoke -> separate OIDC publish
pattern. Do not copy Hovel's unrelated SDK, module-family, multi-platform or
build-cache machinery. A packaging implementation and PyPI publisher setup
are still required: an API key alone would not deliver pipx support.

## Method

Read the repository context, Wayfinder issue summary, production package target,
launcher and current installation documentation. Context7 returned no relevant
pipx library after two name queries; fetched official pipx documentation
directly and used Context7's `/websites/packaging_python_en` for wheel/entry-point
facts. Verified the occupied PyPI name through its live JSON API. These sources
establish feasibility; the proposed installed-wheel acceptance remains to be run.
