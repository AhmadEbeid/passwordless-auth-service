-- A unique violation on phone is translated to auth.ErrAccountExists.
INSERT INTO user_account
    (id, role, phone, google_subject, google_email, preferred_language, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);
