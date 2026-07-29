# Implementation Plan: Auth: Sign Up & Sign In

**Branch**: `001-auth-signup-signin` | **Date**: 2026-07-20 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-auth-signup-signin/spec.md`

## Summary

Passwordless authentication (WhatsApp OTP + Google OAuth) for Coaches and Athletes across three surfaces of one feature: a **Flutter mobile app** (launch routing + sign-up/sign-in), a **Go backend** (the auth service and the contract that binds everything), and a **React admin dashboard** (account visibility, lockout support, analytics). Egypt-only (+20) for v0.

Technical approach: a hexagonal Go service with auth-**owned ports** for the two external identity channels (a WhatsApp verification sender and a Google identity verifier), **opaque revocable sessions**, **hashed OTPs**, an **injectable clock** for every timed rule, and a **minimal append-only audit mechanism** (this is the first sensitive-resource feature, so it births auditing per Constitution VII). Contract-first: the Huma-generated OpenAPI is the single source of truth and drives orval (admin) and swagger_parser (mobile) clients. Full-system validation is defined in [testing-strategy.md](./testing-strategy.md).

## Technical Context

**Language/Version**: Go (backend); Dart/Flutter 3 (mobile); TypeScript/React (admin)

**Primary Dependencies**: backend — Huma v2, chi v5, pgx v5, sqlc, goose v3, Cobra; admin — Vite, React, TanStack Query/Router, Zustand, orval; mobile — Riverpod 3, go_router 17, freezed, swagger_parser

**Storage**: PostgreSQL (18-alpine, via the backend `docker-compose.yml`); no other datastore

**Testing**: Go `testing` + hand-written port fakes (unit) + testcontainers-Postgres (adapter integration); Flutter `test` + `integration_test` on the iOS Simulator; Vitest + Playwright (admin); full-system E2E per [testing-strategy.md](./testing-strategy.md)

**Target Platform**: Linux container (backend); iOS first, Android later (mobile); modern browsers (admin)

**Project Type**: Multi-repo — web service + mobile app + admin SPA (three vertical-slice surfaces of one feature)

**Performance Goals**: user-facing task targets from the spec (sign-up < 2 min, sign-in < 90 s; SC-001/002); OTP delivery perceived-instant. No hard system-latency SLA is defined for v0.

**Constraints**: Egypt-only (+20, FR-003); no passwords anywhere (FR-017); WhatsApp OTP + Google OAuth are the only identity proofs; bilingual Arabic (RTL) / English on mobile (FR-027); per-device sessions (FR-031); every admin auth-state change audited (FR-023, Constitution VII); feature isolation + contract-first (Constitution I, V)

**Scale/Scope**: v0 Egypt launch; single Postgres; 7 user stories / 31 functional requirements; ~9 endpoints; no explicit concurrency target for v0

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design (see [Post-Design re-check](#post-design-constitution-re-check)).*

| Principle | Assessment |
|-----------|------------|
| I. Feature Isolation | **PASS** — `auth` is a vertical slice in each repo, depending only on shared `platform/*` (db, observability) and on auth-**owned** ports (verification sender, identity verifier), never another feature's internals. |
| II. Test-First & Test Shape | **PASS** — ports covered by hand-written fakes (WhatsApp, Google, clock); adapters by testcontainers integration tests; system behavior by E2E ([testing-strategy.md](./testing-strategy.md)). Tests precede implementation. |
| III. Authorization Boundary | **PASS** — session validation + coarse `is-admin` role check live in middleware; the thin resource-level check for admin actions lives in the service layer. Handlers make no authz decisions. |
| IV. House Go Style | **PASS (with note)** — typed errors, `ctx` first, structured logging via the platform's context-carried logger. **Note:** the platform currently uses `zap` while the constitution names `slog`; auth logs through the platform abstraction — the slog/zap reconciliation is a platform-level item, not an auth deviation. |
| V. Contract-First | **PASS** — Huma OpenAPI is the source of truth; admin (orval) + mobile (swagger_parser) clients generated with staleness gates. **Greenfield note:** neither generator is wired yet — Phase 0 stands them up. |
| VI. Simplicity & Split-by-Symptom | **PASS** — flat vertical slice; the audit mechanism is kept **minimal** (append-only table + one emit call consumed by FR-023), not a framework; no infrastructure ahead of a consumer. |
| VII. Auditability | **PASS** — this is the **first sensitive-resource feature**, so it births the minimal audit mechanism (ADR below); FR-023 is its sole initial consumer. |

**ADRs to write** (governance gate — `docs/adr/`):

- **ADR — Audit event mechanism**: append-only, immutable, scoped minimally to FR-023's need. Resolves the open item in `docs/adr/open-decisions.md`.
- **ADR — Session strategy**: opaque, server-stored, hashed, revocable tokens (not JWT), to support per-device sign-out (FR-026/031) and admin lockout actions.

## Project Structure

### Documentation (this feature)

```text
specs/001-auth-signup-signin/
├── spec.md               # Feature spec (clarified)
├── plan.md               # This file
├── research.md           # Phase 0 decisions
├── data-model.md         # Phase 1 entities
├── quickstart.md         # Phase 1 validation guide
├── contracts/            # Phase 1 API contract
├── testing-strategy.md   # Full-system E2E strategy (plan input)
├── design/               # Mockups (English/LTR — need RTL pass)
├── checklists/           # Spec quality checklist
└── tasks.md              # /speckit-tasks output (not created here)
```

### Source Code (repositories)

```text

├── internal/feature/auth/
│   ├── domain/        # entities, typed errors, state rules (pure)
│   ├── ports/         # VerificationSender, IdentityVerifier, Clock, repositories (auth-owned)
│   ├── service/       # use cases: request/confirm verification, google exchange, sessions, admin ops
│   ├── postgres/      # sqlc repos + adapters
│   ├── whatsapp/      # adapter implementing VerificationSender (over platform/whatsapp)
│   ├── google/        # adapter implementing IdentityVerifier (verifies Google ID token)
│   ├── http/          # Huma handlers + routes; session/admin middleware
│   └── fakes/         # hand-written fakes for unit tests + E2E
├── internal/platform/ # existing: db, httpserver, observability, whatsapp stub
└── migrations/        # goose (first migrations authored here)

admin-dashboard/
├── src/features/auth/ # accounts list, account drawer + clear-lockout, analytics
├── src/api/           # orval-generated client (new)
└── e2e/               # Playwright (new)

mobile-app/
├── lib/features/auth/ # splash/launch routing, role select, sign up/in, OTP, google, sign out
│                      #   (Riverpod controllers, go_router routes)
├── lib/api/           # swagger_parser-generated client (new)
└── integration_test/  # iOS-simulator E2E (new)

# workspace root
docker-compose.e2e.yml + Makefile/script   # full-system orchestration (testing-strategy Phase 4)
```

**Structure Decision**: The three existing repos each gain an `auth` vertical slice — hexagonal in the backend (domain/ports/service/adapters/http), a feature folder in admin and mobile. Client code is **generated, not hand-written** (Constitution V). Cross-repo E2E orchestration lives at the workspace root.

## Complexity Tracking

*No unjustified violations.* The one real complexity — three repos for a single feature — is inherent to the product, not a plan choice; feature isolation and contract-first are the constitution's prescribed controls for exactly that. The **admin dashboard is English-only for v0** (confirmed by the user 2026-07-20) and is deliberately kept out of the data model so it stays cheap to flip to bilingual later.

## Post-Design Constitution Re-check

After Phase 1 design (data-model, contracts, quickstart), all gates still hold: the contract surface introduces no cross-feature imports; every timed/stateful rule is expressed behind the auth-owned Clock and repository ports (testable with fakes); the audit table remains minimal; and no endpoint pushes an authz decision into a handler. No new violations; no Complexity Tracking entries required.
