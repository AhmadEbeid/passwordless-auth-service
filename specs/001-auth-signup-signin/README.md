# 001 — Auth: Sign Up & Sign In

The specification this service was built from, as written before the code.

## What is here

| Document | What it holds |
|---|---|
| [spec.md](./spec.md) | User stories US1–US7 and functional requirements FR-001…FR-031 |
| [data-model.md](./data-model.md) | Entities, fields, and their lifecycle rules |
| [research.md](./research.md) | Decisions D1–D8 taken before implementation, with rationale |
| [contracts/auth-api.md](./contracts/auth-api.md) | The HTTP contract: endpoints, error codes, auth modes |
| [plan.md](./plan.md) | Implementation plan and structure decision |
| [tasks.md](./tasks.md) | Task breakdown and the running record of what landed |
| [testing-strategy.md](./testing-strategy.md) | Test shape per layer and exit criteria |
| [checklists/requirements.md](./checklists/requirements.md) | Quality gate applied to the spec itself |

## Read this first: the spec is wider than this repository

The feature spanned three codebases — this Go backend, a Flutter mobile client,
and a React admin dashboard. **Only the backend is in this repository.**

The documents are kept as written rather than trimmed to the backend, because
the requirements they drop explain decisions here that would otherwise look
arbitrary. Some examples:

- **FR-018 through FR-020** are mobile launch-routing requirements. They are why
  `GET /auth/session` exists and why confirm returns a `route` of `onboarding`
  or `home` rather than leaving the client to work it out.
- **FR-027** requires Arabic and English throughout, which is why an account
  carries `preferred_language` even though nothing in this repository renders a
  screen.
- **US5–US7** are admin-dashboard stories. They are why `/admin/*` exists at all
  and why ADR-0009 settles for a placeholder gate rather than a real admin
  identity model.

So: requirements naming the mobile app or the admin dashboard are context, not
gaps. What this repository owns is the backend surface —
[contracts/auth-api.md](./contracts/auth-api.md) is the accurate boundary.

`tasks.md` likewise records work across all three repositories, including client
tasks that were completed elsewhere and items deliberately deferred. It is the
honest execution log, not a description of this repository's contents.

## Traceability

Requirement identifiers used here resolve to real behaviour:

- FR-005 duplicate-phone block before sending → `Service.RequestVerification`
- FR-008 five wrong codes, thirty-minute lockout → `Verification.RegisterFailure`
- FR-028 a failed send costs no attempt → the send-before-persist ordering in
  `RequestVerification` and `ResendVerification`
- FR-029 rolling send cap → `CountSendsSince` plus `SendCapReached`
- D2 OTP hashed with bcrypt, D1 sessions opaque and hashed at rest → the two
  different hash choices in `service.go`

Where the implementation diverged from the plan, the ADRs record why —
[ADR-0009](../../docs/adr/0009-admin-authorization-placeholder.md) for the admin
gate and [ADR-0010](../../docs/adr/0010-sql-in-embedded-files.md) for dropping
sqlc in favour of embedded `.sql` files.
