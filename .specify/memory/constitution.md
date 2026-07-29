# Constitution

The non-negotiable engineering principles for this service. This constitution
supersedes convenience and personal preference. It is written for a team scaling
to ~10 engineers across multiple pods: its job is to keep parallel work from
colliding.

Each principle states a rule and how it is **enforced** — an unenforced
principle is a comment, not a rule.

> **Scope of this copy.** These principles were authored for a product spanning
> this Go backend, a Flutter mobile client, and a React admin dashboard. Only
> the backend lives in this repository. The client-side enforcement mechanisms
> named below are kept as written, because they are what makes each principle
> concrete — but the tools that run here are the Go ones.

## Core Principles

### I. Feature Isolation

Code is organized by feature (vertical slice). A feature may depend on another
feature **only through that feature's exported service interface**, declared as a
port owned by the *consumer*. A feature must never import another feature's
internals — backend `postgres/`/`domain`, mobile `data/`, admin `api/`.

- **Why:** this is the property the whole architecture exists to protect. Broken
  once, the dependency graph tangles and never untangles.
- **Enforced:** backend `depguard` (golangci-lint) forbidding cross-feature
  internal imports; admin `eslint-plugin-boundaries`; mobile `custom_lint` /
  layer rule. CI-blocking.

### II. Test-First & Test Shape

Tests are written before the implementation they cover. Ports are tested with
**hand-written fakes** (not mock frameworks). Adapters (DB, external providers)
are covered by **integration tests**, build-tagged / separately runnable so the
default unit run stays fast.

- **Enforced:** CI test gate; coverage reported per package.

### III. Authorization Boundary

Authentication and coarse (role) authorization live in **middleware**.
Resource-level authorization — "can *this* principal act on *this* entity" —
lives in the **service layer**, never in the handler. Handlers do not make
authorization decisions.

- **Why:** resource authz is a business rule and needs the entity; scattering it
  into handlers is how it drifts inconsistent across features and authors.
- **Enforced:** code review against ADR-0004.

### IV. House Go Style (errors, logging, context)

- Errors are **typed and never swallowed**; the HTTP/UI edge maps them.
- Logging is **structured** (Go: `slog`), emitted through a logger carried in
  `context.Context`. No ad-hoc stdout logging.
- `ctx context.Context` is the **first argument** of every I/O-touching
  function; deadlines are honored; no `context.Background()` in request paths.
- Frontend analogue: typed API errors map to explicit UI states; no swallowed
  failures.
- **Enforced:** `golangci-lint` (`errcheck`, `contextcheck`, `sloglint`).

### V. Contract-First

The backend HTTP API is the **single source of truth** for the contract between
backend, admin, and mobile. Client types are **generated** from it — never
hand-written and never allowed to drift.

- **Mechanism:** backend expresses the contract in Go types via **Huma**
  (OpenAPI 3.1 generated from real request/response types, runtime-validated);
  admin generates via **orval**, mobile via **swagger_parser**.
- **Enforced:** CI generated-code **staleness gate** in every repo (regenerate →
  `git diff --exit-code`).

### VI. Simplicity & Split-by-Symptom

Every unit starts flat. Split only when a concrete symptom appears, using the
smallest step that resolves it. Duplicate before extracting shared code; extract
on the second real occurrence. Do not build infrastructure ahead of a consumer.

- **Enforced:** code review.

### VII. Auditability

Every state-changing action on a **sensitive resource** (authentication,
roles/permissions, money, bookings, PII) emits an immutable audit event
recording who did what, to which resource, when.

- **Enforced:** code review; the audit mechanism itself is built when the first
  sensitive-resource feature exists (see `docs/adr/open-decisions.md`).

## Technology & Architecture Constraints

- **Architecture:** hexagonal (ports & adapters), organized by feature. Weighty
  decisions are recorded as ADRs in `docs/adr/`.
- **Backend stack:** Go, chi + **Huma** (HTTP), pgx with SQL held in `.sql`
  files embedded at build time (Postgres), goose (migrations), Cobra (CLI).
- **Client stacks:** admin is Vite + React + TypeScript with **orval** for
  generated clients; mobile is Flutter with **swagger_parser**. Neither lives in
  this repository.
- **Transactions are scoped by database** (ADR-0002): within a single Postgres,
  co-located features may share a transaction only through the `platform/db`
  Unit of Work (never a raw `pgx.Tx` in a public interface); atomicity that
  would span separate databases or services must use a saga. Import isolation
  (Principle I) is absolute regardless.

## Development Workflow & Quality Gates

- Every PR must pass the enforcement gate for each principle above (lint,
  boundary checks, tests, contract staleness).
- Architectural decisions are proposed as an ADR and reviewed before merge.
- Enforcement config is specified from day one; CI wiring activates when the
  first feature lands (there is nothing to lint on an empty repo).

## Governance

- This constitution supersedes other practices. Where a PR conflicts with it,
  the PR changes, not the principle.
- Amendments require a PR editing this file, team review, and a version bump
  below. Superseding a principle requires a linked ADR explaining why.
- ADRs record *decisions*; this constitution records *invariants*. When they
  disagree, the constitution wins and the ADR is corrected.

## Amendments

- **1.2.0** — Backend persistence changed from **sqlc** to hand-written adapters
  over `pgx`, with each statement in its own `.sql` file embedded into a string
  at build time. Rationale and the rejected alternative are in
  [ADR-0010](../../docs/adr/0010-sql-in-embedded-files.md). No principle
  changed; this is a stack constraint under Technology & Architecture.

**Version**: 1.2.0 | **Ratified**: 2026-07-18 | **Last Amended**: 2026-07-29
