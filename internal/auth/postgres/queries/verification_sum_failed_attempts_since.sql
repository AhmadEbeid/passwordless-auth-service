SELECT COALESCE(SUM(failed_attempts), 0)
FROM verification
WHERE sent_at >= $1;
