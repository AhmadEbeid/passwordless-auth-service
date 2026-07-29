-- Column order must match scanAccount's Scan.
SELECT id, role, phone, google_subject, google_email, preferred_language, created_at
FROM user_account
WHERE phone = $1;
