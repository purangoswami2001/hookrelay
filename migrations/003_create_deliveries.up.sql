CREATE TABLE deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    event_id UUID NOT NULL
        REFERENCES events(id) ON DELETE CASCADE,

    subscription_id UUID NOT NULL
        REFERENCES subscriptions(id) ON DELETE RESTRICT,

    status TEXT NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,

    next_attempt_at TIMESTAMPTZ,
    last_http_status INTEGER,
    last_error TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT deliveries_status_valid
        CHECK (
            status IN (
                'pending',
                'processing',
                'delivered',
                'retry_waiting',
                'dead_letter'
            )
        ),

    CONSTRAINT deliveries_attempt_count_nonnegative
        CHECK (attempt_count >= 0),

    CONSTRAINT deliveries_event_subscription_unique
        UNIQUE (event_id, subscription_id)
);

CREATE INDEX idx_deliveries_status_next_attempt
    ON deliveries (status, next_attempt_at);

CREATE INDEX idx_deliveries_event_id
    ON deliveries (event_id);

CREATE INDEX idx_deliveries_subscription_id
    ON deliveries (subscription_id);

CREATE INDEX idx_deliveries_created_at
    ON deliveries (created_at DESC);