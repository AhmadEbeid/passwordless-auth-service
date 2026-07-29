# ADR-0009: Admin authorization placeholder

**Status:** Accepted
**Date:** 2026-07-22

## Context

The admin endpoints need a coarse `is-admin` gate
in front of `/admin/*`. The spec is explicit that a real admin
authentication/authorization mechanism is **out of this feature's scope** —
assumed to already exist or be provided elsewhere —
and the test plan only calls for "a test-only way to obtain an
authorized-admin session," without specifying how. There is no admin-user
table anywhere in this data model, and inventing one would both exceed this
feature's stated scope and pre-build infrastructure a separate admin-auth
feature is expected to own.

## Decision

A single configured shared secret gates every `/admin/*` endpoint, carrying a
configured display identity for the audit trail:

- `ADMIN_API_KEY` (required to reach any `/admin/*` route) checked against
  the request's `Authorization: Bearer <key>` header.
- `ADMIN_ID` (a plain configured string, e.g. `ops-team`) is the identity
  attached to the request as the admin principal — it is what
  `audit_event.actor_admin_id` (typed "text/uuid, admin
  auth assumed external") records.
- Implemented in `internal/platform/auth` (ADR-0004: authn/coarse authz
  live in middleware) as a self-contained middleware with no dependency on
  the `auth` feature or any session/account table — it is unrelated to the
  consumer session mechanism (ADR-0007) and does not reuse its tokens.
- Missing/blank `ADMIN_API_KEY` means every admin request is rejected — the
  gate fails closed, never open.

## Consequences

- There is exactly one admin identity for v0, not one per human operator —
  audit records show *that* an admin acted, not *which* one. Acceptable
  because the spec assumes a real admin-auth feature is coming and only
  asks this feature to enforce the coarse gate correctly.
- Rotating or replacing this mechanism later is additive: swap
  `platform/auth`'s admin middleware implementation without touching
  `internal/auth`'s business logic, since handlers only ever see the
  resulting principal, never the credential.
- The shared secret must be provisioned outside version control (deployment
  secret store); this ADR does not specify where.

## Alternatives considered

- A minimal `admin_user` table with per-admin credentials: rejected — this
  duplicates the "existing admin authentication mechanism" the spec already
  assumes exists elsewhere, and adds a login surface (password/OAuth/etc.)
  this feature would then have to secure and maintain for no product
  requirement.
- Reusing the consumer `session`/`user_account` tables with an `is_admin`
  flag: rejected — conflates the coach/athlete identity this feature owns
  with an operationally distinct admin identity the spec explicitly
  separates ("distinct from the coach/athlete auth this feature
  specifies").
