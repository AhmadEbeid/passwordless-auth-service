-- Column order must match scanVerification's Scan, as must
-- verification_get.sql.
SELECT id, phone, intent, code_hash, pending_role, pending_language,
       pending_google_subject, pending_google_email,
       sent_at, expires_at, failed_attempts, sends_in_window, locked_until, status
FROM verification
WHERE phone = $1
ORDER BY sent_at DESC
LIMIT 1;
