# Tasks: Auth: Sign Up & Sign In

**Input**: Design documents in `/specs/001-auth-signup-signin/`
**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/auth-api.md](./contracts/auth-api.md), [quickstart.md](./quickstart.md), [testing-strategy.md](./testing-strategy.md)

**Tests**: **INCLUDED.** Constitution II (Test-First & Test Shape) makes tests a hard governance requirement for this repo, and `testing-strategy.md` defines the full-system E2E — so test tasks are explicitly requested. Port behavior is covered by hand-written fakes; adapters by testcontainers integration tests; system behavior by E2E. Tests are written **before** their implementation within each story.

**Organization**: Tasks are grouped by user story (from spec.md priorities) so each story is independently implementable and testable. This is a **multi-repo** feature — one `auth` vertical slice per surface.

## Format: `[ID] [P?] [Story] Description with file path`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: `[US1]`…`[US7]` — user-story phases only (Setup / Foundational / Polish carry no story label)

## Path Conventions (per plan.md Structure Decision)

- **Backend** (Go, hexagonal): **flat feature slice at `internal/auth/`** — `domain.go`, `ports.go`, `service.go`, `handler.go` at the feature root + a `postgres/` subpackage (ADR-0001 house style; *not* `internal/feature/auth/` with per-concern subdirs). Plus `migrations/`, `docs/adr/`. External identity/audit adapters are added as flat files (e.g. `google.go`) or platform packages when their consumer story lands.
- **Admin** (React/Vite): `admin-dashboard/src/features/auth/`, `admin-dashboard/src/api/` (orval, generated), `admin-dashboard/e2e/` (Playwright)
- **Mobile** (Flutter): `mobile-app/lib/features/auth/`, `mobile-app/lib/api/` (swagger_parser, generated), `mobile-app/integration_test/`
- **Workspace root**: `docker-compose.e2e.yml`, `Makefile`

> Client code in `src/api/` and `lib/api/` is **generated, not hand-written** (Constitution V). The Huma-generated OpenAPI is the single source of truth.

---

## Implementation Progress — 2026-07-21 (session: backend US1 cone)

Scoped to **US1's dependency cone** (Principle VI: no infra ahead of a consumer). Backend landed **green**: `go build ./...` ✅, `make test -race -cover` ✅ (auth pkg 53.5%), `golangci-lint run` ✅ (0 issues). Integration test **compiles** (`go vet -tags=integration` ✅); its **testcontainers runtime run is still pending** (Docker was unavailable at the time) — run `go test -tags=integration ./internal/auth/...` once Docker is up.

**Done:** T010 (ADR-0007 Session strategy), T012 (domain + typed errors), T013 (Clock port + fakes), T015 (repo ports + **hand-written** Postgres adapters — sqlc deferred, noted in `open-decisions.md`), T016 (VerificationSender port + fake + log-only stub sender), T022 (phone normalizer), T023 (Huma registration seam — captured the previously-discarded `huma.API` in `serve.go`, established `auth.Register`), T031 (US1 service unit tests), T033/T034/T035 (RequestVerification / ConfirmVerification / ResendVerification), T036 (3 Huma endpoints).

**Partial:** T014 (migrations for user_account/verification/session — `audit_event` deferred with the audit mechanism), T021 (session **issued** on confirm; validate/revoke behavior deferred to US4/US2), T027 (auth integration test written; shared harness minimal), T032 (US1 integration test compiles; runtime pending Docker).

**Deferred to their consumer story (not built):** T011 + T020 audit ADR/table/`EmitAuditEvent` (US6), T017 IdentityVerifier + T019 Google adapter (US2/US3), T018 real Meta WhatsApp client (stub only for now), T024 authn/`is-admin` middleware (US4/US5), T025/T026 orval + swagger_parser pipelines and T037 client regen (need the exported `openapi.yaml`), all mobile T029/T030/T038–T043.

**Note:** OTP stored via **bcrypt** (embeds its own salt — no salt column). Per-task file paths below still say `internal/feature/auth/...`/`service/`/`http/`; the real code is the **flat** `internal/auth/` layout per the corrected Path Conventions above.

---

## Implementation Progress — 2026-07-23 (session: backend fully — US2–US7 + Foundational spine)

Continued from the US1 cone above to finish the entire backend surface (US2–US7), finalising the backend ahead of the clients. Every chunk landed **green** and was committed separately: `go build ./...` ✅, `go vet ./...` ✅ (incl. `-tags=integration`), `golangci-lint run` ✅ (0 issues throughout), full `go test ./... -race -count=1` ✅, and — with Docker available — the testcontainers integration suite actually **runs and passes** (`go test -tags=integration ./internal/auth/postgres/...`), closing T032's long-pending runtime gap.

