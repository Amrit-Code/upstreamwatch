---
name: review-gate
description: Decides whether a result returned by go-builder, go-mech or test-writer is accepted and committed to main. For the lead session only. The lead reruns make check and the acceptance commands itself, reads the diff against the work order's owned files, sends a fresh reviewer agent on design-heavy tasks, runs fix rounds capped at three, confirms a bug-fix test fails without the fix, triggers docs-sync, and commits. Includes a short root-cause debugging procedure for failures found at the gate. Triggers on an agent reporting DONE, DONE_WITH_CONCERNS, BLOCKED or NEEDS_CONTEXT, on "review this result", "is this ready to commit", "accept the work order", a failing check after an agent's run, or a bug to fix. Not for writing or dispatching work orders (use work-order) or closing a milestone and its security review (use milestone-accept).
---

# Review gate

An agent's statement that tests pass is not evidence; the lead's own run is. Nothing reaches `main` until the lead has rerun the checks, read the diff, and matched it against the work order. The work order and its report file are under `.claude/plans/m<n>/`; the report contract is defined in the work-order skill.

## 1. Read the status

- **DONE**: go to step 2.
- **DONE_WITH_CONCERNS**: read the concerns first. A correctness doubt or a claim that a requirement is wrong is settled before the gate continues (rule on it in the work order's Rulings, or treat it as a finding). An observation ("this file is getting long") is noted and the gate continues.
- **NEEDS_CONTEXT**: answer in the work order's Rulings section, dated, then resume the same agent with SendMessage pointing at the ruling. If the answer is a decision the owner has not made and PLAN.md or `docs/decisions.md` does not settle, ask the owner.
- **BLOCKED**: change something before retrying: supply the missing context, fix the interface file yourself and commit it, add the module to `go.mod`, split the work order, or re-dispatch to `go-builder` on `opus` if a `go-mech` run needed judgement. Retrying the same agent with the same input produces the same block.

## 2. Rerun the checks yourself

Run in the tree where the change is (the main working tree, or the worktree path the agent's result names, with `git -C <path>` and `make -C <path>`):

```
make check
```

Read the output, not only the exit code. Then run every acceptance command in the work order and compare with its expected result. Any mismatch is a finding, whatever the report says. When the report's command output and yours disagree, believe yours and record the discrepancy in the report file's fix round.

## 3. Read the diff against the work order

```
git status --porcelain
git diff --stat
git diff -- <the work order's lead-owned files> go.mod go.sum
```

- Every changed or new path is inside the work order's owned files, apart from the report file. A path outside them is reverted by the lead (`git checkout -- <path>`, or deleting the new file) and becomes a finding, because a silent edit to someone else's package is how parallel work collides.
- The lead-owned diff is empty.
- Read the full diff, including new untracked files. For each requirement, find the code that implements it and the test or acceptance command that proves it. A requirement with no proof is a finding; so is code the work order did not ask for.
- Read the tests as tests: does each one fail if the behaviour it names breaks? A test that asserts nothing, or asserts what the fake returned, proves nothing.

## 4. Fresh reviewer, on design-heavy tasks only

Design-heavy means the work order went to `go-builder`, or it changed a transaction boundary, a state machine, a cache key, a concurrency pattern or anything security-relevant. For those, write the diff to a file and dispatch a fresh `reviewer`:

```
git add -N <new files inside owned paths>
git diff > .claude/plans/m<n>/WO-<nn>-<slug>.diff
```

```
Agent(subagent_type: "reviewer", model: "sonnet", prompt:
  "Review the change for WO-<nn>. Work order: <path>. Agent report: <path>. Diff: <path>.
   Report what affects correctness against the work order, not style.")
```

Give it these three paths and nothing else: no summary of your own view, no "this part is fine", because a reviewer handed a conclusion tends to return it. Delete the diff file after the review; it is not committed. Treat the reviewer's findings like your own: a `major` or `critical` finding is fixed before commit; a `minor` one the lead fixes now, defers with a line in the report file, or dismisses with a reason.

## 5. Fix rounds, capped at three

Send all open findings verbatim to the original agent with SendMessage (its context still holds the task). The agent fixes, reruns `make check`, appends `## Fix round <r>` to the report file and replies in the short form. Repeat steps 2 and 3; if a reviewer was used, re-dispatch it with the findings list and the new diff, asking only whether each finding is addressed and whether the fix broke anything.

After round three, stop sending rounds and decide: accept with the open finding recorded in the report file, re-dispatch the work order fresh to a stronger agent and model, split it, change the interface, or take it to the owner. Rounds past three rarely converge; the problem is usually in the work order or the design, not the agent's effort.

## 6. Bug fixes: prove the test catches the bug

For a fix, confirm the new test fails without the fix and passes with it:

```
git stash push -- <changed non-test files>
go test -race -run '<NewTest>' ./<pkg>/      # expect FAIL on the assertion
git stash pop
go test -race -run '<NewTest>' ./<pkg>/      # expect ok
```

A compile failure in the first run only counts if the fix introduced the symbol on purpose; otherwise the test is not testing the bug.

When `test-writer` reports DONE_WITH_CONCERNS because a kept test exposes a production bug, do not commit the red test alone. Dispatch a fix work order to the owner of the production file with that test as its acceptance, then gate and commit tests and fix together; the test is the proof required above.

## 7. Docs and commit

1. Commit to `main` with a message naming the work order (`timeline: diff manifests into dep events (WO-03)`), including the work order and its report file, and ending with the attribution lines the session gives for commits. In a worktree, commit there and merge into `main`, then rerun `make check` on `main`.
2. If the task changed packages, interfaces, schema or migrations, routes, CLI commands, schedules, config keys, external sources or dependencies, run the docs-sync skill once the code commit exists (it records that commit), wait for its reply, read `git diff -- docs/`, and commit the docs as `docs: sync after WO-<nn>`. Several tasks in one wave may share one sync, done before the next wave is dispatched.
3. Act on contradictions docs-sync reports: they are decisions for the lead or the owner, not for the docs.

`security-reviewer` runs once per milestone, as part of milestone-accept, not per task.

## Root-cause debugging

Use this when a check fails at the gate, a fix round does not converge, or a bug is reported. Debugging can be done by the lead read-only, then written into a fix work order with the reproduction; it is not a licence to patch code in the lead session.

1. **Reproduce** with the smallest command that shows the failure (one test with `-run`, one CLI call on a fixture). Read the whole error and stack trace.
2. **Find the cause before changing anything.** Check what changed since the last green commit (`git diff`, `git log -p -3`). Trace the bad value back to where it was first produced; fix it there, not where it surfaced.
3. **One hypothesis, one change.** State the cause in a sentence, make the smallest change that tests it, rerun. If it was wrong, undo it before trying the next idea, so changes do not stack.
4. **After three failed fixes, stop patching.** Three misses usually mean the design is wrong: the interface, the transaction boundary, or the way the work order split the work. Rethink that, and bring it to the owner if it touches a recorded decision.
5. The fix ships with a test that fails without it (step 6).

## Not covered here

Writing and dispatching work orders, routing and the report contract: work-order. Milestone checklists and the per-milestone security review: milestone-accept. What docs-sync updates: docs-sync. What counts as a security finding: security-check.
