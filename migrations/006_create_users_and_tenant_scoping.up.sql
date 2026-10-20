CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    api_key_hash TEXT NOT NULL UNIQUE,
    api_key_prefix TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT users_email_not_empty
        CHECK (length(trim(email)) > 0),

    CONSTRAINT users_name_not_empty
        CHECK (length(trim(name)) > 0),

    CONSTRAINT users_api_key_hash_not_empty
        CHECK (length(trim(api_key_hash)) > 0)
);

CREATE INDEX idx_users_email
    ON users (email);

CREATE INDEX idx_users_api_key_hash
    ON users (api_key_hash);

-- Tenant scoping for subscriptions
ALTER TABLE subscriptions
    ADD COLUMN user_id UUID REFERENCES users(id) ON DELETE CASCADE;

CREATE INDEX idx_subscriptions_user_id
    ON subscriptions (user_id);

CREATE INDEX idx_subscriptions_user_active
    ON subscriptions (user_id, active);

-- Tenant scoping for events
ALTER TABLE events
    ADD COLUMN user_id UUID REFERENCES users(id) ON DELETE CASCADE;

CREATE INDEX idx_events_user_id
    ON events (user_id);

-- Update idempotency constraint: unique per user (null idempotency keys are allowed)
ALTER TABLE events
    DROP CONSTRAINT events_idempotency_key_unique;

ALTER TABLE events
    ADD CONSTRAINT events_user_idempotency_key_unique
    UNIQUE (user_id, idempotency_key);

-- Tenant scoping for deliveries
ALTER TABLE deliveries
    ADD COLUMN user_id UUID REFERENCES users(id) ON DELETE CASCADE;

CREATE INDEX idx_deliveries_user_id
    ON deliveries (user_id);
