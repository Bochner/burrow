---
name: closeout
description: "Verify the current issue, close it when complete, and identify the next issue to implement."
disable-model-invocation: true
---

# Closeout

Verify issue-backed work after implementation. Invoking this skill authorizes
the completion comment and tracker updates for work proven complete.

1. Read `AGENTS.md` and `docs/agents/issue-tracker.md`. Identify the current issue
   from the user's reference or the conversation, branch, and commits. Ask only
   if the issue is ambiguous. Fetch its full description, comments, parent, and
   blocking relationships through the repository's tracker workflow.
2. Check every acceptance criterion against the committed implementation and
   its verification and review evidence. Reuse current evidence; run only checks
   or reviews needed to fill gaps or cover relevant changes since verification.
   If work is uncommitted, requirements are unmet, or required owner acceptance
   is outstanding, leave the issue open and report exactly what remains.
3. When complete, mark the proven acceptance criteria, post a concise resolution
   with the commit, outcome, checks, review results, and known limits, then close
   the issue as completed. Correct tracker fields made stale by this completion.
   Update its parent only where an explicit checklist, status, or context pointer
   changes; native sub-issue and dependency state needs no duplicate comment.
   A parent's own exit criteria must be satisfied before it can be closed.
4. Refresh the open siblings and their blockers. Follow an explicit implementation
   order when recorded; otherwise choose the first unassigned, unblocked issue
   in native parent order. Without a parent, use the current milestone or agreed
   work sequence. Read the candidate before recommending it; distinguish an
   implementation ticket from work that still needs an owner decision. If none
   is ready, report the blocking issue or decision instead of inventing work.

Finish with the current issue's verified status, tracker changes, and the next
issue's number, title, link, and why it is ready. Recommend the next issue without
claiming or starting it. This workflow does not push, open a PR, or merge.
