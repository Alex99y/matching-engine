CREATE TABLE sessions (
    id         UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash CHAR(64) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    revoked_at TIMESTAMP
);

CREATE UNIQUE INDEX sessions_token_hash_uk ON sessions (token_hash);
CREATE INDEX sessions_user_id_created_at ON sessions (user_id, created_at DESC);
