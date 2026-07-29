# Feature Specification: Auth: Sign Up & Sign In

**Feature Branch**: `001-auth-signup-signin`

**Created**: 2026-07-19

**Status**: Draft

**Input**: User description: "lets start working on this F-01 — Auth: Sign Up & Sign In"

**Source**: product epic with five child stories (tracker links removed).

## Clarifications

### Session 2026-07-20

- Q: How does a session end — by expiry, by an explicit "Sign out" action, or both? → A: Rolling session + explicit sign-out — a session stays valid while the app is in active use, auto-expires after a long idle period (~30–90 days of inactivity), and an explicit "Sign out" control ends it immediately, returning the user to the entry gate.
- Q: Which language(s) does the v0 user-facing UI ship in? → A: Bilingual — Arabic and English with a user-selectable toggle; Arabic renders right-to-left (RTL), English left-to-right (LTR), defaulting to Arabic for the Egypt-only launch.
- Q: When sending the WhatsApp verification code fails (provider/delivery error, not a wrong entry), what does the user experience? → A: A clear "couldn't send your code" error with an immediate retry; the failure does not count toward the 5-attempt lockout and does not lock the number.
- Q: Is there a ceiling on how many verification codes can be sent to one number, beyond the 60-second cooldown? → A: Yes — cap sends per rolling window (target 5 sends per 30 minutes per number); exceeding the cap triggers the same 30-minute lockout used for failed attempts.
- Q: What does the user experience when Google sign-in doesn't complete (provider error vs. user cancel)? → A: Distinguish the two — a genuine Google error shows a brief "couldn't complete Google sign-in" message and returns to the auth screen (phone entry available, free retry); a deliberate cancel returns silently with no error.
- Q: How many concurrent device sessions per account, and what is the scope of "Sign out"? → A: Multiple concurrent sessions (one per device); "Sign out" ends only the current device's session, and each session expires independently on its own inactivity timer. A global "sign out everywhere" is out of scope for v0.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - New user creates an account with a phone number (Priority: P1)

A first-time visitor picks whether they're a Coach or an Athlete, enters their Egyptian phone number, and confirms it via a WhatsApp verification code. Only once the code is confirmed does their account exist.

**Why this priority**: This is the only account-creation path that doesn't depend on a third-party identity provider, and it's the mechanism that guarantees every account has a working WhatsApp channel — the app's sole communication method between coaches and athletes. Without this, there is no way to create a usable account at all.

**Independent Test**: Can be fully tested by walking a fresh device through role selection → phone entry → OTP entry, and confirming an account exists only after the correct code is submitted.

**Acceptance Scenarios**:

1. **Given** a user has never signed up, **When** they open the app, **Then** they see two full-width role options ("I'm a Coach" / "I'm an Athlete") with no way to go back, and choosing one carries that role into sign-up.
2. **Given** a user has selected a role, **When** they enter a valid 10-digit Egyptian mobile number (with or without a leading 0 or pasted +20), **Then** the "Create Account" action becomes available and, on submit, a 6-digit code is sent to that number via WhatsApp.
3. **Given** a phone number is already registered, **When** a user tries to sign up with it, **Then** they see an inline message that an account already exists and are offered a link to sign in instead, before any code is sent.
4. **Given** an OTP has been sent, **When** the user enters the correct 6 digits (typed one at a time or pasted), **Then** the form auto-submits, the account is created, a session starts, and role-appropriate onboarding begins.
5. **Given** an OTP has been sent, **When** the user enters an incorrect code, **Then** the input clears with an error shown and they may retry, up to 5 attempts before the number is locked out for 30 minutes.
6. **Given** an OTP was sent more than 10 minutes ago, **When** the user tries to submit it, **Then** they see a message that the code expired and are offered a way to request a new one (also available after a 60-second resend cooldown).

---

### User Story 2 - Returning user signs back in (Priority: P1)

A user who already has an account opens the app and re-authenticates using either their verified phone number (via a fresh WhatsApp code) or their linked Google account, landing back on their role-appropriate home screen without repeating onboarding.

**Why this priority**: A one-time sign-up with no way to come back delivers no ongoing value — coaches and athletes need to return to the app repeatedly (manage packages, check requests, message). This is as essential to a working product as account creation itself.

**Independent Test**: Can be fully tested by signing in with an already-registered phone number or linked Google account and confirming the session resumes on the correct role's home screen with no onboarding replay.

