# ADR-0005: API contract & client generation

**Status:** Accepted
**Date:** 2026-07-18

## Context

Backend, admin, and mobile share one HTTP contract. Hand-written client types
drift from the server the moment either side changes. We need one source of
truth and generated clients, with drift caught mechanically.

## Decision

The **backend is the contract source of truth**, expressed as **Go types via
Huma**:

- Huma operations are declared in each feature's `handler.go`; **OpenAPI 3.1**
  is generated from the actual request/response Go types (no comment drift),
  with **runtime request validation**. Huma runs on chi via the **humachi**
  adapter.
- Huma I/O structs are **transport DTOs confined to the HTTP adapter layer**;
  they map to/from domain types at the service boundary (hexagonal-clean).
- CI exports **`openapi.yaml`**. **admin** generates typed TanStack Query hooks
  via **orval**; **mobile** generates **freezed** models + client via
  **swagger_parser**.
- A CI **staleness gate** in every repo (regenerate → `git diff --exit-code`)
  fails if committed generated output is out of date.
- **Spectral** lints the spec; **oasdiff** flags breaking changes on PRs.

### Schema guardrails

- Explicit `discriminator` on every `oneOf` / `anyOf`.
- Keep unions **shallow**; prefer **flat + enum** over polymorphism in
  responses.
- Money as **integer minor units** or **string decimal** — never float.

## Consequences

- Clients cannot silently diverge from the server; the gate blocks stale output.
- DTO/domain separation keeps transport concerns out of the core.
- Spectral + oasdiff make contract quality and breaking changes a PR surface.

## Alternatives considered

- **swaggo** (annotation comments) — **rejected**: annotations are unchecked
  comments that drift from the code they describe.
- **Spec-first (oapi-codegen)** — viable, but chosen against: the team prefers
  the backend-in-Go as the single source of truth over hand-editing a spec file.
  **`ogen`** is the escape hatch if response polymorphism ever gets heavy enough
  that Huma's ergonomics strain.
