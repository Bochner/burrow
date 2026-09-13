# Contributing

Read [AGENTS.md](AGENTS.md), [CONTEXT.md](CONTEXT.md), and the
[Wayfinder map](https://github.com/Bochner/burrow/issues/1) before implementation.
Research is evidence; resolve the relevant decision ticket before treating a
recommendation as the specification.

Use `aspect help` for available workflows and `aspect burrow-check` for the
current full gate. Add cacheable work as declared Bazel targets and expose
repository workflows through `.aspect/*.axl`. Pin dependencies and record
upstream provenance. Keep credentials and machine-specific cache settings out
of the repository.

Follow the [development guide](docs/site/src/content/spec/development-guide.html)
for repository layout and [docs authoring conventions](docs/agents/docs.md)
when changing the book. Application code lives under `core/`; add other
Hovel-style areas only when they have an actual responsibility.

For nontrivial behavior, leave a focused runnable check. Report the relevant
Aspect gate and its limits. Do not equate the metadata target or documentation
checks with working SSH behavior.
