-- The batched form of find-latest-then-check-lock, so the admin account list
-- costs one statement rather than one per row. DISTINCT ON with this ORDER BY
-- keeps the most recently sent challenge per phone; a phone with no challenge
-- simply yields no row. The lock itself is evaluated in Go against the caller's
-- clock, not here.
SELECT DISTINCT ON (phone) phone, locked_until
FROM verification
WHERE phone = ANY($1)
ORDER BY phone, sent_at DESC;
