-- Append-only: no update or delete statement exists for this table.
INSERT INTO audit_event
    (id, actor_admin_id, action, target_account_id, created_at, metadata)
VALUES ($1, $2, $3, $4, $5, $6);
