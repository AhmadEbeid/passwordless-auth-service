SELECT COUNT(*)
FROM verification
WHERE status = $1 AND sent_at >= $2;
