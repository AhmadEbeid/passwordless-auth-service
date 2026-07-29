-- Column order must match FindByTokenHash's Scan.
SELECT id, account_id, token_hash, device_label, created_at, last_active_at, expires_at, revoked_at
FROM session
WHERE token_hash = $1;