**Done:** T011 (ADR-0008 audit event mechanism), T014 (audit_event table + verification pending-Google columns, additive migration), T017 (IdentityVerifier port + fake), T019 (real Google ID-token adapter — JWKS-based, structured to Google's contract but unverified against a live token: no real `GOOGLE_CLIENT_ID`/credentials with Docker available), T020 (audit mechanism: repo + `emitAuditEvent`, first wired by US6), T021 (session validate/revoke with rolling idle-expiry), T024 (`platform/auth`: consumer `SessionMiddleware` backed by the auth feature's `ValidateSession` as a consumer-owned port, plus a coarse `AdminMiddleware` — see ADR-0009 for why admin auth is a configured shared secret, not a new admin-user system), T027 (testcontainers harness extended and actually exercised), T032 (integration suite runs and passes), T044–T048 (US2 backend), T054–T056 (US3 backend), T060–T062 (US4 backend, plus new HTTP-level tests for the session endpoints), T065–T067 (US5), T071–T073 (US6), T076–T078 (US7), T085 (security hardening pass — audited, not rewritten: OTP-hashed-at-rest, no-password-affordance, and non-+20-rejection all already held; confirmed via direct code inspection, not just decree).

**Also fixed, not just built:** a critical vulnerability caught by automated security review during US3 — the first draft accepted the Google identity to link directly from the client request body, which would have let any caller link a new account to a Google identity they don't own. Fixed with a server-signed, short-lived token `GoogleExchange` mints and `RequestVerification` verifies; covered by tests proving a forged or wrongly-signed token is rejected before anything is sent or persisted. Separately, building US5/US6 surfaced two real pre-existing bugs in the US1 lockout logic (both fixed, both now covered by tests): `RequestVerification` never checked the phone's latest challenge for an active lock, so a locked-out phone could bypass its lockout via a fresh request instead of a resend; and tripping the send-cap from `RequestVerification` (as opposed to `ResendVerification`) returned a locked error without persisting the lock on any row, so it wasn't later visible to admins (`lockout_active`) or clearable.

**Deferred/still open:** T018's real send has not been exercised live (same "structured, unverified, deferred to ops" status as the Google adapter — no real Meta WhatsApp Cloud API credentials exist here); T025/T026/T037/T048/T056/T062/T067/T073/T078 (client regeneration + staleness gates) need the admin/mobile repos, out of this backend-only pass; T081–T084/T086–T087 (full-system E2E orchestration, the Arabic/RTL mockup pass, quickstart validation, performance validation) all need mobile and/or admin and are explicitly cross-repo — not attempted.

---

## Implementation Progress — 2026-07-25 (session: mobile US1–US4 complete, integration coverage, checklist sync)

Picked up the mobile side after the backend's full finalization above. This entry also **retroactively syncs the checklist** for mobile work built earlier (US1's screens/scaffold) that was never checked off — the code existed and was tested, but the boxes were stale. Every chunk verified `flutter analyze` clean (0 issues), `dart format` clean, `flutter test` (widget suite) green, and the relevant `integration_test/` journeys passing live on the iOS Simulator.

**Done:** T003/T005/T007 (mobile scaffold, swagger_parser, `integration_test` — from the earlier US1 pass, now marked), T029/T030 (go_router entry gate + Riverpod shell; Arabic-default bilingual/RTL — also from that pass), T037/T038–T043 (US1 screens + client regen, previously built, now marked), T048–T053 (US2: Sign In screen, Google existing-account resume + no-account routing into Google sign-up, "no account" message/link, sign-out control, and journeys J5/J10), T056–T059 (US3: Google-first signup with phone pre-fill and account creation gated on OTP, genuine-error-vs-cancel handling, journeys J6/J7/J8), T062–T064 (US4: real `GET /auth/session` check on launch replacing the old "any local token = authenticated" placeholder, fail-open only on network/5xx and fail-closed on 401/403, journeys J9 across valid/expired/no-session/network-down). A full design pass (named color tokens, Cairo typeface for both scripts, a signature role-select moment) was also applied across the app, verified visually on the simulator.

**Also fixed, not just built:** the sign-in screen mutated Riverpod provider state synchronously from its own `initState`, which Riverpod rejects as "modifying a provider while the widget tree was building" (only surfaced once an integration test actually drove that screen) — fixed by deferring to a post-frame callback. Separately, tracing the sign-in journey found there was no way to reach Sign In directly from a cold launch — only via the buried "account already exists" error deep in the signup flow — which contradicts the spec's framing of Sign In as a screen a returning user opens directly; added a direct "Log in" link on role-select.

