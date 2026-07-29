-- token_hash is the SHA-256 of the opaque token; the token itself is never
-- stored.
INSERT INTO session
    (id, account_id, token_hash, device_label, created_at, last_active_at, expires_at, revoked_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);
