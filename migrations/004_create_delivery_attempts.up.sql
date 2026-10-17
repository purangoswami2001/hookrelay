CREATE TABLE delivery_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    delivery_id UUID NOT NULL
        REFERENCES deliveries(id) ON DELETE CASCADE,

    attempt_number INTEGER NOT NULL,
    http_status INTEGER,
    duration_ms BIGINT NOT NULL DEFAULT 0,
    error TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT delivery_attempts_number_positive
        CHECK (attempt_number > 0),

    CONSTRAINT delivery_attempts_duration_nonnegative
        CHECK (duration_ms >= 0),

    CONSTRAINT delivery_attempts_unique_number
        UNIQUE (delivery_id, attempt_number)
);

CREATE INDEX idx_delivery_attempts_delivery_id
    ON delivery_attempts (delivery_id, attempt_number);