**Acceptance Scenarios**:

1. **Given** a returning user opens the Sign In screen, **When** they see the layout, **Then** it matches Sign Up (country code + phone field, OR divider, Google button) but the call-to-action reads "Send WhatsApp code" and there is no password or "forgot password" affordance anywhere.
2. **Given** a registered phone number, **When** the user submits it and enters the correct WhatsApp code, **Then** their existing session resumes and they land on the home tab matching their role.
3. **Given** a phone number with no matching account, **When** the user tries to sign in with it, **Then** they see "No account found. Sign up instead." with a link to Sign Up.
4. **Given** a user taps "Sign in with Google" and that Google account already maps to a platform account, **When** the OAuth completes, **Then** their session resumes directly with no OTP step (their phone was already verified at signup).
5. **Given** a user taps "Sign in with Google" and no matching platform account exists, **When** the OAuth completes, **Then** they are routed into the new-user Google sign-up path (User Story 3) rather than shown an error.

---

### User Story 3 - New user signs up via Google (Priority: P2)

A first-time visitor signs up using their Google account instead of typing a phone number by hand. WhatsApp verification of a phone number is still required before the account is created, since Google only proves identity, not that the person has a working WhatsApp number.

**Why this priority**: This removes a friction point (manual phone entry) for users who prefer Google, but it is additive — phone-based sign-up (User Story 1) already provides a complete, working account-creation path without it.

**Independent Test**: Can be fully tested by completing Google OAuth as a brand-new user and confirming the account is only created after a WhatsApp code is verified, with the phone screen pre-filled when Google supplies a number and blank otherwise.

**Acceptance Scenarios**:

1. **Given** a new user taps "Sign in with Google" on the Sign Up screen, **When** Google OAuth succeeds, **Then** the app pre-fills the phone field from the Google profile if a number is available.
2. **Given** Google OAuth succeeds but supplies no phone number, **When** the user reaches the next step, **Then** they are shown a phone entry screen to provide one manually.
3. **Given** a phone number has been confirmed or entered after Google OAuth, **When** the user proceeds, **Then** the same WhatsApp OTP screen and rules from User Story 1 apply, and the account is not created until that code is verified.
4. **Given** the OTP is verified after a Google sign-up, **When** verification succeeds, **Then** the account is created, a session starts, and role-appropriate onboarding begins.

---

### User Story 4 - App launch routes returning and new users correctly (Priority: P2)

On opening the mobile app, a brief branded launch screen checks whether the device holds a valid, still-active session before showing anything else. A returning user with a valid session lands directly on their role's home screen; everyone else lands on the role-selection entry gate from User Story 1.

**Why this priority**: This is the mechanic that makes User Stories 1 and 2 reachable in the real app and stops a returning user from being dropped back into sign-up. It's supporting infrastructure for the core flows, not a new capability of its own — hence P2 rather than P1.

**Independent Test**: Can be fully tested by launching the app with a valid session (lands on home), with an expired/no session (lands on role selection), and confirming the launch screen never blocks longer than a brief check.

**Acceptance Scenarios**:

1. **Given** a device has a valid, active session, **When** the app launches, **Then** the launch screen resolves directly to that user's role-appropriate home screen with no re-authentication.
2. **Given** a device has no session or an expired one, **When** the app launches, **Then** the launch screen resolves to the role-selection entry gate.
3. **Given** the session check cannot complete (e.g., no network), **When** the app launches, **Then** the user is routed to the role-selection / sign-in entry gate rather than left on the launch screen indefinitely.

---

### User Story 5 - Admin views registered accounts (Priority: P2)

An authorized administrator opens the admin dashboard and sees a list of registered accounts, with enough detail (role, verified phone number, Google-linked status, sign-up method, creation date) to answer "does this person have an account, and how did they get it."

**Why this priority**: This is the minimum visibility needed before any support action or analytics is possible — it's the foundation the other two admin stories build on.

**Independent Test**: Can be fully tested by seeding a handful of accounts created via each sign-up path and confirming they all appear in the admin list with correct role/verification/method detail.

**Acceptance Scenarios**:

1. **Given** accounts have been created via phone and via Google, **When** an administrator opens the account list, **Then** each entry shows role, phone number, Google-linked status, sign-up method, and creation date.
2. **Given** the administrator needs to find one person, **When** they search or filter the list (e.g., by phone number), **Then** matching accounts are returned.

