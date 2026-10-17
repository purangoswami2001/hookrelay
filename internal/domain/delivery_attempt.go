package domain

import "time"

type DeliveryAttempt struct {
	ID            string    `json:"id"`
	DeliveryID    string    `json:"delivery_id"`
	AttemptNumber int       `json:"attempt_number"`
	HTTPStatus    int       `json:"http_status"`
	DurationMS    int64     `json:"duration_ms"`
	Error         string    `json:"error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}
