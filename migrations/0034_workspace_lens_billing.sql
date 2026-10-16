-- 0034_workspace_lens_billing.sql — B17.220: the Lens workspace(s) each Track workspace is billed to.
-- ADDITIVE ONLY.
--
-- Track mints its own workspace ids, so Lens never holds spend under them. The gateway names the caller's
-- Lens workspace on every request (X-Talyvor-Workspace); Track records it here against the workspace the
-- request acted on, and the AI-cost syncer pulls per-request spend per recorded Lens workspace.
CREATE TABLE IF NOT EXISTS workspace_lens_billing (
    workspace_id      TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    lens_workspace_id TEXT NOT NULL,
    first_seen_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, lens_workspace_id)
);
