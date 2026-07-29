# ADR-0004: Authentication & authorization

**Status:** Accepted
**Date:** 2026-07-18

## Context

Authorization drifts inconsistent across features and authors when each handler
invents its own checks. The split between "who are you / what role" and "may
*this* principal act on *this* entity" needs one home each.

## Decision

- **Authentication + coarse (role) authorization** live in **middleware**
  (`platform/auth` + `platform/httpserver`). Middleware verifies the token,
  builds a **principal**, and enforces role gates.
- The **principal is carried in `context.Context`**; handlers and services read
  it from context and never re-parse the token.
- **Resource-level authorization** — "can *this* principal act on *this*
  entity" — lives in the **service layer**, never in the handler. It is a
  business rule that needs the entity loaded, so it belongs with the use case.

Handlers make no authorization decisions.

## Consequences

- Coarse gates are enforced once, uniformly, at the edge.
- Resource authz sits with the business rule and the entity it guards, so it
  stays consistent across features and authors.
- Testable: resource authz is exercised by service unit tests, not HTTP tests.
- The token-verify mechanism is a thin `platform/auth` seam, built when the
  first authenticated feature lands.
