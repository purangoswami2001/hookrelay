ALTER TABLE deliveries DROP COLUMN IF EXISTS user_id;

ALTER TABLE events DROP CONSTRAINT IF EXISTS events_user_idempotency_key_unique;
ALTER TABLE events ADD CONSTRAINT events_idempotency_key_unique UNIQUE (idempotency_key);
ALTER TABLE events DROP COLUMN IF EXISTS user_id;

ALTER TABLE subscriptions DROP COLUMN IF EXISTS user_id;

DROP TABLE IF EXISTS users CASCADE;
