# passwordless-auth-service

Go service for passwordless phone signup and sign-in: WhatsApp OTP, Google
sign-in, opaque per-device sessions. No password field exists in the schema.

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan
<!-- SPECKIT END -->

## Read first

`.specify/memory/constitution.md` holds the invariants — boundaries, testing,
auth placement, observability, contract, build-ahead, audit. It constrains the
ADRs, not the reverse. `docs/adr/` records decisions and their trade-offs;
`docs/ARCHITECTURE.md` describes the layout.

## Layout

```
internal/<feature>/        vertical slice: domain, ports, service, handler
  postgres/                its adapter; queries/*.sql embedded into strings
internal/platform/         shared seams: config, db, httpserver, auth, observability
cmd/                       composition root — chooses and wires, never implements
migrations/                goose, timestamped, one shared history
```

## Commands

```sh
make up                # Postgres, migrations, API; prints where it listens
make test              # unit, race, coverage
make test-integration  # tagged suite against real Postgres, needs Docker
make lint              # golangci-lint v2
make vuln              # govulncheck
```

## Conventions that get enforced

- `pgx` only in `platform/db`, a feature's `postgres/`, and `cmd/` — `depguard`
  fails the build otherwise.
- Structured logging via `slog`; `context.Context` threaded, not reconstructed.
  `sloglint` and `contextcheck` enforce both.
- Domain code never reads the wall clock. Timed rules take `now` as an argument.
- SQL lives in `postgres/queries/*.sql`, embedded into a `string` so a missing
  file is a build error. Adding one means adding its `//go:embed` var.
- Comments explain why, not what. One or two lines. No references to documents
  that are not in this repository.
- Security-relevant behaviour needs a test that fails when the behaviour is
  broken — check by breaking it.

## Traps

- `serve` does not run migrations; compose runs them as a one-shot service.
- Without Meta credentials the sender logs the code at **debug** instead of
  sending it. At the default info level the OTP will not appear.
- `GOOGLE_LINK_SECRET` is required when `APP_ENV=production`; blank elsewhere
  generates a per-process secret and warns.
