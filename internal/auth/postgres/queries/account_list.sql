-- Admin account list, newest first. $1 arrives pre-escaped by escapeLike, so
-- ESCAPE '\' makes a literal % or _ in the filter match itself rather than act
-- as a wildcard. An empty filter collapses the pattern to '%%' and matches all.
-- Column order must match scanAccount's Scan.
SELECT id, role, phone, google_subject, google_email, preferred_language, created_at
FROM user_account
WHERE phone LIKE '%' || $1 || '%' ESCAPE '\'
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;
