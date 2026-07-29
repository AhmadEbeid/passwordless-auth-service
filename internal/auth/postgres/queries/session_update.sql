-- Persists the rolling idle-expiry extension. Throttled by the caller, so this
-- runs far less often than session validation does.
UPDATE session
SET last_active_at = $2,
    expires_at = $3
WHERE id = $1;
