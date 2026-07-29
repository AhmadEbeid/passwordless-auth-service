# Quickstart & Validation Guide: Auth: Sign Up & Sign In

**Feature**: [spec.md](./spec.md) · **Plan**: [plan.md](./plan.md) · **Date**: 2026-07-20

How to validate the feature works end-to-end. Detailed journeys and the harness design live in [testing-strategy.md](./testing-strategy.md); the API surface in [contracts/auth-api.md](./contracts/auth-api.md); entities in [data-model.md](./data-model.md). This guide is the run/validation flow, **not** implementation.

> **Greenfield**: nothing here runs until the corresponding layer is implemented (see the plan's Structure Decision and the testing-strategy Sequencing). Validate each layer as it lands, ending with the full-system E2E. Command targets marked *(new)* don't exist yet — they're created in the phase that introduces them; the backend targets already exist.

## Prerequisites

- **Docker** (backend + Postgres + fakes)
- **Go** toolchain (backend tests)
- **Flutter + Xcode iOS Simulator** (mobile `integration_test`)
- **Node** (admin + Playwright)

## Layer-by-layer validation

### 1. Backend (API-level)

```bash
cd passwordless-auth-service
make up          # Postgres + API (+ fakes) on :8080
make migrate-up          # apply goose migrations
make test                # unit (port fakes) + testcontainers integration
```

**Expected**: health green; the endpoints in [contracts/auth-api.md](./contracts/auth-api.md) respond; integration tests pass against real Postgres. Verify directly (curl/httptest): request→confirm creates an account **only** on the correct code (FR-010); 5 wrong codes → `locked` (FR-008); expired code by advancing the injectable clock (FR-007); exceeding the send-cap → `locked` (FR-029); admin clear-lockout writes an audit row (FR-023) while a no-op clear writes none.

### 2. Admin (browser)

```bash
cd admin-dashboard
npm install && npm run test:e2e   # (new) Playwright against the dockerized backend
```

**Expected**: accounts list shows role / phone / Google-linked / method / date and filters by phone (US5); clear-lockout re-enables the user and records an audit event (US6); analytics reflect the selected range (US7).

### 3. Mobile (iOS Simulator)

```bash
cd mobile-app
open -a Simulator
flutter test integration_test --dart-define=API_BASE_URL=http://localhost:8080   # (new)
```

**Expected**: role-select → phone → OTP (read from the fake WhatsApp sink) → home (US1); returning sign-in (US2); Google paths (US2/US3); launch routing for valid/expired/no session (US4); per-device sign-out (FR-026/031); Arabic↔English toggle persists (FR-027). The **iOS Simulator reaches the backend over plain `localhost:8080`** — no `10.0.2.2` indirection.

### 4. Full-system E2E

```bash
# workspace root — once orchestration exists (testing-strategy Phase 4)
make e2e   # (new)
```

Brings up backend + Postgres + fakes → migrations + seed → admin Playwright → mobile `integration_test` on a booted simulator → teardown.

## Definition of done

E2E green = the spec's acceptance scenarios (US1–7 + the recorded clarifications) pass against the **assembled** system with WhatsApp/OAuth faked and time controlled (see testing-strategy Exit Criteria). Cross-cutting invariants hold: no account before OTP verified (SC-004), one account per phone (SC-003), no password affordance anywhere (FR-017).
