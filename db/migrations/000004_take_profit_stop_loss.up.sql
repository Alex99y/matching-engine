-- Bracket orders: an entry may carry take_profit_price / stop_loss_price. When it fills, core
-- inserts one exit order for what the entry received, with status 'pending' and parent_order_id
-- pointing at the entry. The exit has no open_orders row (it is not resting in the book); it is
-- fully described by its orders row and leaves 'pending' when a trigger fires or the user cancels.
ALTER TABLE orders
    ADD COLUMN take_profit_price BIGINT CHECK (take_profit_price > 0),
    ADD COLUMN stop_loss_price   BIGINT CHECK (stop_loss_price > 0),
    ADD COLUMN parent_order_id   UUID REFERENCES orders(id);

ALTER TABLE orders DROP CONSTRAINT orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check
    CHECK (status IN ('pending', 'open', 'filled', 'partially_filled', 'cancelled'));

-- Hydration of pending exits per market: orders has no market_id, so the lookup is by
-- instrument pair. Partial so it only ever holds the (few) parked exits.
CREATE INDEX orders_pending_by_instruments
    ON orders (have_instrument_id, want_instrument_id)
    WHERE status = 'pending';
