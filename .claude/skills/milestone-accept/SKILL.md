---
name: milestone-accept
description: Closes an upstreamwatch milestone (M0 to M5) against its acceptance checklist. For the lead session only. Each milestone has a checklist built from PLAN.md section 10 and the owner's later decisions, with the exact command or observation, the expected result and a status that only the lead changes; items needing the owner (tokens, a digest arriving, the VM) are marked. A milestone closes only when every item is passed or waived by the owner, the milestone security review is clean and docs are synced. Triggers on "is M2 done", "close the milestone", "acceptance", "run the checklist", "what is left in this milestone", or planning which work orders a milestone still needs. Not for accepting a single work order (use review-gate) or writing work orders (use work-order).
---

# Milestone acceptance

Each milestone has one checklist under `references/`. It is both the definition of done and the record: the lead runs the items, writes the status into the checklist file, and commits it.

| Milestone | Checklist |
|---|---|
| M0 Skeleton | [references/M0-SKELETON.md](references/M0-SKELETON.md) |
| M1 Timeline | [references/M1-TIMELINE.md](references/M1-TIMELINE.md) |
| M2 Vulnerability feed and digest | [references/M2-VULNS-DIGEST.md](references/M2-VULNS-DIGEST.md) |
| M3 Upstream issues, prefilter, eval scaffolding | [references/M3-UPSTREAM-PREFILTER.md](references/M3-UPSTREAM-PREFILTER.md) |
| M4 LLM triage and settings | [references/M4-LLM-TRIAGE.md](references/M4-LLM-TRIAGE.md) |
| M5 Eval, hardening, docs | [references/M5-EVAL-HARDENING.md](references/M5-EVAL-HARDENING.md) |

Read the current milestone's checklist when planning its work orders, so every item has a work order that produces it, and again when closing.

## Status

Each item's status is one of `open`, `passed`, `failed`, `waived`. Only the lead changes it, and only on evidence:

- `passed`: the lead ran the command or saw the observation, and the result matched. Put a short evidence note in the item's Evidence column (the key output line, a commit, a run id). For an owner item, quote what the owner reported and the date.
- `failed`: the result did not match. Note what happened; a work order follows.
- `waived`: the owner explicitly said, in their own words, that the item does not need to pass for this milestone. Record their words and the date. The lead never waives an item on the owner's behalf.

An agent's report is not evidence for a checklist item; the lead reruns the command.

Items marked `owner` in the Who column need the owner: real tokens, the homelab VM, an inbox or phone, private target repositories, or human labelling. Give the owner the exact command and expected result from the checklist and ask them to report what they saw. The VM is deployment only (builds and tests run on the Mac); deployment steps follow the upw-deploy skill and the homelab skill.

Commands run from the repository root on the Mac unless the item says VM. `<fixture>` means a public fixture repository slug from `eval/fixtures/` or `upw.dev.yaml`; `<target>` means a private target configured only in the VM's `upw.yaml`, which never appears in the repository.

## Closing

A milestone closes when all of these hold:

1. **Every checklist item is `passed` or `waived`.**
2. **The security review is clean.** Dispatch `security-reviewer` once, with `model: "sonnet"`, over the whole milestone:

   ```
   Agent(subagent_type: "security-reviewer", model: "sonnet", prompt:
     "Security review for M<n>. Diff range: <previous milestone tag, or the root commit for M0>..HEAD.
      Checklist: .claude/skills/milestone-accept/references/<file>.")
   ```

   Clean means every finding is fixed (each fix gated through review-gate) or waived by the owner in writing, and every unconfirmed suspicion is either shown false by the lead or treated as a finding. Fixes made after the review are not re-reviewed in full; the lead checks each against its finding. Record the outcome on the checklist's last line (`Security review: clean, <date>, <findings fixed>/<waived>`).
3. **Docs are synced.** The `Last synced:` line in `docs/README.md` names HEAD or a commit after which only docs changed. If not, run the docs-sync skill and commit its edits. Resolve any contradiction it reports with the owner before closing.
4. **`make check` passes on `main`**, run by the lead after the last merge.

Then commit the checklist with its statuses, tag the commit `m<n>` (the next milestone's security review starts from it), and report to the owner: items passed, items waived and by whom, security findings and their outcome, and anything carried to the next milestone.

## Changing a checklist

Items come from PLAN.md section 10 adjusted by `docs/decisions.md`. When the owner changes scope, edit the checklist in the same commit as the decision is recorded and run docs-sync. Do not delete a failing item to close a milestone; it is either fixed, waived by the owner, or moved to a later milestone's checklist with the owner's agreement noted.

## Not covered here

Accepting individual work orders and the fix loop: review-gate. Writing the work orders a checklist item needs: work-order. Deploying to the VM: upw-deploy. What the security reviewer checks: security-check.
