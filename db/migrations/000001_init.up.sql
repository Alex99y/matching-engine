CREATE TABLE users  (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    username VARCHAR(25) NOT NULL,
    email VARCHAR(100) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX users_username_uk ON users (username);

CREATE TABLE instruments (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    symbol VARCHAR(10) UNIQUE NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    decimals INT NOT NULL
);

CREATE TABLE user_balances (
    id SERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id),
    instrument_id INT NOT NULL REFERENCES instruments(id),
    balance BIGINT NOT NULL DEFAULT 0,
    blocked BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX user_balances_user_instrument_uk ON user_balances (user_id, instrument_id);

CREATE TABLE markets (
    id SERIAL PRIMARY KEY,
    base_instrument_id INT NOT NULL REFERENCES instruments(id),
    quote_instrument_id INT NOT NULL REFERENCES instruments(id),
    price_quantum BIGINT NOT NULL DEFAULT 1,
    amount_quantum BIGINT NOT NULL DEFAULT 1,
    min_order_size BIGINT NOT NULL DEFAULT 1,
    max_order_size BIGINT NOT NULL DEFAULT 1000000000,
    -- Maker/taker fees in basis points (1 bp = 0.01%), charged on the asset each party
    -- receives at match time. Defaulted to 0 so CreateMarket needs no change.
    taker_fee_bps BIGINT NOT NULL DEFAULT 0 CHECK (taker_fee_bps BETWEEN 0 AND 10000),
    maker_fee_bps BIGINT NOT NULL DEFAULT 0 CHECK (maker_fee_bps BETWEEN 0 AND 10000),
    CONSTRAINT markets_base_quote_uk UNIQUE (base_instrument_id, quote_instrument_id)
);


--How each combination behaves at the matching engine level:
--- Limit + GTC — the only type that actually sits in open_orders. Has a price, waits until filled or expires_at is reached.
--- Limit + IOC — tries to fill immediately at the given price; whatever doesn't fill goes straight to cancelled_orders. Never enters open_orders.
--- Limit + FOK — must fill 100% immediately at the given price or the whole order cancels. Never enters open_orders.
--- Market + IOC — fills as much as possible at the best available price immediately; remainder cancels. No price column needed. Never enters open_orders.
--- Market + FOK — same as above but all-or-nothing. Never enters open_orders.
--- Market + GTC — nonsensical combination. A market order has no price to sit in the book with. You should reject this at the application layer.

CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    client_order_id VARCHAR(64),
    user_id UUID NOT NULL REFERENCES users(id),
    have_instrument_id INT NOT NULL REFERENCES instruments(id),
    want_instrument_id INT NOT NULL REFERENCES instruments(id),
    have_quantity BIGINT,
    want_quantity BIGINT,
    CHECK (have_quantity IS NOT NULL OR want_quantity IS NOT NULL),
    status VARCHAR(10) NOT NULL CHECK (status IN ('open', 'filled', 'partially_filled', 'cancelled')),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP,
    type          VARCHAR(6) NOT NULL CHECK (type IN ('limit', 'market')),
    time_in_force VARCHAR(3) NOT NULL CHECK (time_in_force IN ('GTC', 'IOC', 'FOK')),
    CHECK (
        (type = 'limit'  AND have_quantity IS NOT NULL) OR
        (type = 'market' AND (have_quantity IS NOT NULL OR want_quantity IS NOT NULL))
    )
);

CREATE INDEX orders_user_id_created_at ON orders (user_id, created_at DESC);
CREATE UNIQUE INDEX orders_client_order_id_user_id_uk
    ON orders (user_id, client_order_id)
    WHERE client_order_id IS NOT NULL;


CREATE TABLE open_orders (
    id BIGSERIAL PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id),
    price BIGINT NOT NULL,
    market_id INT NOT NULL REFERENCES markets(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    side VARCHAR(4) NOT NULL CHECK (side IN ('buy', 'sell')),
    remaining_have_amount BIGINT NOT NULL,
    remaining_want_amount BIGINT NOT NULL
);

CREATE INDEX idx_open_orders_order_id ON open_orders (order_id);
CREATE INDEX idx_open_orders_asks ON open_orders (market_id, price ASC)  WHERE side = 'sell';
CREATE INDEX idx_open_orders_bids ON open_orders (market_id, price DESC) WHERE side = 'buy';


CREATE TABLE cancelled_orders (
    id BIGSERIAL PRIMARY KEY,
    order_id UUID NOT NULL REFERENCES orders(id),
    cancelled_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    remaining_have_amount BIGINT NOT NULL,
    remaining_want_amount BIGINT NOT NULL
);

CREATE INDEX idx_cancelled_orders_order_id ON cancelled_orders (order_id);

CREATE TABLE matches (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    market_id INT NOT NULL REFERENCES markets(id),
    buy_order_id UUID NOT NULL REFERENCES orders(id),
    sell_order_id UUID NOT NULL REFERENCES orders(id),
    match_buy_amount BIGINT NOT NULL,
    match_sell_amount BIGINT NOT NULL,
    match_price BIGINT NOT NULL,
    match_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    match_sell_fees BIGINT NOT NULL,
    match_buy_fees BIGINT NOT NULL,
    buy_order_is_taker BOOLEAN NOT NULL,
    is_buy_order_filled BOOLEAN NOT NULL,
    is_sell_order_filled BOOLEAN NOT NULL
);

CREATE INDEX idx_matches_market_id ON matches (market_id, match_time DESC);
CREATE INDEX idx_matches_buy_order_id ON matches (buy_order_id);
CREATE INDEX idx_matches_sell_order_id ON matches (sell_order_id);