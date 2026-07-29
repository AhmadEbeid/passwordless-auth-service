# Architecture Decision Records

An ADR records one weighty architectural decision — its context, the choice,
and the consequences — so the *why* survives past the people who were in the
room. One file per decision. Decisions are immutable once Accepted: supersede
with a new ADR, do not rewrite history.

ADRs record **decisions**; the [constitution](../../.specify/memory/constitution.md)
records **invariants**. When they disagree, the constitution wins and the ADR is
corrected.

## When to write one

- A choice that constrains how features are built — boundaries, transactions,
  contract, auth, migrations, observability.
- A choice a future engineer would otherwise reopen for lack of the rationale.

Small, reversible, feature-local choices do not need an ADR.

## Format

Status / Context / Decision / Consequences (add Alternatives where a rejected
option is worth recording). Status is one of Proposed / Accepted / Superseded.

## Index

| ADR | Title | Status |
|---|---|---|
| [0001](0001-hexagonal-feature-slice-architecture.md) | Hexagonal + feature-slice architecture | Accepted |
| [0002](0002-cross-feature-communication-and-transactions.md) | Cross-feature communication & transactions | Accepted |
| [0003](0003-database-migrations.md) | Database migrations | Accepted |
| [0004](0004-authentication-and-authorization.md) | Authentication & authorization | Accepted |
| [0005](0005-api-contract-and-client-generation.md) | API contract & client generation | Accepted |
| [0006](0006-observability-baseline.md) | Observability baseline | Accepted |
| [0007](0007-session-strategy.md) | Session strategy | Accepted |

Deferred decisions — consciously postponed, each with a build trigger — live in
[open-decisions.md](open-decisions.md).
