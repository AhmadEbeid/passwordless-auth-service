-- Signup breakdown for admin analytics: phone-only first, Google-linked second.
SELECT
    COUNT(*) FILTER (WHERE google_subject IS NULL),
    COUNT(*) FILTER (WHERE google_subject IS NOT NULL)
FROM user_account
WHERE created_at >= $1;
