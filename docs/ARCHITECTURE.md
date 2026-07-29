# passwordless-auth-service architecture

## Pattern

Hexagonal architecture (ports & adapters), organized by feature.

Each feature is a vertical slice under `internal/<feature>/` and owns its
full stack — entity, ports, use-case logic, HTTP adapter, and persistence
adapter. There is no top-level `domain/`, `usecase/`, `repository/`, `http/`:
organizing by layer scatters one feature's code across four folders, which
does not scale with team size. Organizing by feature gives each
engineer/pod a folder they own outright.

## Feature folder template

```
internal/<feature>/
├── domain.go        entity + business rules
├── ports.go          interfaces this feature needs (repository, external
│                     providers) — defined by the consumer, not centrally
├── service.go         use-case orchestration, depends only on ports.go
├── handler.go          HTTP adapter (chi), implements the driving side
└── postgres/
    ├── repository.go   adapter implementing the repository port
    ├── queries.go      one embedded string per statement
    └── queries/        the statements themselves, one .sql file each
```

A feature folder is created when work on that feature begins.

## Shared infrastructure

Cross-feature infrastructure — DB connection pool, config loading, chi
router/middleware setup, WhatsApp client — lives in `internal/platform/`:

```
internal/platform/
├── db/            Postgres connection pool (pgx) + Unit of Work (ADR-0002)
├── config/        env-based configuration
├── httpserver/    Huma + chi router setup, middleware (request ID, recoverer,
│                  CORS, logging, authn)
├── observability/ (new) slog setup, ctx helpers, OTel tracer seam (ADR-0006)
├── errors/        (new, thin) error taxonomy + HTTP mapper
├── auth/          (new, thin) token-verify middleware + principal extraction
└── whatsapp/      WhatsApp API client
```

Each feature defines its own port for what it needs from shared infra, so
two features depending on the same client (e.g. WhatsApp) do not become
coupled to each other.

`flags/` and `audit/` are **not** pre-built: they are domain-shaped and must be
defined by their first consumer (as a consumer-owned port), not added to
`platform/` speculatively. See `docs/adr/open-decisions.md`.

## Cross-feature communication

A feature depends on another feature *only* through that feature's exported
service interface, declared as a port **owned by the consumer** — never by
importing its `postgres/` or `domain` internals.
This is what keeps features independently ownable and extractable. See
[ADR-0002](adr/0002-cross-feature-communication-and-transactions.md).

## Transactions

**Atomicity is scoped by database, not by feature.** Within our single Postgres,
co-located features *may* share one transaction when a real cross-feature
invariant requires it (e.g. debit wallet + create booking). The transaction is
orchestrated by a higher-level use-case depending on both ports; preferred
mechanism is tx-bound construction (services built from `WithTx(tx)` inside
`InTx`), with ambient tx-in-context as a sparing fallback; a raw `pgx.Tx` never
appears in a public service interface. Each feature's adapter still enforces its
own invariants inside the transaction. Atomicity spanning separate databases or
services is impossible
in one tx — use a saga (compensating actions + durable state + idempotent
steps). Never hold a transaction open across an external API call (e.g. a
payment provider); coordinate those with idempotency + compensation.

Intra-feature atomicity uses the same `platform/db` Unit of Work
(`TxManager.InTx` handing the sqlc-generated `Queries.WithTx(tx)` to repos within
one feature). See
[ADR-0002](adr/0002-cross-feature-communication-and-transactions.md).

## Migrations

A single goose directory (`migrations/`); feature owners contribute timestamped
migrations. The schema is a shared cross-cutting resource, reviewed as such.
Zero-downtime changes follow expand-contract (backward-compatible add → migrate
data/code → contract). See [ADR-0003](adr/0003-database-migrations.md).

## API contract

The backend is the contract source of truth, expressed as Go types via **Huma**
(OpenAPI 3.1 generated from real request/response types, runtime-validated, on
chi via humachi). Huma I/O structs are transport DTOs confined to the handler,
mapped to/from domain at the service boundary. CI exports `openapi.yaml`; admin
generates clients via **orval**, mobile via **swagger_parser**; a staleness gate
fails on stale generated output. See
[ADR-0005](adr/0005-api-contract-and-client-generation.md).

