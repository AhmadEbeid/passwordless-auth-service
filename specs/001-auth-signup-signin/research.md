# Phase 0 Research & Decisions: Auth: Sign Up & Sign In

**Feature**: [spec.md](./spec.md) · **Plan**: [plan.md](./plan.md) · **Date**: 2026-07-20

All open/deferred items from the spec clarifications and [testing-strategy.md](./testing-strategy.md) were resolvable from the constitution + spec + clarifications; no external/library research was required. Each decision is Decision / Rationale / Alternatives.

### D1 — Session strategy: opaque, server-stored, revocable tokens

- **Decision**: Sessions are opaque random tokens, stored server-side (hashed at rest), one row per device, validated on each request.
- **Rationale**: per-device sign-out (FR-026/031), admin-driven state changes, and idle-expiry (FR-025) all need server-side revocation and per-session state — trivial with server-stored sessions, awkward with stateless JWTs.
- **Alternatives**: JWT (rejected — revocation/rotation complexity for no v0 benefit); signed cookies (rejected — mobile is the primary client, not a browser).
- → ADR "Session strategy".

### D2 — OTP stored as a hash

- **Decision**: Persist a hash of the 6-digit code (with per-code salt), never plaintext; compare on confirm.
- **Rationale**: the code is a short-lived credential; a DB read must not reveal a live code. Cheap and standard.
- **Alternatives**: plaintext (rejected — credential exposure); provider-side-only verification (rejected — WhatsApp only delivers text; the app owns verification per FR-006/010).

### D3 — Injectable clock for all timed rules

- **Decision**: A `Clock` port injected into the auth service; all durations (10-min expiry, 30-min lockout, 60-s resend, 30-min send-cap window, 60-day session idle-expiry) are computed from it.
- **Rationale**: makes every timed FR deterministically testable (the #1 seam in testing-strategy.md); aligns with House Go Style (explicit deps).
- **Alternatives**: `time.Now()` inline (rejected — untestable); test-only short windows (rejected — divergent prod/test behavior).

### D4 — Concrete numeric thresholds

- **Decision**: Session idle-expiry = **60 days**; send-cap = **5 sends / 30 min** per number. (Spec-fixed: OTP expiry 10 min, lockout 5 attempts / 30 min, resend cooldown 60 s.)
- **Rationale**: 60 days is the midpoint of the spec's ~30–90-day range — long enough to avoid nagging re-auth, short enough for PII hygiene on abandoned devices; 5/30 matches the accepted clarification and reuses the lockout window.
- **Alternatives**: 30 or 90 days (either acceptable; 60 chosen as the balanced midpoint — a single tunable constant).

### D5 — External identity via auth-owned ports + adapters + fakes

- **Decision**: auth declares a `VerificationSender` port (send a code to a phone) and an `IdentityVerifier` port (verify a Google ID token → identity). Real adapters: WhatsApp = **Meta WhatsApp Cloud API** (the concrete provider sits behind the port and is deferrable to ops); Google = server-side ID-token verification. Fakes implement both for tests/E2E.
- **Rationale**: feature isolation (Constitution I) + the E2E faking seams (testing-strategy constraints 2–3). Mobile obtains a Google ID token via the native Google Sign-In SDK and hands it to the backend — no client secret on the device.
- **Alternatives**: calling providers directly from the service (rejected — couples to a vendor, unfakeable); backend authorization-code OAuth flow (rejected for v0 — native ID-token flow is simpler on mobile).

### D6 — Minimal audit mechanism

- **Decision**: A single append-only `audit_event` table + one `EmitAuditEvent` service call; FR-023 (admin lockout-clear) is the only initial emitter.
- **Rationale**: Constitution VII requires auditing to exist from the first sensitive-resource feature (this one); Constitution VI forbids over-building — so minimal, not a framework.
- **Alternatives**: full audit framework / event bus (rejected — no second consumer yet); logging-only (rejected — not immutable/queryable, fails FR-023's "audit record").
- → ADR "Audit event mechanism".

### D7 — Admin browser-E2E tool: Playwright

- **Decision**: Playwright for admin E2E.
- **Rationale**: first-class TypeScript support, fits Vite/React, reliable auto-waiting; no existing e2e tool to preserve.
- **Alternatives**: Cypress (viable; Playwright preferred for multi-context + speed).

### D8 — iOS-simulator E2E runs local-first

- **Decision**: Mobile `integration_test` on the iOS Simulator runs locally / on-demand first; wire into CI once stable.
- **Rationale**: simulator E2E in CI is slower and flakier to stand up; capture value locally now (Constitution VI — no infra ahead of need).
- **Alternatives**: CI-from-day-one (deferred, not rejected).

### D9 — Admin dashboard language: English-only (CONFIRMED)

- **Decision**: Admin UI is English-only for v0; the bilingual requirement (FR-027) is scoped to the consumer mobile app.
- **Rationale**: internal operational tool; the bilingual requirement is a consumer-UI concern.
- **Status**: **confirmed by the user 2026-07-20**. Kept out of the data model, so a later flip to bilingual stays additive.

### D10 — Client generation is greenfield

- **Decision**: Phase 0 stands up orval (admin) and swagger_parser (mobile) against the Huma OpenAPI, each with a CI staleness gate (regenerate → `git diff --exit-code`).
- **Rationale**: Constitution V; neither generator exists yet (Explore confirmed).
- **Alternatives**: hand-written clients (rejected — violates Constitution V).
