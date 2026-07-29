# ADR-0003: Database migrations

**Status:** Accepted
**Date:** 2026-07-18

## Context

Features own their slices, but they share one Postgres database. Schema is a
cross-cutting resource; if each feature kept its own migration history they
would collide and ordering would be undefined.

## Decision

A **single goose migrations directory** (`migrations/`, already at the repo
root). Feature owners contribute **timestamped** migrations to it; goose applies
them in order. The schema is a **shared cross-cutting resource**: a migration PR
is reviewed as a change to shared surface, not a feature-local edit.

Schema changes that must ship without downtime use **expand-contract**:

1. **Expand** — add the new shape (nullable column, new table, new index)
   backward-compatibly; deploy.
2. **Migrate** — backfill data and move code to the new shape; deploy.
3. **Contract** — drop the old shape once nothing reads it; deploy.

Never rename or drop-and-re-add in a single step against a running system.

## Consequences

- One ordered, authoritative history; no per-feature drift.
- Migrations are a deliberate review surface, catching cross-feature schema
  impact early.
- Zero-downtime change is the default discipline, not a special case.
