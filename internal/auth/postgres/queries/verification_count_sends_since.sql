-- Approximates the rolling send-cap window by summing the per-row counters of
-- challenges last sent inside it.
SELECT COALESCE(SUM(sends_in_window), 0)
FROM verification
WHERE phone = $1 AND sent_at >= $2;
