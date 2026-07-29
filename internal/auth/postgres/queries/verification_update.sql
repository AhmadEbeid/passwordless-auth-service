-- Zero rows affected means the challenge vanished; the caller maps that to
-- auth.ErrNotFound.
UPDATE verification
SET code_hash = $2,
    sent_at = $3,
    expires_at = $4,
    failed_attempts = $5,
    sends_in_window = $6,
    locked_until = $7,
    status = $8
WHERE id = $1;
