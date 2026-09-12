# Herdr terminal state and dependency boundary

Investigated 2026-09-11. Evidence for the shell restoration discussion, not an
accepted architecture or a Burrow implementation proof. The user's preference
is to keep this internal and avoid additional managed dependencies; willingness
to test tmux does not accept it.

## Identification and pin

The public project matching the reference is
[herdrdev/herdr](https://github.com/herdrdev/herdr), formerly referenced by its
README as `ogulcancelik/herdr`. Inspected commit
[`9ad65d9031e8cb16a7b553c0e6f74809e9811e92`](https://github.com/herdrdev/herdr/commit/9ad65d9031e8cb16a7b553c0e6f74809e9811e92),
whose [manifest](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/Cargo.toml)
declares version 0.9.0. This is inspected source, not a claim that the commit is
a stable release.

## How restoration works

Herdr embeds terminal emulation. Its `GhosttyPaneTerminal` owns a mutex-protected
Ghostty terminal and render state. PTY bytes enter the terminal parser; rendering
reads the resulting cells into a Ratatui frame. The same object exposes cursor,
alternate-screen, scrollback, keyboard and mouse modes, and resize operations.
Returning to a pane can render its current screen because terminal state remains
live, rather than treating the output as lines of text.
[Source: pane terminal and rendering](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/src/pane/terminal.rs#L188).

The Unix PTY actor services reads, queued writes, resize and terminal-generated
responses. The terminal runtime belongs to Herdr's server. These responsibilities
are implemented inside Herdr; tmux is not the engine of this path.
[PTY actor](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/src/pty/actor/unix.rs),
[runtime](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/src/terminal/runtime.rs#L11).

Herdr distinguishes live persistence from reconstruction: client detach keeps
server-owned processes and screen state; full server restart loses those
processes. Optional screen-history replay restores appearance, not the original
shell. Live update handoff is a separate, experimental operation.
[Session-state documentation](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/docs/next/website/src/content/docs/session-state.mdx).

## What “internal” costs

Herdr vendors Ghostty at upstream commit
`44f2a44df7e8c4a0c6df3f7d872ef3d7ead88e51`, records provenance, and statically
links it through Rust bindings. Building requires Zig 0.16.0 in addition to
Rust. Its Cargo dependencies include Ratatui 0.30, Crossterm 0.29 and a locally
patched, vendored `portable-pty` 0.9.0. An embedded engine removes a separately
managed multiplexer runtime; it does not remove upstream dependencies.
[Ghostty provenance](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/vendor/libghostty-vt.vendor.json),
[build and static linking](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/build.rs),
[dependencies](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/Cargo.toml).

Vendoring makes upstream changes deliberate upgrades. Herdr still maintains
local Ghostty patches for a keyboard-mode query and hosted builds, each with
removal criteria and verification. One patch records that an upstream enum
assignment displaced Herdr's previous local value: concrete evidence that
pinning controls exposure but does not eliminate maintenance.
[Patch ledger](https://github.com/herdrdev/herdr/blob/9ad65d9031e8cb16a7b553c0e6f74809e9811e92/vendor/libghostty-vt.patches.md).

## Implication for Burrow

Recommendation, not acceptance: test the same principle with a pinned Go
terminal-emulation library inside the existing frontend before adding tmux.
Herdr's Rust/Ratatui/Zig stack is evidence for the principle, not a dependency
set to transplant into Bubble Tea v1. Burrow initially needs shells to survive
switching views while its frontend runs; it does not need Herdr's separate
server persistence or live update handoff for that requirement.

The bounded proof should keep two shell PTYs alive, continue consuming output
while hidden, switch between management and an alternate-screen application,
resize, and verify screen/cursor restoration and terminal replies. It should
also establish the actual pinned Go dependency delta and normal-quit cleanup.
Passing those checks would support the internal option; source inspection alone
does not establish compatibility or user acceptance. No Herdr build or runtime
test was executed for this note.
