# Charm operator feedback for Burrow

Researched 2026-09-11 for **Which terminal interaction model fits the agreed
boundaries?** This is evidence and recommendations for the approved
connection-first management view, full-terminal shell, responsive layout and
truthful progress; it is not an implementation specification.

## What the evidence supports

The recurring attraction is care: pleasant visual hierarchy, quick interaction,
small menus, and useful work inside an existing terminal. That does not establish
that operators want every animation or widget. These are deliberately selected
firsthand comments and issue reports, not a representative survey or popularity
ranking. Reports describe versions used at the time; they are not assertions
that the same defects remain today.

| Firsthand source | Signal | Limit |
| --- | --- | --- |
| [Crush user jax024](https://www.reddit.com/r/golang/comments/1q5ntao/comment/ny1jq98/) | Appreciates its speed. | One subjective report, no benchmark. |
| [Crush comparison by orak7ee](https://www.reddit.com/r/golang/comments/1q5ntao/comment/nyy4bt5/) | Likes the perceived care, configuration simplicity and UI; wants dark/light theme following. | Personal comparison; unrelated licensing/model claims are not adopted. |
| [Crush user starBH](https://www.reddit.com/r/golang/comments/1q5ntao/comment/ny3hnat/) | Enjoyed the appearance but moved away after apparently endless spinning. | Does not identify a reproducible cause; useful warning about ambiguous activity. |
| [Crush launch discussion](https://news.ycombinator.com/item?id=44736176) | dedpool praises the slick TUI; jsnell objects to whitespace, decoration and animation displacing expected keys, scrollback and stable rendering. | Criticism addresses a wider agent-TUI trend, not a verified defect list for Crush. |
| [Charm discussion](https://news.ycombinator.com/item?id=30048332) | Users enjoy the SSH interface and aesthetic care. chrismorgan reports weak focus/code contrast on a light terminal; chris_st reports good light-theme results. | Conflicting firsthand experiences demonstrate terminal variation, not universal failure. |
| [Gum in actual shell use](https://news.ycombinator.com/item?id=40321043) | Cu3PO42 finds its simple option picker pleasant to use from scripts. | Supports focused controls; not evidence for a larger dashboard. |
| [Gum discussion](https://news.ycombinator.com/item?id=39572528) | Enthusiasm for Charm branding coexists with a user finding no daily reason to use it; another points to Glow for reading a README. | Visual appeal alone does not establish utility or adoption. |
| [Request to disable Crush mouse capture](https://github.com/charmbracelet/crush/discussions/3430) | A keyboard/tmux user wants native selection, copy, scrolling and context menus; a maintainer supports making capture optional. | Specific workflow evidence, not a claim that all users dislike mouse interaction. |

## Official behavior worth borrowing

- **Gum:** choose/filter/input/confirm are small controls for a concrete task.
  Its spinner is tied to a running command and stops when that command exits.
  Borrow the interaction shapes, not an extra Gum subprocess dependency inside
  Burrow. [README at `7179388031ae67d7f538d001be87d931f1cf5e28`](https://github.com/charmbracelet/gum/blob/7179388031ae67d7f538d001be87d931f1cf5e28/README.md).
- **Glow:** familiar `less` keys, `?` to discover shortcuts, width control,
  terminal-background detection and explicit light/dark overrides. Its plain
  CLI and TUI serve different reading needs.
  [README at `7b2431d4a82428fb477eb4361e11583e1644e9ba`](https://github.com/charmbracelet/glow/blob/7b2431d4a82428fb477eb4361e11583e1644e9ba/README.md).
- **Soft Serve:** browse repositories over SSH, enter a particular repository
  directly, or print a file through a command. A contextual `c` copies the
  selected repository's clone command, subject to terminal OSC52 support.
  Borrow contextual actions and explicit capability limits, not its server
  architecture. [README at `37685d36f5b7bf0e32217ddd7c8e045c57772619`](https://github.com/charmbracelet/soft-serve/blob/37685d36f5b7bf0e32217ddd7c8e045c57772619/README.md#the-soft-serve-tui).
- **Crush:** the latest release verified during this reading is **v0.93.1**,
  published 2026-09-09. It adds disabling mouse support through configuration or
  the command palette. Its changelog also records fixes for false clipboard
  success, inline-code flicker and viewport-bottom detection. Those changes
  corroborate the importance of mundane terminal behavior without implying
  that Burrow needs a coding-agent interface.
  [Official v0.93.1 release](https://github.com/charmbracelet/crush/releases/tag/v0.93.1).

These commit pins record the inspected upstream documentation, not approved
Burrow dependency upgrades. Readmes establish advertised behavior; this research
did not run the applications or benchmark their rendering.

## Ranked recommendations for the interaction prototype

1. **Make the next action visible and keyboard-complete.** A short contextual
   footer shows select/open/back/help; `?` expands help. Search the connection
   list and retain selection on return. Use ordinary arrows/Enter/Escape first;
   no operator should need a mouse to reach an offscreen item. This applies
   Glow/Gum's documented patterns to the approved connection-first layout.
2. **Protect terminal ownership and copy.** Management should begin without
   mouse capture. Give an attached shell the full terminal and document the
   management-return shortcut before attachment. Restore terminal modes on
   return/exit. Copyable diagnostics remain plain text; a future explicit copy
   action must not announce success it cannot establish. Crush feedback and
   its latest fixes make this a high-value interaction check.
3. **Use color as reinforcement.** Keep the approved green circle plus
   `CONNECTED`, yellow plus `CONNECTING`, red plus `DISCONNECTED`; preserve
   labels and a visible selection marker without color. Check light, dark and
   no-color views. A failed operation needs its own explanation, not merely a
   red connection dot. This addresses the conflicting real contrast reports.
4. **Spend space on the operator's work.** Show connection identity and useful
   shell/tunnel/transfer summaries, with details on demand. Use the approved
   two-pane wide view and single focused narrow view. Keep decoration modest;
   no permanent splash, ornamental animation or dashboards without actionable
   data. This is an inference from both aesthetic praise and density criticism.
5. **Make activity honest and recoverable.** Spin only while an outcome is
   unknown; show the operation and elapsed time. Use measured bytes/total for
   transfer progress. Preserve failure text and available recovery actions;
   distinguish a cancel request from confirmed cancellation. Feedback about
   endless spinning supports clarity, not invented timeout percentages.
6. **Keep interaction stable while work changes.** Background events must not
   steal selection, focus or scroll position. A log view should visibly state
   when it is following new output and allow explicit resumption. Exercise
   resize, long labels and rapid status updates before adding more polish.
   Crush's [auto-follow report](https://github.com/charmbracelet/crush/issues/2481)
   and subsequent viewport fixes illustrate the failure mode.

The owner has already approved the overall interaction direction. These findings
refine the prototype within it; they do not silently approve new runtime
capabilities, library upgrades, a theme marketplace, or AI/chat behavior.

Owner clarification during this session: some animation is expected and welcome.
The intended default is active spinners and smooth measured-progress updates;
reduced motion is optional. Density and feedback concerns do not imply an
animation-free interface. Animation should reflect real activity, stop on a
terminal outcome, and never substitute for explaining failure or a stalled task.
