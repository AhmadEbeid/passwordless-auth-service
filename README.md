# passwordless-auth-service

A Go service for passwordless phone signup and sign-in: a one-time code
delivered over WhatsApp, optional Google sign-in, and opaque per-device
sessions. There is no password field anywhere in the schema.

Built for a sports-club product, so accounts carry a `coach`/`athlete` role and
phone numbers normalize to Egyptian E.164.

## What it does

**Signup / sign-in.** A client requests a challenge for a phone number. Signup
rejects a number that already has an account, sign-in rejects one that does
not — both before a code is sent, so neither burns a WhatsApp message. The code
is a random six digits, delivered as an AUTHENTICATION-category template
message, and stored only as a bcrypt hash. Confirming it creates the account
(signup) or resumes the existing one (sign-in) and issues a session.

**Abuse limits.** Five wrong codes lock a challenge for 30 minutes. Resends are
behind a 60-second cooldown and a cap of five sends per 30 minutes; tripping the
cap locks the challenge too. Every limit is checked before a send, so a
locked-out number cannot escape by starting a fresh request instead of resending.

**Google sign-in.** A Google ID token is verified against Google's published
JWKS. If it maps to a linked account, that session resumes with no OTP. If it
maps to nothing, the server mints a short-lived signed token proving *that*
identity and hands it back; the client passes it to the phone-verification step,
so the new account is linked to the identity actually proven rather than to a
subject the client could substitute.

**Sessions.** Opaque 256-bit tokens, stored as SHA-256 hashes, with a rolling
60-day idle expiry. Sign-out revokes only the current device.

**Admin.** Behind a shared-secret gate that fails closed: search accounts, clear
a lockout, and read aggregate signup/failure metrics. Clearing a lockout writes
an append-only audit event naming the admin who did it.

## Design notes

A few decisions worth calling out, with the reasoning in the code near each:

- **Two hash functions, deliberately.** The OTP is bcrypt — it is only six
  digits, so the work factor is what makes the space impractical to brute-force
  from a database dump. The session token is 256 random bits, so SHA-256 is
  correct there: fast and indexable, with nothing for bcrypt to add.
- **Send before persisting.** A failed WhatsApp send leaves no challenge row and
  consumes no attempt, so a provider outage doesn't lock users out.
- **Time is injected.** No domain code reads the wall clock; every timed rule
  takes `now` as an argument, which is what makes expiry, cooldown and lockout
  testable without sleeping.
- **The session-expiry write is throttled.** Validation runs on every
  authenticated request, so persisting the rolling expiry each time would put an
  `UPDATE` on the hottest path in the service.

Architecture is hexagonal, organized by feature rather than by layer — see
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) and the ADRs in
[docs/adr](docs/adr) for the decisions and their trade-offs.

## How it is built

Development is spec-driven, using [Spec Kit](https://github.com/github/spec-kit):
a feature starts as a specification, becomes a plan and a task breakdown, and is
implemented against them. Three documents carry the weight, in decreasing order
of permanence:

- [`specs/001-auth-signup-signin/`](specs/001-auth-signup-signin) — the
  specification this service was built from: user stories, FR-001…FR-031, the
  data model, the decisions taken before implementation, and the HTTP contract.
  Requirement ids cited in the ADRs resolve here. Note the spec covers a mobile
  client and an admin dashboard too; only the backend is in this repository, and
  [its README](specs/001-auth-signup-signin/README.md) explains what that means
  for reading it.
- [`.specify/memory/constitution.md`](.specify/memory/constitution.md) — the
  invariants. Boundaries, testing, where auth lives, what must be observable,
  what fails closed. It constrains the ADRs rather than the reverse, and names
  the linter that enforces each principle where one can.
- [`docs/adr/`](docs/adr) — the decisions, with their trade-offs and what was
  rejected. Immutable once accepted; superseded rather than rewritten.
- [`docs/adr/open-decisions.md`](docs/adr/open-decisions.md) — what is
  deliberately *not* built yet, each with the trigger that would change that.

The point of the constitution is that principles are checkable. Where a rule can
be enforced by a tool it is — `depguard` for the import boundary, `sloglint` and
`contextcheck` for logging and context propagation — so the architecture holds
under review pressure instead of eroding.

## Running it

```sh
make up
```

That is the whole setup — Postgres, migrations, then the API. It waits until
the service answers and prints where to reach it. If 5432 or 8080 are already
taken locally, override them: `make up API_PORT=8081 DB_PORT=5433`.

`make down` stops it, `make clean` also drops the database volume, and
`make logs` follows the API.

Without Meta credentials the WhatsApp sender falls back to a stub that logs the
code instead of sending it, so the whole flow is completable locally:

```sh
curl -X POST localhost:8080/auth/verifications \
  -H 'Content-Type: application/json' \
  -d '{"intent":"signup","phone":"01012345678","role":"coach","language":"en"}'

docker compose logs api | grep code      # the OTP the stub "sent"

curl -X POST localhost:8080/auth/verifications/$ID/confirm \
  -H 'Content-Type: application/json' -d '{"code":"123456"}'
```

The confirm returns a session token; pass it as `Authorization: Bearer <token>`
to `GET /auth/session`.

Against your own Postgres:

```sh
export DATABASE_URL=postgres://authsvc:authsvc@localhost:5432/authsvc?sslmode=disable
go run . migrate up
go run . serve
```

The OpenAPI spec is generated from the handlers and committed at
[docs/openapi.yaml](docs/openapi.yaml), so the contract is readable without
running anything. CI regenerates it and fails on any diff, which is what stops
it drifting from the code:

```sh
make openapi         # regenerate
make openapi-check   # what CI runs
```

## Development

Prerequisites: Go 1.26+, and these on your `PATH`:

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go install mvdan.cc/gofumpt@latest
go install github.com/evilmartians/lefthook@latest   # or: brew install lefthook
lefthook install                                     # once per clone
```

```sh
make test              # go test ./... -race -cover
make test-integration  # the tagged suite, needs Docker
make lint              # golangci-lint v2
make vuln              # govulncheck
```

`make help` lists every target.

Tests are stdlib-only with hand-written fakes; the repository layer is covered
by integration tests against a real Postgres via testcontainers, behind the
`integration` build tag. Note that `make test` therefore reports 0% for
`internal/auth/postgres` — that package is 75% covered, but only with the tag:

```sh
go test -tags integration ./...   # needs a running Docker daemon
```

CI runs both suites — the tagged one in its own job, since the statements in
`postgres/queries/` are otherwise never executed anywhere but a developer's
machine. A `depguard` rule keeps `pgx` out of every package
except the Postgres adapter, so the architecture boundary is enforced by the
linter rather than by convention.

## Status

The WhatsApp and Google adapters are written against the documented Meta and
Google contracts but have not been exercised against live credentials — neither
a Meta Business account nor a Google OAuth client was provisioned for this
build. Both fall back cleanly and say so at warn level on startup: the sender
logs codes instead of delivering them, the verifier rejects every token.

`GOOGLE_LINK_SECRET` is the one fallback that becomes a hard failure: with
`APP_ENV=production` and no secret set, the process refuses to start, because
improvising a per-process one silently breaks any Google signup that crosses a
replica or a restart.
