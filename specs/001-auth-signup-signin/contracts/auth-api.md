# Phase 1 API Contract: Auth: Sign Up & Sign In

**Feature**: [spec.md](../spec.md) · **Plan**: [plan.md](../plan.md) · **Date**: 2026-07-20

**Source of truth**: the backend expresses these in Go types via **Huma**, which generates the OpenAPI 3.1 document at build time; admin (orval) and mobile (swagger_parser) generate clients from it (Constitution V). This file is the **design intent** those Huma types must realize and the staleness gate keeps in sync — it is not itself the generated schema. Field lists are indicative, not exhaustive.

**Conventions**: JSON over HTTPS; errors use Huma's RFC7807 problem format with a stable typed `code`; timestamps RFC3339; phone always normalized to `+20`. All endpoints are unauthenticated except where **Auth: session** or **Auth: admin** is noted.

## Consumer auth

### `POST /auth/verifications`
Request a WhatsApp code. Enforces the duplicate check (signup, FR-005), the no-account check (signin, FR-015), the send-cap (FR-029) and resend cooldown (FR-009) — before any code is sent.
- **Body**: `intent` (`signup`|`signin`), `phone`; for signup also `role`, `language`; optional Google context when initiated from the Google path.
- **201**: `{ verification_id, expires_at, resend_available_at }`
- **Errors**: `account_exists` (signup — before any send, FR-005), `no_account` (signin, FR-015), `locked` (+ `locked_until`, FR-008/029), `send_failed` (FR-028 — retry allowed, no attempt consumed), `invalid_phone` (FR-003/004).

### `POST /auth/verifications/{id}/resend`
Resend the code for an existing challenge. Subject to the 60-s cooldown (FR-009) and the send-cap (FR-029).
- **200**: `{ resend_available_at }` · **Errors**: `cooldown_active`, `locked`, `send_failed`, `expired`.

### `POST /auth/verifications/{id}/confirm`
Submit the 6 digits. On success creates the account (signup, FR-010) or resumes (signin) and issues a session.
- **Body**: `{ code }`
- **200**: `{ session_token, account: { id, role, language }, route: "onboarding" | "home" }` (FR-016)
- **Errors**: `incorrect_code` (+ `attempts_remaining`; 5th → `locked`, FR-008), `expired` (FR-007), `locked`.

### `POST /auth/google`
Exchange a Google ID token. Branches per FR-013/014 and US3.
- **Body**: `{ id_token, intent, role? }`
- **200 (existing account)**: `{ session_token, account, route }` — no OTP (FR-013).
- **200 (no account)**: `{ next: "verify_phone", phone_prefill? }` — routes into Google signup (FR-014/US3); phone pre-filled when Google supplies one (FR-012), else the client collects it and calls `/auth/verifications`.
- **Errors**: `google_failed` (FR-030 — provider error; a client-side *cancel* is silent and issues no request).

### `GET /auth/session`  *(Auth: session)*
Validate the current session at launch (US4).
- **200**: `{ account: { id, role, language } }` → route home (FR-019). **401**: no/expired/revoked session → entry gate (FR-020).

### `DELETE /auth/session`  *(Auth: session)*
Sign out the **current device only** (FR-026/031). **204**. Other devices' sessions are unaffected.

## Admin  *(Auth: admin — coarse `is-admin` in middleware; admin identity assumed provided externally)*

### `GET /admin/accounts`
List/search accounts (FR-021, US5).
- **Query**: `q`/`phone` filter, pagination.
- **200**: `[{ id, role, phone, google_linked, signup_method, created_at, lockout_active }]`

### `POST /admin/accounts/{id}/clear-lockout`
Clear an active verification lockout (FR-022, US6); emits an audit event (FR-023).
- **200 (was locked)**: `{ cleared: true }` + audit event recorded.
- **200 (not locked)**: `{ cleared: false }` — no-op, **no** audit event (edge case).

### `GET /admin/analytics`
Aggregate metrics over a range (FR-024, US7).
- **Query**: `range` (e.g. `24h`|`7d`|`30d`).
- **200**: `{ signups_by_method, verification_failure_rate, lockout_count }`

## Contract notes

- No password or password-recovery endpoint exists anywhere (FR-017).
- Egypt-only: the server rejects non-`+20` numbers (FR-003).
- Exact field names and error-code strings are finalized in the Huma Go types during implementation; this document is the surface those types must realize, and the per-repo staleness gate keeps the generated admin/mobile clients in sync.