## Observability

Structured logging via `slog` through a logger carried in `context.Context`; a
request ID is generated in middleware and carried alongside it so every line
correlates. An OpenTelemetry tracer-provider seam exists now; no exporter is
wired until needed. No ad-hoc stdout logging. See
[ADR-0006](adr/0006-observability-baseline.md).

## CLI

Cobra. Root `main.go` is a thin wrapper calling `cmd.Execute()`; `cmd/`
holds command definitions (`serve`, `migrate`, and future subcommands).

## Stack

| Concern      | Choice        |
|--------------|---------------|
| HTTP router  | chi           |
| DB access    | sqlc + pgx (Postgres) |
| CLI          | Cobra         |

## Growth rules — when and how to split

Every feature starts flat, exactly as shown in the template above. Split
only when a concrete symptom appears, using the smallest step that resolves
it:

| Symptom | Action |
|---|---|
| A file exceeds ~300–400 lines or covers more than one responsibility | Split into more files, same package (e.g. `service.go` → `service_create_booking.go`, `service_cancel_booking.go`). No architectural change. |
| A port interface exceeds ~5–7 methods, or mixes reads and writes | Split the interface by consumer: `CoachReader` / `CoachWriter` instead of one `CoachRepository`. The adapter still implements both; each service file declares only the interface it uses. |
| A use case mixes business rules with I/O orchestration, making the rule hard to test | Separate a pure domain service (rules only, no I/O) from the application service (orchestrates repo + external calls). |
| A feature accumulates multiple sub-concerns (e.g. pricing, availability, cancellation policy) | Apply the feature template one level deeper — sub-features nest directly under the feature: `internal/booking/pricing/`, `internal/booking/availability/`, each with its own `domain.go`/`ports.go`/`service.go`/`handler.go`. |
| A feature needs independent scaling, on-call, or ownership | Extract the folder into its own service. Because it only communicates through its own ports, this is a folder move plus swapping an in-process call for an HTTP/gRPC client implementing the same port — not a rewrite. |

Rule of thumb: split by symptom, not by anticipation.

## Shared code policy

- Shared code lives in `internal/platform/`. There is no top-level `pkg/` —
  `pkg/` signals "safe for other modules to import," which does not apply to
  an application backend.
- Code moves to `platform/` only once two features need the exact same
  implementation unchanged. Duplicate first; extract on the second real
  occurrence.
- A feature-local helper stays inside that feature's folder, regardless of
  how generic it looks, until a second feature needs it.

## Enforcement

Principles are enforced mechanically, not by convention alone:

- **Feature isolation:** a `depguard` rule (golangci-lint) forbids cross-feature
  internal imports; see [ADR-0002](adr/0002-cross-feature-communication-and-transactions.md).
- **House Go style:** `golangci-lint` (`errcheck`, `contextcheck`, `sloglint`,
  …) enforces typed errors, `ctx`-first I/O, and structured logging.
- **Contract staleness:** CI regenerates clients from `openapi.yaml` and runs
  `git diff --exit-code` (ADR-0005).

Enforcement config is specified from day one; CI wiring activates when the first
feature lands (there is nothing to lint on an empty repo).

## Deferred-with-trigger

Some capabilities are consciously deferred — decided in principle, built only
when a concrete trigger fires. Full table in
[adr/open-decisions.md](adr/open-decisions.md):

| Capability | Build when… |
|---|---|
| Feature flags | decoupling deploy from release for real users |
| Audit store impl | the first sensitive-resource feature exists |
| Rate limiting | a publicly reachable abuse surface exists |
| Idempotency keys | a non-idempotent public POST is retried by clients |
| Caching | a read path is *measured* hot |
| Outbox worker | the first genuine cross-feature async need appears |
| SLOs / alerting | running in production with real users |

`flags/` and `audit/` are not pre-built platform packages — they are
domain-shaped and defined by their first consumer.
