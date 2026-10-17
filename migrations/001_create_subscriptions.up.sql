CREATE TABLE subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    url TEXT NOT NULL,
    event_types TEXT[] NOT NULL,
    secret TEXT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT subscriptions_url_not_empty
        CHECK (length(trim(url)) > 0),

    CONSTRAINT subscriptions_event_types_not_empty
        CHECK (cardinality(event_types) > 0)
);

CREATE INDEX idx_subscriptions_active
    ON subscriptions (active);