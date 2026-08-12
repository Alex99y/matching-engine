CREATE TABLE sessions (
    id          UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  CHAR(64) NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at  TIMESTAMP NOT NULL,
    revoked_at  TIMESTAMP,
    origin      VARCHAR(10) NOT NULL DEFAULT 'login' CHECK (origin IN ('login', 'minted')),
    scope       VARCHAR(5)  NOT NULL DEFAULT 'write' CHECK (scope IN ('read', 'write')),
    user_agent  VARCHAR(512),
    ip_address  INET
);

CREATE UNIQUE INDEX sessions_token_hash_uk ON sessions (token_hash);
CREATE INDEX sessions_user_id_created_at ON sessions (user_id, created_at DESC);
