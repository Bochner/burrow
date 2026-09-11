# Issue tracker: GitHub

Issues and future specifications live in the private
[Bochner/burrow](https://github.com/Bochner/burrow/issues) repository.
Use the installed `gh-axi` skill and `npx -y gh-axi <command> --help` for current
CLI syntax. Use its `api` command for native GitHub relationships that lack a
high-level command. Read-only exploration does not authorize publishing issues.

PRs as a request surface: no.

## Project view

The private [Burrow project](https://github.com/users/Bochner/projects/9) is copied
from Tirnaill: Delivery and Closed tickets tables, grouped by Milestone, with
Status, Priority, Parent issue, and Sub-issues progress. The initial decision
map and its children belong to the SSH specification milestone.

Native workflows add sub-issues when their parent is in the project and update
status when items close. Add standalone issues with `--project Burrow` or
`gh-axi project item-add 9 --owner Bochner --url <issue-url>` until the owner
enables the native auto-add rule for `Bochner/burrow`, filter `is:issue`.
The saved Show hierarchy setting and auto-add workflow configuration require
GitHub's project UI; current public API setters do not expose them.

Project views are for scanning; the map and decision tickets below remain
canonical. Avoid duplicating decision details in the project README.

## Wayfinding operations

These conventions prepare the tracker; the bootstrap does not publish a map or
pretend that the required live discussion has happened.

- Map: one issue labeled `wayfinder:map`, with Destination, Notes, Decisions so
  far, Not yet specified, and Out of scope. Refer to issues by linked titles.
- Tickets: child issues using GitHub's native sub-issue relationship, labeled
  `wayfinder:research`, `wayfinder:prototype`, `wayfinder:grilling`, or
  `wayfinder:task`. Create labels on first use.
- Attach a child using `POST /repos/Bochner/burrow/issues/<map>/sub_issues`
  with the child's numeric database `issue_id`.
- Blocking: use native dependencies via
  `POST /repos/Bochner/burrow/issues/<child>/dependencies/blocked_by`, with
  the blocker's numeric database `issue_id`. Get that ID from the issue API's
  `.id`, not its displayed number or GraphQL node ID.
- Query the map's children through its `sub_issues` endpoint, with pagination.
  The frontier is open, unassigned children without open blockers. Use the
  issue's `issue_dependencies_summary.blocked_by` or fetch its blockers and
  inspect state. Do not confuse every repository issue with a map child.
- Claim before working: assign the chosen ticket to the developer driving it.
- Resolve: record an answer comment, close the ticket, and append a short linked
  pointer to the map's Decisions so far. Detailed decisions belong in tickets.
- If a native feature is unavailable for this account/repository, document the
  limitation and use a map task list, `Part of #<map>` and `Blocked by: #<n>`
  body conventions. Do not silently omit dependencies.

Source: [Matt Pocock's GitHub tracker template](https://github.com/mattpocock/skills/blob/3cca18b368ae95cdbdebbff572ccafa662551015/skills/engineering/setup-matt-pocock-skills/issue-tracker-github.md).
