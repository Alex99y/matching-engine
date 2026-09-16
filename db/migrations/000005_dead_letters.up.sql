-- This table stores the messages that the core service couldn't process (DLQ)
CREATE TABLE dead_letters (
    id           BIGSERIAL PRIMARY KEY,
    message_id   VARCHAR(64) NOT NULL DEFAULT '',
    market_ref   VARCHAR(32) NOT NULL,
    order_id     UUID,
    event_type   VARCHAR(16) NOT NULL DEFAULT '',
    reason       VARCHAR(16) NOT NULL CHECK (reason IN ('malformed', 'invalid', 'unknown_type', 'poison')),
    error        TEXT NOT NULL DEFAULT '',
    payload      JSONB NOT NULL,
    dead_at      TIMESTAMP NOT NULL,
    recorded_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    status       VARCHAR(16) NOT NULL DEFAULT 'parked' CHECK (status IN ('parked', 'replayed', 'discarded'))
);

CREATE INDEX dead_letters_market_recorded ON dead_letters (market_ref, recorded_at DESC);
