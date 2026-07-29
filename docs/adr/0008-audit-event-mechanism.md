# ADR-0008: Audit event mechanism

**Status:** Accepted
**Date:** 2026-07-22

## Context

Auth is the first feature to touch a sensitive resource — an administrator
changing another principal's authentication state — so this is where an audit
mechanism first has to exist. `open-decisions.md` deferred "audit-store
implementation" until that trigger fired; it has now. We do not build ahead of a
real consumer, so the mechanism must stay minimal rather than become a
framework.

## Decision

A single append-only `audit_event` table plus one internal `emitAuditEvent`
call on the auth `Service`, with exactly one emitter for v0: clearing a
verification lockout.

- `audit_event(id, actor_admin_id, action, target_account_id, created_at, metadata)`
  — no update/delete path exists; the row is written once and read only.
- `actor_admin_id` is a plain string, not a foreign key to any admin-user
  table — admin identity is out of this feature's scope (see ADR-0009).
- `EmitAuditEvent`-equivalent logic lives in `internal/auth` (consumer-owned,
  per `open-decisions.md`: audit is domain-shaped and defined by its first
  consumer, not a speculative platform package).
- A no-op admin action (e.g. clearing a lockout that isn't active) emits
  nothing — the table records effects, not attempts.

## Consequences

- Adding a second auditable action (or a second feature that needs
  auditing) is additive: a new `action` value and emit call, or — if a
  second feature needs the same shape — promoting this to a platform
  package at that point, not before.
- No audit read/query API ships with this feature. For v0 the record only has
  to exist and be inspectable, e.g. directly via SQL.

## Alternatives considered

- A generic event bus / audit framework: rejected, no second consumer exists
  yet.
- Logging only: rejected — not immutable or queryable independent of log
  retention, so it does not satisfy the audit-record requirement.
