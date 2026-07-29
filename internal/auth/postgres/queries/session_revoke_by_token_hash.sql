-- Revoking an already-revoked or unknown session affects no rows, which the
-- caller treats as success: sign-out is idempotent.
UPDATE session
SET revoked_at = $2
WHERE token_hash = $1 AND revoked_at IS NULL;