**Deferred/still open:** T028 admin-side base-URL config (mobile's half was already done); T025/T026 CI staleness gates for the generated clients (regeneration itself happens correctly, just not gated in CI); the entire admin frontend (T002/T004/T006, US5–US7 UI: T068–T070/T074–T075/T079–T080) — `admin-dashboard` is presently a bare React/Vite/TanStack scaffold with no auth feature, orval client, or Playwright harness, and building it belongs in its own change; T008 feature-isolation lint enforcement for mobile; T081–T084/T086–T087 (full docker-compose E2E orchestration, the Arabic/RTL design-mockup HTML pass, quickstart validation, performance validation) — all cross-repo/ops items, not attempted.

---

## Implementation Progress — 2026-07-26 (session: mobile design bugfixes, admin dashboard US5–US7, backend CORS)

Continued directly from the 2026-07-25 mobile session in the same conversation. Three parts: closing out three live-testing bugs the user found on the mobile app, then building the entire admin frontend (previously deferred as its own session), then a backend fix the admin work exposed.

**Done — mobile bugfixes (`mobile-app`):** a locale-switch crash (Arabic ↔ English produced a red error screen — `TextTheme.lerp` refuses to interpolate `TextStyle`s with different `inherit` flags, and Cairo's default text theme and the hand-built English one disagreed; fixed by building both from the same base `TextTheme`); the back button's fixed-content sizing and physical (non-directional) padding, now a `SizedBox` with `EdgeInsetsDirectional`; every route transition sliding in from the same edge regardless of navigation direction (all screens use `context.go`, which has no real back-stack for GoRouter's default transition to key off), replaced with a direction-neutral fade. A fourth bug surfaced live during simulator verification: the OTP input boxes rendered as a distorted double-outline shape — the custom `DecoratedBox` border was fighting the theme's own `InputDecorationTheme` fill/border still active on the `TextField` beneath it; fixed with `isCollapsed: true` and explicit `InputBorder.none` on every border variant.

**Done — admin dashboard (`admin-dashboard`, T002/T004/T006/T028/T067–T070/T073–T075/T078–T080):** the entire admin frontend, built from the bare scaffold. Orval generates a client scoped to the `admin` OpenAPI tag only (`input.filters`, not `output.filters` — the latter silently does nothing); the fetch mutator matches orval's default `{data, status, headers}` discriminated envelope (an `includeHttpResponseReturnType: false` + `forceSuccessResponse: true` override was tried first but hit a real orval bug generating unresolvable `*Success` type references on schemas with a nullable array or a readonly field — reverted). Full dark/light design system ported from the vendored `admin-auth-dashboard.html` mockup as CSS custom properties (real `oklch()`, no hex conversion needed on the web), Clash Display/General Sans bundled as `@font-face`. No login flow (ADR-0009: one configured shared secret) — a minimal key-entry gate persists it to `localStorage`; a 401 from any query clears it centrally via `queryClient`'s `onError`. Accounts list (search + role/locked filter chips + detail drawer + clear-lockout with toast), analytics (stat grid + range selector + split bar) — all verified live against the real backend and real Postgres in both themes in a real browser, not just unit-tested. Playwright covers all three (8 specs, `workers: 1` since specs share one fixed test account rather than seeding fresh ones per test — see `e2e/README.md` for why: the stub WhatsApp sender only logs the OTP for a human, no programmatic read seam exists, so locking a phone for the lockout tests works [wrong codes need no real one] but creating a fresh confirmed account for other tests doesn't).

**Also fixed, not just built:** the backend had zero CORS support (`internal/platform/httpserver/server.go`'s own comment: "CORS ... added here as needed") — harmless for mobile's native client, but it silently blocked every admin request the moment the dashboard was tested in an actual browser. Added a `go-chi/cors`-backed middleware, gated behind a new `ADMIN_ALLOWED_ORIGINS` config var (comma-separated, empty by default — fails closed like `ADMIN_API_KEY`), covered by four new tests (`cors_test.go`: no-origins-configured, allowed origin, disallowed origin, preflight).

**Deferred/still open:** T025/T026 CI staleness gates (admin's client regenerates correctly but isn't gated); T008 feature-isolation lint enforcement (admin/mobile); the e2e OTP-read gap above (would need a test-only debug endpoint gated to non-production, deliberately not built — see `e2e/README.md`); T081–T084/T086–T087 (full docker-compose E2E orchestration, the Arabic/RTL design-mockup HTML pass, quickstart validation, performance validation).

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Scaffold the three vertical slices and stand up the toolchains the rest of the feature depends on.

- [ ] T001 [P] Scaffold backend auth slice package stubs in `internal/feature/auth/` (`domain/ ports/ service/ postgres/ whatsapp/ google/ http/ fakes/`)
- [x] T002 [P] Scaffold admin auth feature folder `admin-dashboard/src/features/auth/` and create `admin-dashboard/src/api/` + `admin-dashboard/e2e/`
- [x] T003 [P] Scaffold mobile auth feature folder `mobile-app/lib/features/auth/` and create `mobile-app/lib/api/` + `mobile-app/integration_test/`
- [x] T004 [P] Add orval dev dependency + `admin-dashboard/orval.config.ts` (input: backend OpenAPI; output: `src/api/`)
- [x] T005 [P] Add swagger_parser dev dependency + config in `mobile-app/` (output generated client to `lib/api/`)
- [x] T006 [P] Add Playwright to `admin-dashboard/` (`playwright.config.ts` with `baseURL` from env, `e2e/` dir) and a `test:e2e` npm script
- [x] T007 [P] Enable Flutter `integration_test` in `mobile-app/` (dev_dependency + `integration_test/` harness reading `API_BASE_URL` via `--dart-define`)
- [ ] T008 [P] Configure feature-isolation boundary enforcement — depguard (``), eslint-boundaries (`admin-dashboard/`), custom_lint (`mobile-app/`) forbidding cross-feature internal imports (Constitution I)
- [ ] T009 Create workspace-root `docker-compose.e2e.yml` + `Makefile` `e2e` target skeleton for full-system orchestration (testing-strategy Phase 4)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The auth spine every user story depends on — domain, persistence, ports/adapters/fakes, the audit mechanism this feature *births*, the Huma+middleware HTTP spine, the generated-client pipeline, test seams, and the mobile app shell.

**⚠️ CRITICAL**: No user-story work begins until this phase is complete.

### Governance gate (ADRs — write before the code that realizes them)

- [x] T010 Write ADR "Session strategy" (opaque, server-stored, hashed, revocable — not JWT) in `docs/adr/` (research D1)
- [x] T011 Write ADR "Audit event mechanism" (append-only, immutable, minimal; FR-023 sole initial consumer) in `docs/adr/`, resolving `docs/adr/open-decisions.md` (research D6, Constitution VII)

### Backend domain, persistence, ports

- [x] T012 [P] Define auth domain entities + typed errors (`account_exists`, `no_account`, `locked`, `send_failed`, `incorrect_code`, `expired`, `google_failed`, `invalid_phone`) in `internal/feature/auth/domain/` (data-model.md)
- [x] T013 [P] Define `Clock` port + real adapter + fake in `internal/feature/auth/ports/` and `fakes/` (research D3 — the #1 test seam)
- [x] T014 Author goose migrations for `user_account`, `verification`, `session`, `audit_event` in `migrations/` (data-model.md; `audit_event` append-only, phone unique)
- [x] T015 Define repository ports (Accounts, Verifications, Sessions, AuditEvents) in `ports/` and implement sqlc-backed Postgres adapters in `postgres/`
- [x] T016 [P] Define `VerificationSender` port + hand-written fake that exposes the last-sent code (OTP retrieval sink) in `ports/` + `fakes/` (research D5; testing-strategy seam 2)
- [x] T017 [P] Define `IdentityVerifier` port + hand-written fake with controllable Google identities in `ports/` + `fakes/` (research D5; testing-strategy seam 3)
- [x] T018 Implement WhatsApp adapter (`VerificationSender` over the platform WhatsApp / Meta Cloud API) in `internal/feature/auth/whatsapp/` (research D5)
- [x] T019 Implement Google adapter (server-side ID-token verification → identity) in `internal/feature/auth/google/` (research D5)
- [x] T020 Implement minimal audit mechanism — append-only `audit_event` repo + single `EmitAuditEvent` service call in `service/` (research D6, Constitution VII)
- [x] T021 Implement session core — hashed opaque token, issue/validate/revoke, 60-day rolling idle-expiry — in `service/` + `postgres/` (research D1/D4; FR-025/031)
- [x] T022 [P] Add `+20`-only phone normalization/validation helper (strip leading 0 / pasted `+20`, require 10 digits, reject non-Egyptian) in `domain/` (FR-003/004)

### Backend HTTP spine + middleware (Authorization Boundary — Constitution III)

- [x] T023 Stand up Huma app + RFC7807 typed-error mapping + route-registration skeleton in `internal/feature/auth/http/` (Constitution V; contracts/auth-api.md conventions)
- [x] T024 Implement session-validation middleware (authn) + coarse `is-admin` middleware (authz) in `http/`; handlers make no authz decisions (Constitution III)

### Contract-first client pipeline (Constitution V)

- [ ] T025 [P] Wire orval generation + CI staleness gate (regenerate → `git diff --exit-code`) in `admin-dashboard/` (research D10)
- [ ] T026 [P] Wire swagger_parser generation + CI staleness gate in `mobile-app/` (research D10)

### Test & E2E seams

- [x] T027 [P] Backend integration-test harness — testcontainers-Postgres + DB reset/seed helpers + audit-trail assertion helper in `internal/feature/auth/postgres/` test support (testing-strategy seams 5–6)
- [x] T028 [P] Configurable backend base URL — admin Playwright `baseURL` from env + mobile `API_BASE_URL` `--dart-define` defaulting to `http://localhost:8080` (iOS Simulator uses `localhost`, **not** `10.0.2.2`) (testing-strategy seam 7)

### Mobile foundation

- [x] T029 Mobile app shell — go_router launch/entry-gate/home skeleton + base Riverpod providers in `mobile-app/lib/features/auth/` (FR-018)
- [x] T030 Arabic/English localization + RTL/LTR scaffolding (Arabic default, persists across launches) in `mobile-app/` (FR-027)

**Checkpoint**: Foundation ready — the audit mechanism, session core, ports/fakes, contract pipeline, and mobile shell exist. User stories can now proceed (in parallel if staffed).

---

## Phase 3: User Story 1 - New user creates an account with a phone number (Priority: P1) 🎯 MVP

**Goal**: A fresh visitor selects a role, enters an Egyptian phone number, and confirms a WhatsApp OTP — the account exists **only** after the correct code is confirmed, and a session starts.

**Independent Test**: Walk a fresh device through role selection → phone entry → OTP; confirm an account exists only after the correct code is submitted (and 5 wrong codes lock the number for 30 min).

### Tests (write first — Constitution II)

- [x] T031 [P] [US1] Service unit tests with fakes in `internal/feature/auth/service/`: correct code creates account only on confirm (FR-010); 5 wrong → `locked` 30 min (FR-008); expiry via fake clock (FR-007); duplicate phone blocks **before** send (FR-005); send-cap → `locked` (FR-029); provider send-failure not counted, retry allowed (FR-028); resend 60-s cooldown (FR-009)
- [x] T032 [P] [US1] Testcontainers integration test in `internal/feature/auth/postgres/`: verification + account repos persist/lookup, phone uniqueness enforced (SC-003)

### Backend implementation

- [x] T033 [US1] Implement `RequestVerification` (signup intent: duplicate check, send-cap, cooldown, dispatch via `VerificationSender`) in `service/` (FR-005/006/009/029)
- [x] T034 [US1] Implement `ConfirmVerification` (validate code; attempts/lockout/expiry; create account on success; issue session) in `service/` (FR-007/008/010/016)
- [x] T035 [US1] Implement `ResendVerification` (60-s cooldown + send-cap) in `service/` (FR-009/029)
- [x] T036 [US1] Implement Huma handlers `POST /auth/verifications`, `.../{id}/resend`, `.../{id}/confirm` in `http/` (contracts/auth-api.md)
- [x] T037 [US1] Regenerate admin + mobile clients from updated OpenAPI (orval + swagger_parser); confirm staleness gate green

### Mobile implementation

- [x] T038 [P] [US1] Role-selection screen (Coach/Athlete, full-width, **no back**, carries role into signup) in `lib/features/auth/` (FR-001/002)
- [x] T039 [P] [US1] Phone-entry screen + normalization + "Create Account" action in `lib/features/auth/` (FR-003/004)
- [x] T040 [US1] OTP screen — 6-digit boxes, paste-fill, auto-submit, error/clear/retry, resend cooldown, expiry & lockout messaging, "Change number" return in `lib/features/auth/` (FR-006/007/008/009)
- [x] T041 [US1] Duplicate-phone inline message + "Sign in instead" link in `lib/features/auth/` (FR-005)
- [x] T042 [US1] Wire signup flow to generated client + Riverpod controllers in `lib/features/auth/`
- [x] T043 [US1] Mobile `integration_test` journey J1 (role → phone → OTP → home) reading the code from the fake WhatsApp sink, on the iOS Simulator, in `integration_test/`

**Checkpoint**: US1 fully functional and independently testable — this is the MVP.

---

## Phase 4: User Story 2 - Returning user signs back in (Priority: P1)

**Goal**: An existing user re-authenticates via a fresh WhatsApp code **or** a linked Google account and lands on their role's home screen; an explicit Sign out ends the current device's session.

**Independent Test**: Sign in with an already-registered phone or linked Google account; confirm the session resumes on the correct home screen with no onboarding replay; sign out and confirm return to the entry gate.

### Tests (write first)

- [x] T044 [P] [US2] Service unit tests in `service/`: signin no-account check (FR-015); Google existing-account resume with no OTP (FR-013); sign-out revokes only the current session, others survive (FR-026/031)

### Backend implementation

- [x] T045 [US2] Extend `RequestVerification`/`ConfirmVerification` for signin intent (no-account check; resume existing account's session) in `service/` (FR-015/016)
- [x] T046 [US2] Implement `GoogleExchange` existing-account branch (verify ID token → issue session, no OTP) in `service/` + `POST /auth/google` handler in `http/` (FR-013)
- [x] T047 [US2] Implement `DELETE /auth/session` sign-out (current device only) in `service/` + `http/` (FR-026/031)
- [x] T048 [US2] Regenerate clients; confirm staleness gate green

### Mobile implementation

- [x] T049 [P] [US2] Sign In screen mirroring Sign Up ("Send WhatsApp code" CTA, Google button, **no password / no "forgot password"** affordance) in `lib/features/auth/` (FR-011/017)
- [x] T050 [US2] Google sign-in wiring: existing account resumes; no matching account routes into the US3 Google-signup path (not an error) in `lib/features/auth/` (FR-013/014)
- [x] T051 [US2] "No account found — Sign up instead" message + link in `lib/features/auth/` (FR-015)
- [x] T052 [US2] Sign out control (ends current session → entry gate) in `lib/features/auth/` (FR-026)
- [x] T053 [US2] Mobile `integration_test` journeys: phone sign-in + Google-existing resume + sign-out, in `integration_test/`

**Checkpoint**: US1 and US2 both work independently — sign-up and returning sign-in are complete.

---

## Phase 5: User Story 3 - New user signs up via Google (Priority: P2)

**Goal**: A new user signs up via Google; a WhatsApp phone verification is still required before the account is created, with the phone pre-filled when Google supplies one.

**Independent Test**: Complete Google OAuth as a brand-new user; confirm the account is created only after a WhatsApp code is verified, phone pre-filled when Google supplies a number and blank otherwise.

### Tests (write first)

- [x] T054 [P] [US3] Service unit tests in `service/`: Google no-account → `verify_phone` (with prefill when present); pending-Google `confirm` creates the account with the Google identity linked; provider error vs. cancel handling (FR-030)

### Backend implementation

- [x] T055 [US3] Extend `GoogleExchange` no-account branch (`next: verify_phone`, `phone_prefill?`) and carry pending Google context (`pending_google_subject/email`) into the Verification so `confirm` links Google on account creation in `service/` (FR-012/014; data-model pending-signup context)
- [x] T056 [US3] Regenerate clients; confirm staleness gate green

### Mobile implementation

- [x] T057 [US3] Google-signup flow on Sign Up: OAuth → phone prefill/manual entry → reuse US1 OTP screen → account created with Google linked, in `lib/features/auth/` (FR-012, US3 scenarios)
- [x] T058 [US3] Google error ("couldn't complete Google sign-in" + retry) vs. silent cancel handling in `lib/features/auth/` (FR-030)
- [x] T059 [US3] Mobile `integration_test` journey: Google-first signup, in `integration_test/`

**Checkpoint**: All three account-entry paths (phone signup, returning sign-in, Google signup) work independently.

---

## Phase 6: User Story 4 - App launch routes returning and new users correctly (Priority: P2)

**Goal**: A brief branded launch screen checks for a valid session and routes to home (valid) or the role-selection entry gate (none/expired/network-fail), never blocking.

**Independent Test**: Launch with a valid session (→ home), with an expired/no session (→ role selection), and with the check unable to complete (→ entry gate, not stuck).

### Tests (write first)

- [x] T060 [P] [US4] `GET /auth/session` handler test in `http/`: valid → account; expired/revoked/missing → 401 (FR-019/020)

### Backend implementation

- [x] T061 [US4] Implement `GET /auth/session` validate handler (200 account / 401) in `http/` (FR-019/020)
- [x] T062 [US4] Regenerate clients; confirm staleness gate green

### Mobile implementation

- [x] T063 [US4] Splash/launch screen + session-check routing (valid → role home; none/expired/network-fail → entry gate; never blocks indefinitely) in `lib/features/auth/` (FR-018/019/020)
- [x] T064 [US4] Mobile `integration_test` journeys: launch with valid / expired / no session, in `integration_test/`

**Checkpoint**: The mobile app opens correctly for both returning and new users.

---

## Phase 7: User Story 5 - Admin views registered accounts (Priority: P2)

**Goal**: An authorized admin sees a searchable list of accounts (role, verified phone, Google-linked, sign-up method, creation date).

**Independent Test**: Seed accounts created via each sign-up path; confirm they all appear with correct role/verification/method detail and filter by phone.

> **Decision (research D9, confirmed 2026-07-20)**: admin UI is **English-only** for v0 (internal operational tool). FR-027 bilingual/RTL applies to the mobile app only.

### Tests (write first)

- [x] T065 [P] [US5] Backend test in `http/`: `GET /admin/accounts` lists + filters by phone; enforced admin-only via the `is-admin` middleware fixture (authorized-admin fixture — testing-strategy seam 4) (FR-021)

### Backend implementation

- [x] T066 [US5] Implement accounts list/search service + `GET /admin/accounts` handler (role, phone, Google-linked, sign-up method, created_at, lockout_active; phone filter + pagination) in `service/` + `http/` (FR-021)
- [x] T067 [US5] Regenerate admin client; confirm staleness gate green

### Admin implementation

- [x] T068 [P] [US5] Accounts list feature (table: role / phone / Google-linked / method / date; phone filter) in `admin-dashboard/src/features/auth/` (FR-021)
- [x] T069 [US5] Wire list to orval client + TanStack Query in `admin-dashboard/src/features/auth/`
- [x] T070 [US5] Playwright e2e: list shows all sign-up paths and filters by phone, in `admin-dashboard/e2e/`

**Checkpoint**: Admins can see and search accounts — the foundation for US6/US7.

---

## Phase 8: User Story 6 - Admin resolves a stuck sign-up or sign-in (Priority: P2)

**Goal**: An authorized admin clears an active verification lockout so the user can retry immediately; the action is audited; a no-op clear on an unlocked account writes no audit event.

**Independent Test**: Drive an account into the 5-attempt lockout, clear it as admin, confirm the user can immediately request a new code and an audit record was written; confirm a clear on an unlocked account is a no-op with no audit event.

**Depends on**: US5 (admin list UI) + Foundational audit mechanism (T020).

### Tests (write first)

- [x] T071 [P] [US6] Backend test in `service/`: clear-lockout re-enables the number and writes exactly **one** audit row; a no-op clear writes **none** (FR-022/023; observable audit trail — testing-strategy seam 5)

### Backend implementation

- [x] T072 [US6] Implement clear-lockout service (clear lockout; `EmitAuditEvent` on effect; no-op + no audit when not locked) + `POST /admin/accounts/{id}/clear-lockout` handler in `service/` + `http/` (FR-022/023)
- [x] T073 [US6] Regenerate admin client; confirm staleness gate green

### Admin implementation

- [x] T074 [US6] Account detail/drawer + "Clear lockout" action (success + "nothing to clear") in `admin-dashboard/src/features/auth/` (FR-022)
- [x] T075 [US6] Playwright e2e: drive account into lockout → clear → verify re-enabled and audit recorded, in `admin-dashboard/e2e/`

**Checkpoint**: Support can unblock locked-out users, with an audit trail.

---

## Phase 9: User Story 7 - Admin sees signup/auth analytics (Priority: P3)

**Goal**: An authorized admin views aggregate metrics (sign-ups by method, verification failure rate, lockout count) over a selectable range.

**Independent Test**: Generate a mix of successful/failed sign-up/sign-in attempts across both methods; confirm the analytics view reflects accurate totals and rates for the selected range.

### Tests (write first)

- [x] T076 [P] [US7] Backend test in `service/`: `GET /admin/analytics` returns `signups_by_method`, `verification_failure_rate`, `lockout_count` over `range` (24h/7d/30d) (FR-024)

### Backend implementation

- [x] T077 [US7] Implement analytics aggregation service + `GET /admin/analytics` handler (range-scoped) in `service/` + `http/` (FR-024)
- [x] T078 [US7] Regenerate admin client; confirm staleness gate green

### Admin implementation

- [x] T079 [US7] Analytics view + range selector in `admin-dashboard/src/features/auth/` (FR-024)
- [x] T080 [US7] Playwright e2e: analytics reflect the selected range, in `admin-dashboard/e2e/`

**Checkpoint**: All seven user stories are independently functional.

---

## Phase 10: Polish & Cross-Cutting Concerns (incl. Full-System E2E)

**Purpose**: Assemble the surfaces into the full-system E2E defined in `testing-strategy.md`, and close cross-cutting invariants.

- [ ] T081 Complete `docker-compose.e2e.yml` (backend + Postgres + fakes) and the `make e2e` orchestration: up → migrate + seed → admin Playwright → mobile `integration_test` on a booted simulator → teardown, at the workspace root (testing-strategy Phase 4)
- [ ] T082 [P] Assemble full-system E2E journeys J1–J14 (testing-strategy) against the assembled system; verify cross-cutting invariants: no account before OTP verified (SC-004), one account per phone (SC-003), no password affordance anywhere (FR-017)
- [ ] T083 [P] Wire the separate slow E2E CI lane — mobile simulator runs local-first (research D8); admin Playwright + dockerized backend in CI
- [ ] T084 [P] Arabic/RTL pass on `specs/001-auth-signup-signin/design/` mockups to match the design system's RTL kit (plan open item; FR-027)
- [x] T085 [P] Security hardening pass across `internal/feature/auth/`: OTP hashed at rest (research D2), no password affordance (FR-017), non-`+20` numbers rejected (FR-003)
- [ ] T086 Run `quickstart.md` layer-by-layer validation and record definition-of-done results
- [ ] T087 [P] Performance validation against SC-001 (signup < 2 min) and SC-002 (sign-in < 90 s)

---

## Dependencies & Execution Order

### Phase dependencies

- **Setup (Phase 1)** — no dependencies; start immediately.
- **Foundational (Phase 2)** — depends on Setup; **blocks all user stories**. ADRs (T010–T011) precede the session/audit code that realizes them.
- **User Stories (Phases 3–9)** — all depend on Foundational. Once it's done, P1 stories (US1, US2) come first; the rest can proceed in parallel if staffed, or in priority order.
- **Polish / Full-System E2E (Phase 10)** — depends on every user story whose journey it exercises; T081–T082 need all surfaces present.

### User-story dependencies

- **US1 (P1)** — independent after Foundational (the MVP).
- **US2 (P1)** — independent after Foundational; its Google no-account branch *routes into* US3 but is independently testable via the phone path.
- **US3 (P2)** — reuses the US1 OTP screen; testable on its own.
- **US4 (P2)** — independent after Foundational (session core).
- **US5 (P2)** — independent after Foundational (admin middleware).
- **US6 (P2)** — depends on **US5** (admin list UI) + Foundational audit (T020).
- **US7 (P3)** — independent after Foundational; benefits from US5's list surface.

### Within each user story

- Tests written **first** and failing before implementation (Constitution II).
- Backend: domain/ports (Foundational) → service → Huma handler → regenerate clients.
- Mobile/Admin: screens/features → wire to the **generated** client → integration/e2e journey.

### Parallel opportunities

- All Setup `[P]` tasks (T001–T008) run in parallel across the three repos.
- Foundational `[P]` tasks (T012, T013, T016, T017, T022, T025, T026, T027, T028) run in parallel; T014/T015/T018–T021/T023/T024 serialize on shared files.
- After Foundational, US1–US5 and US7 can be staffed in parallel; US6 waits on US5.
- Within a story, `[P]` tasks touch different files (e.g., role-select vs. phone-entry screens; backend unit vs. integration tests).

---

## Parallel Example: User Story 1

```bash
# Tests first, in parallel (different files):
Task: "T031 Service unit tests with fakes in internal/feature/auth/service/"
Task: "T032 Testcontainers integration test in internal/feature/auth/postgres/"

# Independent mobile screens, in parallel:
Task: "T038 Role-selection screen in lib/features/auth/"
Task: "T039 Phone-entry screen + normalization in lib/features/auth/"
```

---

## Implementation Strategy

### MVP first (User Story 1 only)

1. Phase 1 Setup → 2. Phase 2 Foundational (CRITICAL — births session/audit/contract pipeline) → 3. Phase 3 US1 → **STOP & VALIDATE** the phone-signup journey end-to-end on the iOS Simulator against the dockerized backend → demo.

### Incremental delivery

Setup + Foundational → US1 (MVP) → US2 (returning sign-in completes the P1 core) → US3/US4 (Google signup + launch routing) → US5 → US6 → US7 → Phase 10 full-system E2E. Each story is a testable increment.

### Parallel team strategy

After Foundational: backend + mobile pair on US1→US2→US3→US4; admin dev takes US5→US6→US7 against the generated client once the admin endpoints land. Reconvene for Phase 10 orchestration.

---

## Notes

- `[P]` = different files, no dependency on an incomplete task.
- `[Story]` labels map tasks to spec user stories for traceability; Setup/Foundational/Polish carry none.
- Every timed/stateful rule reads the injectable `Clock` (T013) so tests are deterministic.
- **Open item to confirm before Phase 7**: admin dashboard language (English-only assumption, research D9).
- Client code (`src/api/`, `lib/api/`) is regenerated after every contract change; the staleness gate (T025/T026) must stay green.
