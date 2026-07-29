# Full-System E2E Test Strategy: Auth: Sign Up & Sign In

**Feature**: [spec.md](./spec.md) · **Created**: 2026-07-20 · **Status**: Plan input (fold into `plan.md` via `/speckit-plan`)

## What this document is

The **step for validating the assembled Auth feature end-to-end** — a real user journey driven through the mobile app on an **iOS Simulator**, talking to a **Dockerized backend + Postgres**, with the **admin dashboard** exercised in a browser. It is written *before* implementation on purpose: its job is to propagate the **testability seams** the build must expose (see [Implementation constraints](#implementation-constraints-this-step-imposes)) into the plan, so the feature is built test-ready rather than retrofitted later.

It stays at the layer `/speckit-plan` will not invalidate — observable journeys, environment topology, faking strategy, harness sequencing, and tooling gaps. It deliberately does **not** specify endpoint shapes, request/response bodies, or the data model: those are the Huma-generated contract's and the plan's job (Constitution V, Contract-First).

> **Reality check.** The feature is currently spec-only — no auth code in backend, mobile, or admin. Nothing here runs today. The harness is **built layer-by-layer as each part lands** (see [Sequencing](#harness-sequencing)); no cross-repo infrastructure is created ahead of a consumer (Constitution VI).

## Where E2E sits in the test pyramid

This is the thin top layer, not a replacement for the layers below it (Constitution II, Test-First & Test Shape):

| Layer | Owner | Already present? |
|-------|-------|------------------|
| Unit — ports tested with hand-written fakes | each repo | patterns exist |
| Adapter integration — DB/provider adapters | backend (testcontainers + Postgres) | **yes**, wired in `go.mod` |
| Contract — generated clients vs Huma OpenAPI, staleness gate | all three repos | not yet (no contract authored) |
| **Full-system E2E — assembled system, real journeys** | **this doc** | **no — to build** |

E2E is expensive and slow; it validates *integration of the whole*, not logic that a lower layer already covers. Keep it to the journeys below, not exhaustive permutations.

## System topology

```
 iOS Simulator                 Docker network                  Browser
 ┌───────────────┐        ┌───────────────────────────┐     ┌───────────────┐
 │ Flutter app   │──HTTP─▶ │ backend (Huma/chi :8080)  │◀─HTTP─│ admin (Vite)  │
 │ integration_  │        │   │                        │     │ Playwright    │
 │ test          │        │   ▼                        │     └───────────────┘
 └───────────────┘        │ Postgres 18 (compose)      │
                          │ fake WhatsApp (OTP sink)   │
                          │ fake Google OAuth          │
                          └───────────────────────────┘
```

- **Backend + Postgres**: reuse the backend's existing `docker-compose.yml` (Postgres 18 healthcheck + API on :8080) and `make up`; the E2E lane adds the two fakes as extra compose services.
- **Mobile → backend networking**: the **iOS Simulator shares the host network**, so the app points at `http://localhost:8080` directly. There is **no `10.0.2.2` indirection** — that is Android-emulator-only. (A physical iOS device would instead need the host's LAN IP; the simulator does not.)
- **Admin → backend**: browser E2E hits the same `localhost:8080`.
- **External dependencies are faked, never live** (Constitution + spec Assumptions): real WhatsApp and real Google OAuth are non-deterministic, rate-limited, and cost money — E2E must not depend on them. See [Faking strategy](#faking-external-dependencies).

## End-to-end journeys to cover

Observable behaviors, traced to spec acceptance scenarios. Layer legend: **M** = mobile (iOS sim), **A** = admin (browser), **B** = backend (API-level).

| # | Journey | Traces | Layer |
|---|---------|--------|-------|
| J1 | Phone sign-up: role select → phone entry → WhatsApp OTP (read from fake sink) → account created only after correct code → session → role home | US1 | M→B |
| J2 | OTP failure paths: wrong code clears + retries, 5th wrong → 30-min lockout; expired code (>10 min) offers resend; resend disabled 60 s | US1 AS5/AS6, FR-007/008/009 | M→B |
| J3 | Send cap: exceeding ~5 sends / 30 min → same 30-min lockout | FR-029 | M→B |
| J4 | WhatsApp send *failure* (fake provider forced to error): "couldn't send" + immediate retry, no attempt consumed, no lockout | FR-028 | M→B |
| J5 | Returning sign-in by phone (fresh OTP) → session resumes on role home, no onboarding replay | US2 | M→B |
| J6 | Google sign-in, account exists → resumes with **no** OTP; Google sign-in, no account → routed into Google sign-up path | US2 AS4/AS5, FR-013/014 | M→B |
| J7 | Google sign-up (new): phone pre-filled when Google supplies one, manual entry when not; account created only after OTP | US3, FR-012 | M→B |
| J8 | Google failure vs cancel: forced error → message + retry; user cancel → silent return; phone entry stays available | FR-030 | M→B |
| J9 | App-launch routing: valid session → role home; no/expired session → role-select gate; session-check failure (backend down) → gate, not stuck on splash | US4, FR-018/019/020 | M→B |
| J10 | Session lifecycle: explicit **Sign out** returns to gate; second device stays signed in (multi-device, per-device sign-out) | FR-025/026/031 | M→B |
| J11 | Bilingual: Arabic (RTL) default, toggle to English (LTR), selection persists across relaunch | FR-027 | M |
| J12 | Admin account list: accounts created via phone and via Google appear with role / phone / Google-linked / method / date; filter by phone | US5, FR-021 | A→B |
| J13 | Admin clears a lockout (driven into lockout via J2) → user can immediately re-request; **audit event recorded** and observable; clearing a non-locked account is a no-op with no audit event | US6, FR-022/023 | A→B |
| J14 | Admin analytics: sign-ups by method, verification-failure rate, lockout count over a selectable range | US7, FR-024 | A→B |

Cross-cutting invariants asserted throughout: **no account exists until OTP verified** (SC-004), **no duplicate account per phone** (SC-003), **no password affordance anywhere** (FR-017).

## Implementation constraints this step imposes

These are **requirements on the build**, not test tricks. If they are not designed in, the journeys above cannot be automated later and someone retrofits them under deadline. Carry each into `/speckit-plan`.

1. **Injectable clock / compressible time.** J2, J3, J9, J10 depend on durations no automated test can wait out (10-min OTP expiry, 30-min lockout, 60-s resend cooldown, 30-min send-cap window, 30–90-day session idle-expiry). The backend must expose an **injectable clock** and/or **test-only short durations** so the harness can advance or compress time deterministically. *Without this seam, every expiry/lockout/cooldown/session path is untestable end-to-end.*
2. **OTP retrieval seam.** With WhatsApp faked, the fake provider must expose the **last code sent to a given number** to the harness (a sink/inbox), and the backend's WhatsApp port must be swappable to it by config. (The port already exists as a stub — good.)
3. **Controllable fake-OAuth identities.** Tests must choose the returned Google identity: email, whether a phone is present, and whether it maps to an existing account — plus forced error/cancel — to cover J6, J7, J8.
4. **Authorized-admin fixture.** Admin auth is out-of-scope-but-assumed (spec Assumptions). The build must provide a **test-only way to obtain an authorized-admin session** so J12–J14 run without depending on an unspecified admin-auth feature.
5. **Observable audit trail.** The FR-023 audit event (J13) must be queryable/observable so the harness can assert it (Constitution VII, Auditability).
6. **Deterministic data reset + seed.** Migrations + seed fixtures + a per-run DB reset so runs are independent and idempotent.
7. **Configurable backend base URL** in the mobile and admin clients, so the simulator/browser can target `localhost:8080` (E2E) vs a deployed environment.

## Faking external dependencies

- **WhatsApp**: a fake provider service (compose service) that records outbound codes and serves them to the harness; forced-failure mode for J4. Selected via backend config; never contacts real WhatsApp.
- **Google OAuth**: a fake/stub identity endpoint the app is pointed at in E2E builds, returning harness-controlled identities and error/cancel outcomes (constraint 3).
- **Admin auth**: a test fixture minting an authorized-admin session (constraint 4) — this feature only specifies what an *already-authorized* admin can do.

## Harness sequencing

Each phase adds harness **only once its consumer exists** (Constitution VI). Contract-first throughout (Constitution V).

- **Phase 0 — Contract.** Author the auth API in Go/Huma; generate admin (orval) and mobile (swagger_parser) clients; wire the generated-code staleness gate in each repo.
- **Phase 1 — Backend + fakes (first runnable slice).** goose migrations; auth service/adapters; WhatsApp + OAuth fakes; injectable clock; admin-auth test fixture; seed fixtures. Adapter integration tests via the existing testcontainers setup. **API-level E2E (B) journeys become runnable here** against the Dockerized backend.
- **Phase 2 — Admin.** Auth pages on the generated client; add **Playwright**; J12–J14 browser E2E against the Dockerized backend.
- **Phase 3 — Mobile.** Auth screens + `go_router` wiring on the generated client; add a Flutter **`integration_test/`** harness; J1–J11 run on the **iOS Simulator** against the Dockerized backend.
- **Phase 4 — Full-system orchestration.** Top-level compose (or overlay on the backend's) + a `make e2e` / script that: brings up backend + Postgres + fakes → runs migrations + seed → runs admin Playwright → runs mobile `integration_test` on a booted simulator → tears down.

## Tooling gaps to fill (by repo)

- **Backend**: fake WhatsApp (OTP sink) + fake Google OAuth services; injectable clock / test-time config; first goose migrations; seed fixtures; admin-auth test fixture.
- **Mobile**: `integration_test/` + the `integration_test` package; a build-config for the backend base URL; iOS-simulator boot/run in the E2E lane.
- **Admin**: Playwright (config + first specs); `orval` codegen wiring.
- **Workspace root**: top-level compose + orchestration script/Makefile; a home for the shared fakes; a dedicated **E2E CI workflow** (separate slow lane — the per-repo `ci.yml` unit runs stay fast, per Constitution II).

## Exit criteria

E2E is green when the spec's acceptance scenarios (US1–7 + the recorded clarifications) pass against the **assembled** system on the target environment — Dockerized backend + iOS Simulator + browser admin — with WhatsApp/OAuth faked and time controlled.

## Deferred to `/speckit-plan`

The auth API contract shape and data model; exact choice between an injectable clock vs. short test-window durations; whether iOS-simulator E2E runs in CI or stays local-only initially; confirmation of Playwright as the admin browser-E2E tool; and how the authorized-admin fixture is provided.
