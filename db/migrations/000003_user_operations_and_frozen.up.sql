ALTER TABLE user_balances ADD COLUMN frozen BIGINT NOT NULL DEFAULT 0;

-- Account-level freeze: blocks fund-moving actions (order creation, faucet) while still
-- allowing the user to cancel resting orders. Distinct from user_balances.frozen above, which
-- holds a per-instrument amount out of the tradeable balance.
ALTER TABLE users ADD COLUMN frozen BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE user_operations (
    id            UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id       UUID NOT NULL REFERENCES users(id),
    instrument_id INT NOT NULL REFERENCES instruments(id),
    amount        BIGINT NOT NULL CHECK (amount > 0),
    type          VARCHAR(10) NOT NULL CHECK (type IN ('deposit', 'withdraw', 'freeze', 'unfreeze')),
    reason        VARCHAR(255),
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX user_operations_user_id_created_at ON user_operations (user_id, created_at DESC);
