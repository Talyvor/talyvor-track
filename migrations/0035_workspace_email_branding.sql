-- 0035_workspace_email_branding.sql — B39.18: the header colour of a workspace's emails.
-- ADDITIVE ONLY. Empty means Talyvor's default; the name and logo_url already live on workspaces.
ALTER TABLE workspaces ADD COLUMN IF NOT EXISTS email_header_color TEXT NOT NULL DEFAULT ''
    CHECK (email_header_color = '' OR email_header_color ~ '^#[0-9a-fA-F]{6}$');
