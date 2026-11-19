# Doc map: kind of change to the docs that must follow

Read this after finding what changed. Each row names where the change shows up in the code and every place in `docs/` that must follow it. Section names are the current headings; if a heading has moved, find it by grep and keep the row's intent.

| Kind of change | Where it shows up | Docs to update |
|---|---|---|
| Migration adds, drops or alters a table or column | `internal/store/migrations/*.sql` | `database.md`: the ER diagram part that holds the table (Part A timeline, Part B advisories and issues, Part C triage, findings, settings and operations) and the table's entry under "Tables and columns"; "Reconstructible or not" for a new table. `data-access.md` "Table to interface map" for a new or removed table. |
| Store interface added, renamed, split, or its methods change; transaction or single-writer rule changes | `internal/store` (interface declarations, `InTx`) | `data-access.md`: the store class diagram under "Store interfaces", "Table to interface map", "Rules". `architecture.md` "What each job does" if a job's tables change. |
| Outbound interface added or its signature changes | the Go file declaring the interface in its consuming package | `interfaces.md`: the class diagram, the signature under "Signatures" (verbatim), "Implementations". `architecture.md` "What each job does", column for what a job calls out through. |
| New adapter implementing an existing interface | new file or package under the adapter's package (`internal/notify`, `internal/issues`, `internal/llm`, `internal/upstream`, `internal/vuln`, `internal/gitsrc`, `internal/extract`, `internal/usage`) | `interfaces.md` diagram and "Implementations". `external-sources.md` "Services" if it talks to a new outside service. |
| Domain object added or its fields or decision methods change | `internal/model` | `domain-model.md` class diagram and "Where the rules live". |
| Package added, removed or renamed | new or removed directory under `internal/` or `cmd/` | `architecture.md` "Layers" diagram and any table naming the package; `development.md` if the layering lint rules change. |
| Job added, removed, renamed, or its run order changes | `internal/pipeline`, `internal/sched`, job service packages | `architecture.md` "What each job does", "Schedules", the Layers diagram's jobs row, and the hourly-tick sequence diagram if the order it shows changes. |
| HTTP route added, removed or changed | route registration in `internal/web` | `architecture.md` "HTTP routes"; the ack sequence diagram if the POST flow changes. `data-access.md` "Settings and the LLM key" for `/settings`. |
| CLI command or flag added, removed or changed | `cmd/upw` | `architecture.md` "CLI". `development.md` if it changes how the build or checks are run. |
| Schedule default changes | `internal/config` defaults, `deploy/upw.example.yaml` `schedules:` | `architecture.md` "Schedules". |
| Config key added, renamed or removed | `internal/config`, `deploy/upw.example.yaml` | Grep `docs/` for the old and new key name and update each mention. Auth or endpoint keys of an outside service: `external-sources.md`. Keys that carry an owner decision (notifier types, LLM key handling): `decisions.md` only if the decision itself changed, otherwise report a contradiction. |
| Outside service added, removed, or its auth, cadence or failure behaviour changes | adapters, `httpx`, config | `external-sources.md` "Services" (and "Two GitHub tokens" for token changes); `architecture.md` "System context" diagram. |
| Library dependency added or removed | `go.mod` | `external-sources.md` "Libraries". A library not named in PLAN.md needs the owner's approval: if `docs/decisions.md` does not record it, report a contradiction instead of documenting it as settled. |
| Test referee or fixture approach changes | `Makefile` e2e target, `eval/fixtures/` | `external-sources.md` "Test referees". |
| PLAN.md edited, or the lead reports a new owner decision | `mvp-plan/PLAN.md` diff, the lead's context line | `decisions.md` (the matching section: owner decisions, accepted changes, or the section 12 table), then every doc the decision affects by the rows above. |
| Build process changes: agents, skills, model routing, `make check`, git policy, review weight | `.claude/agents/`, `.claude/skills/`, `Makefile` | `development.md` matching section. |
| Application extended (a post-MVP adapter, a new milestone feature) | several of the above | apply every row that matches; add the new piece to each diagram that shows its siblings. |
| Doc added, removed or renamed under `docs/` | `docs/` | `README.md` index, one line per doc. |

Every sync also updates the `Last synced:` line in `docs/README.md`.
