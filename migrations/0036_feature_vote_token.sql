-- 0036_feature_vote_token.sql — B42.7: a public vote is removed only with the token issued when it was cast.
-- ADDITIVE ONLY. Stores the SHA-256 of the token, never the token. Votes cast before this have none and
-- can no longer be removed from the public board.
ALTER TABLE feature_votes ADD COLUMN IF NOT EXISTS token_hash TEXT;
