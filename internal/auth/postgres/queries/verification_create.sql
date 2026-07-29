INSERT INTO verification
    (id, phone, intent, code_hash, pending_role, pending_language,
     pending_google_subject, pending_google_email,
     sent_at, expires_at, failed_attempts, sends_in_window, locked_until, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14);
