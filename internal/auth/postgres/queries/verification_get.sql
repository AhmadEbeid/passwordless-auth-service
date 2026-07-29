-- Column order must match scanVerification's Scan, as must
-- verification_find_latest_by_phone.sql.
SELECT id, phone, intent, code_hash, pending_role, pending_language,
       pending_google_subject, pending_google_email,
       sent_at, expires_at, failed_attempts, sends_in_window, locked_until, status
FROM verification
WHERE id = $1;
