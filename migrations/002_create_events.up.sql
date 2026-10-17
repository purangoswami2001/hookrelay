CREATE TABLE events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    idempotency_key TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT events_type_not_empty
        CHECK (length(trim(event_type)) > 0),

    CONSTRAINT events_payload_is_object
        CHECK (jsonb_typeof(payload) = 'object'),

    CONSTRAINT events_idempotency_key_unique
        UNIQUE (idempotency_key)
);

CREATE INDEX idx_events_event_type
    ON events (event_type);

CREATE INDEX idx_events_created_at
    ON events (created_at DESC);