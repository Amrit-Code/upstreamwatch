---
name: docs-sync
description: Brings docs/ back in line with the upstreamwatch code and decisions, running as a forked docs-keeper subagent on Sonnet. It finds what changed since the commit recorded in the Last synced line of docs/README.md, maps each kind of change to the doc and Mermaid diagram that must follow, edits only what the change requires, records the new sync commit, and reports contradictions between code and recorded decisions without choosing a side. Use after any change to package structure, interfaces, schema or migrations, routes, CLI commands, schedules, config keys, external sources or dependencies, after a PLAN.md or decision change, when the application is extended, and whenever someone asks to sync, update or check the docs. Not for writing code or skills, and not for deciding what the design should be (decisions belong to the lead and the owner; see review-gate and work-order).
context: fork
agent: docs-keeper
---

# Sync docs/ with the code

You are running as `docs-keeper`. Bring `docs/` in line with what changed, then reply with the report at the end of this file. Write only under `docs/`.

Context from the lead (may be empty): $ARGUMENTS

## Sources of truth

- **What exists**: the code and the migrations (`internal/store/migrations/*.sql`, Go declarations, routes registered in `internal/web`, commands in `cmd/upw`, schedules and config keys in `internal/config` and `deploy/upw.example.yaml`, `go.mod`).
- **What is intended**: `mvp-plan/PLAN.md` and `docs/decisions.md`.

When the code does something different from a recorded decision, do not pick a side and do not edit either to match the other: report it as a contradiction. A documented item that the code does not have yet is not a contradiction if PLAN.md or the decisions schedule it for a later milestone; leave it, keeping any "planned" marking the doc already uses accurate. Never invent a detail to fill a gap (a column type, a default, a failure behaviour); if the source does not say, leave the gap and report it.

## 1. Find what changed

Read the `Last synced:` line in `docs/README.md`. It has the form `Last synced: <commit> (<YYYY-MM-DD>)`.

If the repository has git history and the recorded commit exists:

```
git log --oneline <commit>..HEAD
git diff --stat <commit>..HEAD
git diff <commit>..HEAD -- <paths the stat shows>
git status --porcelain
```

`git status` shows uncommitted changes; the lead normally syncs after committing, so report any you see rather than documenting them.

If there is no git history yet, or the line has no commit, compare each doc in `docs/` against the code and migrations directly, file by file.

If the lead's context above names a change (a work order, a decision), read that too; it may not be visible in code (a PLAN.md edit, an owner decision).

## 2. Map changes to docs

Read [references/DOC-MAP.md](references/DOC-MAP.md). For each change found, it names the doc, section and diagram that must follow. A change with no row still gets checked: grep `docs/` for the names it touches (`grep -rn '<name>' docs/`) and update whatever mentions them.

## 3. Edit

- Change only what the code change requires. Keep each doc's existing structure, headings, ordering and wording style; do not rewrite sections that are still true.
- Take names, signatures, columns and routes from the code exactly. Copy Go signatures verbatim; list columns as the migration declares them.
- Keep Mermaid diagrams valid. Every diagram stays in a ```` ```mermaid ```` fence. Before finishing, reread each diagram you touched against these rules: quote any flowchart label containing spaces, parentheses, brackets, slashes or `<br/>`; in `erDiagram` each attribute line is `type name` optionally followed by `PK`, `FK` or `UK` and one double-quoted comment, with no commas; relationship lines use a valid cardinality pair (`||--o{`, `||--||`, `||--o|`, `}o--o{`) and a quoted label; in `classDiagram` interface markers are `<<interface>>` and dots in names are avoided (use `store_DB`, as the existing diagrams do); every node or entity referenced in a relationship is declared or implied once with one spelling. If `mmdc` (mermaid-cli) is installed, render each touched diagram with it and fix any error.
- When a table, interface, route or job is added or removed, update every doc that lists it, not only the first one found (DOC-MAP lists them together for this reason).
- Do not edit code, `mvp-plan/`, `.claude/` or anything outside `docs/`.

## 4. Record the sync

Set the `Last synced:` line in `docs/README.md` to the commit you synced to (`git rev-parse --short HEAD`) and today's date, in the form above. Without git history, write `Last synced: no commit (<YYYY-MM-DD>), compared against the working tree`. If you changed nothing, still update the line, because the check itself is what it records.

## 5. Reply

Reply with exactly this:

```
Status: DONE | DONE_WITH_CONCERNS | BLOCKED | NEEDS_CONTEXT
Synced: <old commit>..<new commit>, or "no git history: compared against the working tree"
Files changed:
- docs/<file>: <what changed, and the code or migration path it came from>
Commands run:
- `<command>` -> exit <n> (output only where it matters to a finding)
Contradictions:
- <code path:line> says <x>; <PLAN.md section or docs/decisions.md entry> says <y>
Gaps: <things that should be documented but no source states, or "none">
Outside scope: <uncommitted changes seen, errors noticed in code or skills, or "none">
```

Use DONE_WITH_CONCERNS when there are contradictions or gaps, BLOCKED when you could not finish (for example a diagram you cannot make valid), and NEEDS_CONTEXT when a change's meaning cannot be determined from code, PLAN.md or the decisions.
