-- +goose Up
ALTER TABLE verification
    ADD COLUMN pending_google_subject text,
    ADD COLUMN pending_google_email   text;

CREATE TABLE audit_event (
    id                uuid PRIMARY KEY,
    actor_admin_id    text NOT NULL,
    action            text NOT NULL,
    target_account_id uuid NOT NULL REFERENCES user_account (id),
    created_at        timestamptz NOT NULL,
    metadata          jsonb
);

CREATE INDEX audit_event_target_account_id_idx ON audit_event (target_account_id);

-- +goose Down
DROP TABLE IF EXISTS audit_event;

ALTER TABLE verification
    DROP COLUMN IF EXISTS pending_google_subject,
    DROP COLUMN IF EXISTS pending_google_email;
