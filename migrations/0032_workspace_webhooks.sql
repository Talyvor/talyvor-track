-- 0032_workspace_webhooks.sql — B39.13: a workspace receives Track's issue and comment events as signed webhooks.
-- ADDITIVE ONLY.
--
-- workspace_webhooks is one endpoint an owner registered. The signing secret is sealed in Go with the same
-- AES-256-GCM cipher as workspace_integrations (internal/integrations/crypto.go); there is no plaintext column.
-- events empty means every event.
CREATE TABLE IF NOT EXISTS workspace_webhooks (
    id                TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    workspace_id      TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    url               TEXT NOT NULL,
    events            TEXT[] NOT NULL DEFAULT '{}',
    secret_ciphertext BYTEA NOT NULL,
    secret_nonce      BYTEA NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_workspace_webhooks_workspace ON workspace_webhooks (workspace_id);

-- One row per delivery ATTEMPT. message_id is the webhook-id header and stays the same on every retry of one
-- event; payload is the exact body that was signed, kept as text so a retry sends the same bytes. A row with
-- attempted_at NULL is waiting to be sent at due_at; a failed attempt inserts the next one, a 2xx inserts none.
CREATE TABLE IF NOT EXISTS workspace_webhook_deliveries (
    id           TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    webhook_id   TEXT NOT NULL REFERENCES workspace_webhooks(id) ON DELETE CASCADE,
    message_id   TEXT NOT NULL,
    event_type   TEXT NOT NULL,
    payload      TEXT NOT NULL,
    attempt      INT  NOT NULL,
    due_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempted_at TIMESTAMPTZ,
    status_code  INT,
    error        TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (webhook_id, message_id, attempt)
);
CREATE INDEX IF NOT EXISTS idx_workspace_webhook_deliveries_due
    ON workspace_webhook_deliveries (due_at) WHERE attempted_at IS NULL;
