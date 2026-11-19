# upstreamwatch docs

These docs describe how upstreamwatch is designed and built. Start with architecture if you are new to the project. The full plan, including scope, pipeline details and milestones, is in [mvp-plan/PLAN.md](../mvp-plan/PLAN.md).

- [architecture.md](architecture.md): system context, layers, what each job does, entry points and two request traces.
- [interfaces.md](interfaces.md): the outbound interfaces, their Go signatures and the adapters that implement them.
- [data-access.md](data-access.md): `store.DB`, the store interfaces, which tables sit behind each, and transaction rules.
- [domain-model.md](domain-model.md): the objects in `internal/model` and how they relate.
- [database.md](database.md): the entity-relationship diagrams and every table and column.
- [external-sources.md](external-sources.md): every outside service, libraries and test referees.
- [decisions.md](decisions.md): the owner's decisions, the accepted changes to the plan and the section 12 defaults.
- [development.md](development.md): how the build is run: agents, skills, model routing, `make check`, git policy.

The docs are kept current by the `docs-sync` skill.

Last synced: 3b20ee7 (2026-11-19)
