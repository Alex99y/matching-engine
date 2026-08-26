DROP TABLE user_operations;

ALTER TABLE users DROP COLUMN frozen;
ALTER TABLE user_balances DROP COLUMN frozen;
