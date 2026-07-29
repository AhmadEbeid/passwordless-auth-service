# ADR-0001: Hexagonal + feature-slice architecture

**Status:** Accepted
**Date:** 2026-07-18

## Context

The backend must stay ownable and extractable as the team grows to ~10
engineers across multiple pods. Organizing by technical layer (top-level
`domain/`, `usecase/`, `http/`, `repository/`) scatters one feature across four
folders and forces every pod to touch shared trees. This ADR records the
architecture the repo already follows.

## Decision

Hexagonal (ports & adapters), organized by **feature / vertical slice**. Each
feature is a folder under `internal/<feature>/` that owns its full stack:

- `domain.go` — entity + business rules (pure, no I/O)
- `ports.go` — interfaces the feature needs, defined by the consumer
- `service.go` — use-case orchestration, depends only on `ports.go`
- `handler.go` — HTTP adapter (chi/Huma), the driving side
- `postgres/` — adapter implementing the repository port

Cross-cutting infrastructure (DB pool, config, HTTP server, external clients)
lives in `internal/platform/`, never in a feature. Each feature declares its own
port for what it needs from platform, so two features using the same client do
not become coupled through it.

## Consequences

- A feature is a folder one engineer/pod owns outright.
- Features are independently ownable and **extractable**: because a feature
  talks to the outside only through its own ports, pulling it into its own
  service is a folder move plus swapping an in-process call for an RPC client
  that implements the same port — not a rewrite.
- The port/adapter split keeps domain logic testable without I/O.
- The cross-feature rules this enables — isolation and transactions — are
  load-bearing and recorded separately in ADR-0002.
