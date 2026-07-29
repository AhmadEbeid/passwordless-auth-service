# ADR-0010: SQL lives in embedded .sql files, not inline strings

**Status:** Accepted
**Date:** 2026-07-29

## Context

Every statement the Postgres adapter runs was a backtick `const` beside the
method that ran it. That is idiomatic Go and keeps a query next to its caller,
but it costs three things as the query set grows:

- No editor or tooling understands the SQL. It is a Go string, so there is no
  highlighting, no formatting, and no path to running a SQL linter in CI.
- Diffs read as Go string churn rather than as schema access changes, which is
  the wrong review surface for the statement that decides whether an index gets
  used.
- Running a statement by hand — `EXPLAIN ANALYZE` against a real plan — means
  unpicking Go quoting and concatenation first.

Two queries also shared a `selectVerificationCols` const spliced in with `+`,
which made them unreadable as SQL and impossible to run without reassembling.

## Decision

Each statement lives in its own file under the adapter's `queries/` directory
and is embedded into a package-level `string`:

```go
//go:embed queries/account_find_by_phone.sql
var qAccountFindByPhone string
```

**Embedded into a `string`, not an `embed.FS`.** This is the load-bearing part.
A map or FS lookup by name turns a typo into a runtime failure on a live
request; embedding into a named variable keeps exactly the compile-time
guarantee the `const` had — a renamed or deleted file fails the build.

The files stay inside the feature's own `postgres/` package rather than moving
to a central `db/queries/` tree. That keeps the feature slice owning its full
stack (ADR-0001) and keeps the adapter inside the `**/postgres/**` path the
depguard rule allows pgx in.

The shared column-list const is gone; both verification selects now spell their
columns out.

## Consequences

- SQL is readable, diffable, and directly runnable against a database.
- A SQL formatter or linter can be added to CI later with no code movement.
- Reading one repository method now means opening two files. This is the real
  cost, and it is the reason the query files are named after the method that
  runs them.
- Spelling the verification columns out twice removes the structural guarantee
  that both selects matched `scanVerification`'s scan order. Both files carry a
  comment saying so, and the integration tests fail loudly on a mismatch, but
  the invariant is now maintained by convention rather than by construction.
- Two unit tests guard the wiring: every embedded statement is non-empty and
  contains a statement verb, and every `.sql` file on disk is embedded by
  exactly one variable, so an orphaned file cannot sit in `queries/` looking
  live.

## Alternatives considered

- **sqlc** (as used in sibling projects): generates typed Go from `.sql` files
  and validates them against the schema at build time. Rejected for now: it
  adds a code generator to dev and CI, a generated package larger than the
  adapter it replaces, and a drift check — real value at a hundred-plus queries
  across a team, disproportionate at nineteen in one feature slice. The move to
  `.sql` files makes adopting it later a smaller step, not a larger one.
- **`embed.FS` plus a name lookup**: rejected, trades a build error for a
  runtime one.
- **One file per entity with `-- name:` markers**: rejected, that is a parser,
  and writing one is how you end up maintaining a worse sqlc.
