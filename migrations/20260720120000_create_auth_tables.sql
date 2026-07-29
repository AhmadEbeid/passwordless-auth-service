-- +goose Up
CREATE TABLE user_account (
    id                 uuid PRIMARY KEY,
    role               text NOT NULL,
    phone              text NOT NULL UNIQUE,
    google_subject     text UNIQUE,
    google_email       text,
    preferred_language text NOT NULL DEFAULT 'ar',
    created_at         timestamptz NOT NULL
);

CREATE TABLE verification (
    id                uuid PRIMARY KEY,
    phone             text NOT NULL,
    intent            text NOT NULL,
    code_hash         text NOT NULL,
    pending_role      text,
    pending_language  text,
    sent_at           timestamptz NOT NULL,
    expires_at        timestamptz NOT NULL,
    failed_attempts   integer NOT NULL DEFAULT 0,
    sends_in_window   integer NOT NULL DEFAULT 0,
    locked_until      timestamptz,
    status            text NOT NULL
);

CREATE INDEX verification_phone_idx ON verification (phone);

CREATE TABLE session (
    id             uuid PRIMARY KEY,
    account_id     uuid NOT NULL REFERENCES user_account (id),
    token_hash     text NOT NULL UNIQUE,
    device_label   text,
    created_at     timestamptz NOT NULL,
    last_active_at timestamptz NOT NULL,
    expires_at     timestamptz NOT NULL,
    revoked_at     timestamptz
);

CREATE INDEX session_account_id_idx ON session (account_id);

-- +goose Down
DROP TABLE IF EXISTS session;
DROP TABLE IF EXISTS verification;
DROP TABLE IF EXISTS user_account;
