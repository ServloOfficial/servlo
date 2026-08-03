---
name: servlo-open-pr
description: Draft and open a pull request the Servlo way — correct issue linking, human prose, none of the banned sections. Use when asked to open, prepare or draft a PR for Servlo. Never creates anything on GitHub without explicit per-action approval.
---

# Open a Servlo PR

Every write to GitHub (create, edit, close, reopen, merge or comment on an issue
or PR) needs explicit approval **each time**. Draft the text, show it, and wait.

This holds even though the project is one person. Solo relaxes what has to exist
before you start; it does not relax who decides what gets published.

There is no `gh` CLI in these sessions. GitHub goes through the MCP tools.

## Before the PR

1. **An issue is recommended, not required, while the project is solo.** If one
   exists, link it. If the work deserves one, draft it and ask before creating it.
   The requirement returns the moment a second person joins.
2. Run `/servlo-preflight` — the PR is not ready until the local gate is green,
   and a gate that skipped the droplet smoke test is reported as exactly that.
3. Confirm the branch is off `main`, not `main` itself, and staged by explicit
   path (never `git add -A`). `git status` first.

## PR body — write it as a human would

Prose paragraphs, single-line, explaining what changed and why. Then the issue
link:

- Feature PR → `Closes #N` (auto-closes on merge).
- Bug-report issue → `Refs #N` (stays open until the stable release ships).
- Security issue → `Closes #N` (closes once the fix merges to main).

## Never include

- A Test plan section.
- A Verified / Tested / Manual testing section or trailer.
- A checklist of any kind (`- [ ]` / `- [x]`, "Release checklist"…).
- A "Notes for reviewers" section — we own the project, there is no external reviewer.
- `file:line` citations, em dashes, `Co-Authored-By`, or "Generated with…" footers.
- Prose about tests, TDD or coverage; and don't mention incidental cleanup.

## PR and issue comments

Casual plain prose. No markdown, no bullets, no hyphens — commas instead. Don't
open with boilerplate like "Pulled it down and put it on my install." Vary it.

## After pushing

Return immediately. Don't sit polling CI.
