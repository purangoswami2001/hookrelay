package domain

import (
	"encoding/json"
	"time"
)

type Event struct {
	ID             string          `json:"id"`
	UserID         string          `json:"user_id,omitempty"`
	EventType      string          `json:"type"`
	Payload        json.RawMessage `json:"data"`
	IdempotencyKey string          `json:"-"`
	CreatedAt      time.Time       `json:"created_at"`
}
