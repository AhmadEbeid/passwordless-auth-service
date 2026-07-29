# ADR-0007: Session strategy

**Status:** Accepted
**Date:** 2026-07-20

## Context

The auth feature is the first to issue user sessions. The consumer client
is a mobile app, and the product needs per-device sign-out, admin-driven
state changes, and idle-expiry. The session mechanism must
support all three, and it establishes the precedent every later authenticated
feature inherits, so this ADR records it once.

## Decision

Sessions are **opaque random tokens**, **stored server-side**, **hashed at
rest**, **one row per device**, and **revocable**. They are **not JWTs**.

- **Opaque token**: a 256-bit random value minted with `crypto/rand`, returned
  to the client once at confirm. It carries no claims.
- **Server-stored, hashed at rest**: the `session` row holds only a SHA-256 hash
  of the token (SHA-256, not bcrypt — the token is high-entropy, so the at-rest
  hash must be fast and indexable for per-request lookup, and needs no work
  factor). A DB read never reveals a usable token.
- **Per-device**: multiple concurrent rows per account (`device_label`
  distinguishes them); sign-out revokes only the current row.
- **Revocable**: a row is valid **iff** `revoked_at IS NULL AND now <
  expires_at`. Sign-out sets `revoked_at`; an admin can revoke server-side.
- **Rolling 60-day idle-expiry**: `expires_at` is extended on activity;
  60 days is the midpoint of the ~30–90-day range the product wanted. All
  timing derives from the injected `Clock`, never the wall clock in logic.
- **The extension write is throttled** to at most once per `SessionExtendInterval`
  (24h). Validation runs on every authenticated request, so persisting the new
  expiry each time would put an `UPDATE` on the hottest path in the service to
  move a deadline that is already 60 days out. The cost is that a session's
  recorded `last_active_at` can lag reality by up to the interval, which is far
  inside the window it feeds.

The `session` table's full schema (including `device_label`, `last_active_at`,
`revoked_at`) is created up front, though only session **creation** shipped
with the first slice; **validate/revoke** landed later.

## Platform authn without a platform→feature import

Authentication middleware lives in `platform/auth` (ADR-0004), but the session
store is **owned by the auth feature**. To keep the platform from importing a
feature (which ADR-0002 forbids), the middleware must not reach into
`internal/auth`. Instead the platform **owns a session-validator interface** and
the composition root (`cmd/serve.go`) **injects** an implementation backed by
the auth feature's session repository. The dependency
arrow points inward (composition root → both), so there is no platform→feature
import and the feature stays independently extractable.

## Consequences

- Revocation, per-device sign-out, and idle-expiry are trivial with server-side
  state — the properties JWTs make awkward.
- Every request that validates a session costs one indexed DB lookup by token
  hash; acceptable, and revisited only if a read path is *measured* hot.
- The injected-validator seam keeps the platform/feature boundary intact when
  authn middleware arrives.

## Alternatives considered

- **JWT** — rejected: revocation and rotation add complexity for no v0 benefit;
  per-device server-side state is required regardless.
- **Signed cookies** — rejected: the primary client is mobile, not a browser.
- **bcrypt for the token hash** — rejected: unnecessary (high-entropy token) and
  not indexable for per-request validation.
