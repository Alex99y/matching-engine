DROP INDEX orders_pending_by_instruments;

ALTER TABLE orders DROP CONSTRAINT orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check
    CHECK (status IN ('open', 'filled', 'partially_filled', 'cancelled'));

ALTER TABLE orders
    DROP COLUMN parent_order_id,
    DROP COLUMN stop_loss_price,
    DROP COLUMN take_profit_price;