---

### User Story 6 - Admin resolves a stuck sign-up or sign-in (Priority: P2)

A user contacts support because they're locked out after too many failed verification attempts, or their code never arrived. An authorized administrator finds that account and clears the lockout so the user can immediately retry.

**Why this priority**: Without this, a locked-out user simply waits 30 minutes with no recourse — acceptable occasionally, but a real support gap once there are real users.

**Independent Test**: Can be fully tested by driving an account into the 5-attempt lockout state (User Story 1, Acceptance Scenario 5), then confirming an administrator can clear it and the user can immediately request a new code.

**Acceptance Scenarios**:

1. **Given** a phone number is in a 30-minute lockout, **When** an administrator selects that account and clears the lockout, **Then** the user can immediately request a new verification code.
2. **Given** an account has no active lockout, **When** an administrator attempts to clear one anyway, **Then** the action is a no-op and the administrator is told there is nothing to clear.
3. **Given** an administrator clears a lockout, **When** the action completes, **Then** an audit record is created capturing who performed the action, on which account, and when (per this product's standing audit requirement for actions on authentication-related resources).

---

### User Story 7 - Admin sees signup/auth analytics (Priority: P3)

An authorized administrator views aggregate metrics on how people are signing up and signing in: totals by method (phone vs. Google), verification failure rate, and how often lockouts occur, over a selectable time range.

**Why this priority**: Valuable for product visibility but nothing operationally breaks without it — it's reporting, not a workflow any user or support action depends on.

**Independent Test**: Can be fully tested by generating a mix of successful and failed sign-up/sign-in attempts across both methods and confirming the analytics view reflects accurate totals and rates.

**Acceptance Scenarios**:

1. **Given** a mix of phone and Google sign-ups have occurred, **When** an administrator opens the analytics view, **Then** they see a breakdown of sign-ups by method over the selected time range.
2. **Given** some verification attempts have failed, **When** an administrator views analytics, **Then** they see a verification failure rate and a count of lockout occurrences for that range.

---

### Edge Cases

- What happens when a user abandons the flow mid-OTP (backgrounds the app, loses connectivity) and returns after the 10-minute expiry? They must request a new code; no partial account exists.
- What happens when a user pastes a 6-digit string into the OTP input that doesn't match any box focus state? All boxes fill at once from the pasted value.
- What happens when a user hits the 5-failed-attempt lockout and immediately tries a different phone number? The lockout applies only to the number that failed, not the device.
- How does the system handle a user who signs up by phone first and later attempts Google sign-in with a Google account that shares no identifier with their phone-based account? They are treated as a new user and routed into the Google sign-up path (account linking/merging is out of scope for this feature).
- What happens if a user backs out of role selection? There is no back button on that screen — it is the fixed entry gate for unauthenticated users.
- What happens when a user changes their mind about the phone number mid-OTP? A "Change number" link returns them to Sign Up with the number pre-filled, without creating an account.
- What happens when the app-launch session check fails or times out (no network)? The user is routed to the entry gate rather than stuck on the launch screen.
- What happens when an administrator tries to clear a lockout on an account that isn't locked out? The action no-ops with a message; no audit event fires for actions with no effect.
- What happens when a session expires from inactivity or the user signs out? On the next app launch (or attempt to enter a signed-in area) the app resolves to the role-selection entry gate and the user re-authenticates via User Story 2; no account data is lost.
- What happens when the WhatsApp send itself fails at send time (provider/delivery error, distinct from a code that was sent but never arrived)? The user sees a "couldn't send your code" message with an immediate retry; this failure does not count as a verification attempt and does not lock the number.
- What happens when a user keeps requesting new codes (repeatedly resending)? After the send cap is reached within the rolling window (target 5 per 30 minutes), the number enters the same 30-minute lockout as failed attempts and the user must wait or have an administrator clear it.
- What happens when Google OAuth fails or the user cancels it? A genuine Google error shows a brief "couldn't complete Google sign-in" message and returns to the auth screen; a deliberate cancel returns silently. Phone entry stays available and neither case blocks retry.
- What happens when a user signs out on one device while signed in on another? Only the current device's session ends; other devices stay signed in until they expire or are individually signed out.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST require an unauthenticated user to select a role (Coach or Athlete) before any sign-up path begins, with no way to navigate back from that screen.
- **FR-002**: System MUST NOT allow the selected role to be changed later in this release.
- **FR-003**: System MUST restrict phone-based sign-up/sign-in to Egyptian mobile numbers (country code +20) in this release.
- **FR-004**: System MUST normalize an entered phone number by stripping a leading 0 or a pasted +20 prefix, and MUST require exactly 10 significant digits before allowing submission.
- **FR-005**: System MUST check whether a phone number is already registered and, if so, block sign-up with a message directing the user to sign in — before any verification code is sent.
- **FR-006**: System MUST verify phone ownership using a 6-digit code delivered via WhatsApp, for both phone-first and Google-first sign-up paths.
- **FR-007**: System MUST expire each verification code 10 minutes after it is sent.
- **FR-008**: System MUST lock further verification attempts for a given phone number for 30 minutes after 5 consecutive incorrect code submissions.
- **FR-009**: System MUST offer a way to resend the verification code, disabled for 60 seconds after each send.
- **FR-010**: System MUST NOT create a user account until the verification code has been successfully confirmed, regardless of sign-up method.
- **FR-011**: System MUST offer Google sign-in/sign-up as an alternative to manual phone entry on both the Sign Up and Sign In screens.
- **FR-012**: System MUST pre-fill the phone number field from the Google profile when available after Google OAuth, and prompt for manual entry when it is not.
- **FR-013**: System MUST resume an existing session without a new OTP when a returning user signs in via Google and a matching account already exists.
- **FR-014**: System MUST route a Google sign-in attempt with no matching account into the new-user Google sign-up path rather than showing an error.
- **FR-015**: System MUST inform a user attempting to sign in with an unregistered phone number that no account exists, and offer a path to sign up.
- **FR-016**: System MUST route a newly created account into role-appropriate onboarding, and a returning user's resumed session directly to their role's home screen.
- **FR-017**: System MUST NOT expose any password-based authentication or password-recovery affordance anywhere in sign-up or sign-in.
- **FR-018**: Mobile app MUST show a brief branded launch screen while it checks for an existing valid session before showing any auth or home screen.
- **FR-019**: Mobile app MUST route directly to the user's role-appropriate home screen at launch when a valid session exists, without repeating role selection or onboarding.
- **FR-020**: Mobile app MUST route to the role-selection entry gate at launch when no valid session exists, including when the session check itself cannot complete.
- **FR-021**: System MUST let an authorized administrator view a list of registered accounts, showing role, verified phone number, Google-linked status, sign-up method, and creation date, searchable/filterable at minimum by phone number.
- **FR-022**: System MUST let an authorized administrator manually clear an active verification lockout on a specific account so the affected user can immediately retry, with a no-op response when no lockout is active.
- **FR-023**: System MUST record an auditable event — who, what action, on which account, when — for every administrator action that changes an account's authentication state (e.g., clearing a lockout).
- **FR-024**: System MUST let an authorized administrator view aggregate sign-up/authentication analytics (sign-ups by method, verification failure rate, lockout occurrences) over a selectable time range.
- **FR-025**: System MUST keep a signed-in session valid while the app is in active use and MUST automatically expire it after a long idle period (target ~30–90 days of inactivity), after which the user is routed to the role-selection entry gate to re-authenticate via User Story 2.
- **FR-026**: System MUST provide an explicit "Sign out" control for a signed-in user that ends the current session immediately and returns the user to the role-selection entry gate.
- **FR-027**: User-facing mobile screens in this feature (launch, role selection, sign-up, sign-in, OTP) MUST be available in both Arabic and English via a user-selectable language toggle, rendering Arabic right-to-left and English left-to-right; the selection defaults to Arabic for the Egypt-only launch and persists across app launches. UI copy quoted in the scenarios above is illustrative and ships in both languages.
- **FR-028**: When a verification code cannot be sent due to a provider or system error (as opposed to a user entry error), System MUST show a clear "couldn't send your code" message and allow an immediate retry, and MUST NOT count the failure toward the 5-attempt lockout or place the number in lockout.
- **FR-029**: System MUST cap successful verification-code sends to a given phone number within a rolling window (target 5 sends per 30 minutes); exceeding the cap MUST place the number in the same 30-minute lockout used for failed verification attempts (FR-008), clearable by an administrator (FR-022).
- **FR-030**: When Google OAuth fails with a provider/system error, System MUST show a brief "couldn't complete Google sign-in" message and return the user to the auth screen with phone entry available for retry; when the user cancels or backs out of the Google consent step, System MUST return silently to the auth screen with no error. Neither case penalizes the user or blocks retry.
- **FR-031**: System MUST allow one account to hold multiple concurrent sessions (one per device); the "Sign out" control (FR-026) MUST end only the current device's session, and each session MUST expire independently on its own inactivity timer (FR-025). A global "sign out everywhere" is out of scope for this release.

### Key Entities *(include if feature involves data)*

- **User Account**: A Coach or Athlete identity; holds the selected role (fixed after creation), the verified phone number, an optional linked Google identity, a preferred UI language (Arabic or English), and account creation time.
- **Verification Code**: A one-time 6-digit code tied to a phone number; tracks send time, expiry (10 min), failed-attempt count, sends within the rolling window (target max 5 per 30 min), and lockout state (30 min, triggered by 5 failed attempts or by exceeding the send cap). Provider send-failures are not counted as failed attempts.
- **Session**: The signed-in state resumed on returning sign-in or app launch, tied to a User Account, that determines whether a user lands on onboarding (new), the role-selection entry gate (no session), or their role's home screen (returning with a valid session). A session stays valid while the app is in active use, auto-expires after a long idle period (~30–90 days of inactivity), and can be ended immediately by an explicit sign-out; once invalid (expired or signed out) it resolves to the entry gate on next launch. An account may hold multiple concurrent sessions — one per device — each with its own independent inactivity timer; sign-out ends only the current device's session.
- **Admin Audit Event**: An immutable record of an administrator action on an account's authentication state — who performed it, what the action was, which account it affected, and when.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A new user can go from opening the app to a verified, created account in under 2 minutes on the first attempt.
- **SC-002**: A returning user can regain access to their account in under 90 seconds.
- **SC-003**: No two accounts are ever created for the same phone number.
- **SC-004**: Every account that exists, regardless of sign-up path, has a WhatsApp-verified phone number at the moment of creation.
- **SC-005**: At least 95% of verification attempts succeed within 3 code entries, without needing a resend.
- **SC-006**: An administrator can locate a specific account and clear a lockout in under 2 minutes.
- **SC-007**: Signup/auth analytics reflect activity no more than 24 hours old.

## Assumptions

- Egypt-only phone numbers (+20) is a deliberate v0 scope limit; other country codes are out of scope for this feature.
- No password-based authentication exists or is planned anywhere in the product; WhatsApp OTP and Google OAuth are the only two ways to prove identity.
- A WhatsApp messaging channel capable of sending verification codes is available as a dependency to this feature, not something this feature builds.
- Google OAuth as an identity provider is available as a dependency to this feature.
- Linking or merging a phone-based account with a separately-created Google identity for the same real person is out of scope for this feature.
- The specific thresholds carried over from Jira (10-minute OTP expiry, 5-attempt/30-minute lockout, 60-second resend cooldown) are treated as fixed v0 behavior, not user-configurable settings.
- User Stories 5–7 (admin dashboard visibility, support actions, analytics) have no corresponding ticket — they were requested directly and are net-new scope beyond the original epic breakdown. Worth backfilling into the tracker once this spec is approved.
- An existing admin authentication/authorization mechanism (distinct from the coach/athlete auth this feature specifies) is assumed to already exist or be provided elsewhere; this feature only specifies what an already-authorized administrator can do.
- The mobile splash/launch-routing behavior (User Story 4) is supporting UX infrastructure for User Stories 1–2, not an independent business capability.
- The v0 consumer-facing mobile UI is bilingual (Arabic default, English optional, user-toggleable). The admin dashboard is English-only for v0 (confirmed 2026-07-20) as an internal operational tool. The English/LTR mockups produced during specification predate the bilingual decision and need an Arabic/RTL pass (mobile only) to match the design system's RTL kit before implementation.
- A global "sign out everywhere" / remote session-revocation control is deliberately out of scope for v0; sign-out is per-device. It is a natural later addition once account security settings exist.
- PII retention/erasure (account deletion, data-retention windows under Egypt's Personal Data Protection Law) and an explicit accessibility baseline (screen-reader labels, dynamic text sizing, contrast) are not specified here — they are treated as separate cross-cutting concerns to be addressed in planning or their own features, not blockers for this auth spec.
