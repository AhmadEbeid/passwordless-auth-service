# ADR-0002: Cross-feature communication & transactions

**Status:** Accepted
**Date:** 2026-07-18

## Context

Two questions are genuinely hard in a feature-sliced design: how features
communicate across a boundary, and how atomicity is achieved when an invariant
spans more than one feature. Both are where the dependency graph starts to
tangle as multiple pods build in parallel, so this ADR settles both —
communication through consumer-owned ports, atomicity scoped by database.
Feature isolation is the property the whole architecture exists to protect
(see the architecture guide).

## Decision

**(a) Communication.** A feature depends on another feature *only* through that
feature's exported **service interface**, declared as a port **owned by the
consumer**. A feature never imports another feature's `postgres/` or `domain`
internals. Consumer-owned ports mean each feature declares the narrow surface it
actually needs; the provider stays free to change everything behind it.

**(b) Transactions.** Atomicity is scoped by database, not by feature.

- Prefer single-owner consistency boundaries: design so one feature owns each
  atomic invariant where reasonable, and coordinate the rest through ports or
  asynchronous events. A shared transaction is a deliberate tool for a genuine
  cross-feature invariant, not the default way features talk.
- Same database (this platform today): a shared transaction is ALLOWED via the
  platform Unit of Work. Co-located features may participate in one transaction
  when a real invariant requires it (e.g. debit wallet + create booking). It is
  opened by a higher-level use-case (e.g. a `checkout` service) that depends on
  both features' ports. Two rules keep it safe: (1) a raw `pgx.Tx` never appears
  in a feature's public service interface — that leak couples features and
  defeats extraction; (2) transaction orchestration is confined to that
  higher-level use-case, never scattered across features. Preferred mechanism:
  the orchestrator constructs transaction-bound instances of each participating
  service inside `InTx` (each built from a `WithTx(tx)` querier), so the
  transaction is explicit at construction and method signatures stay clean.
  Ambient transaction-in-`context` is an acceptable fallback where per-request
  construction is too heavy, but it is implicit and should be used sparingly.
  Each feature's adapter still enforces its own invariants inside the
  transaction (e.g. the wallet's no-overspend conditional UPDATE).

  Preferred mechanism — tx-bound construction:

  ```go
  // a higher-level use-case (checkout) owns the transaction
  func (c *Checkout) Book(ctx context.Context, req Req) error {
      return c.uow.InTx(ctx, func(tx pgx.Tx) error {
          wallet  := wallet.NewService(c.walletDeps.WithTx(tx))
          booking := booking.NewService(c.bookingDeps.WithTx(tx))
          if err := wallet.Debit(ctx, req.User, req.Amount); err != nil {
              return err
          }
          return booking.Create(ctx, req)   // normal signatures — no tx param
      })
  }
  ```
- Across databases or services: a shared transaction is impossible — use a saga:
  hold/capture with compensating actions, durable saga state (the entity row or
  a saga/outbox table as the state machine), idempotent steps keyed by stable
  ids, plus TTL/expiry and a reconciliation backstop.
- Never hold a database transaction open across an external API call (e.g. a
  payment provider). External side effects are coordinated with idempotency +
  compensation, never a DB transaction.

**(c) Intra-feature atomicity** uses a `platform/db` Unit of Work: `TxManager`
hands the sqlc-generated `Queries.WithTx(tx)` to repos *within one feature*.

```go
type TxManager interface {
    InTx(ctx context.Context, fn func(ctx context.Context) error) error
}
// inside ONE feature's service:
err := uow.InTx(ctx, func(ctx context.Context) error {
    q := s.queries.WithTx(db.Tx(ctx))   // sqlc-generated WithTx
    if err := q.InsertBooking(ctx, ...); err != nil { return err }
    return q.ReserveSlots(ctx, ...)
})
```

## Consequences

- Sharing a transaction couples the participating features to the same database
  and forfeits independent extraction of that pair. Accepted deliberately:
  the platform runs a single Postgres and has no plan to separate
  services/databases.
- Revisit if an extraction trigger appears — independent scaling, separate team
  ownership, reuse by a separate product, or compliance isolation. The
  co-committing pair then migrates to a saga (a bounded, known migration, not a
  rewrite).
- Import isolation remains absolute regardless; only the transaction rule is
  scoped to the single-database reality.

## Worked examples

- External charge (Stripe/provider): the money moves outside your DB, so there
  is no transaction to share — always idempotency + compensation, never a DB tx
  across the call.
- Shared capability (a payment domain used by many features): consume it through
  ports; coordinate atomic local invariants via the same-DB Unit of Work, or via
  events where atomicity is not required. Do not let every consumer open
  transactions on its tables ad hoc.
- Internal wallet (same DB — this platform's case): debit wallet + create booking in
  one transaction opened by the `checkout` use-case via the platform UoW; the
  wallet enforces its own no-overspend guard inside it. Recommended here; no
  saga.

## Enforcement

A `depguard` rule (golangci-lint) forbids cross-feature internal imports — an
`internal/<a>/...` package importing `internal/<b>/postgres` or `<b>` domain
types. Added to the lint config; CI-blocking once the first feature lands.

## Alternatives considered

- **Pass a `tx` (or `TxManager`) through service interfaces so callers compose a
  transaction across features.** Rejected: it leaks persistence through the
  port, recouples features to each other's storage, and breaks service
  extraction — the exact properties this ADR exists to protect.
