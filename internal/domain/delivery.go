package domain

import "time"

type DeliveryStatus string

const (
	DeliveryPending      DeliveryStatus = "pending"
	DeliveryProcessing   DeliveryStatus = "processing"
	DeliveryDelivered    DeliveryStatus = "delivered"
	DeliveryRetryWaiting DeliveryStatus = "retry_waiting"
	DeliveryDeadLetter   DeliveryStatus = "dead_letter"
)

type Delivery struct {
	ID             string            `json:"id"`
	UserID         string            `json:"user_id,omitempty"`
	EventID        string            `json:"event_id"`
	SubscriptionID string            `json:"subscription_id"`
	Status         DeliveryStatus    `json:"status"`
	AttemptCount   int               `json:"attempt_count"`
	NextAttemptAt  *time.Time        `json:"next_attempt_at,omitempty"`
	LastHTTPStatus int               `json:"last_http_status,omitempty"`
	LastError      string            `json:"last_error,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	Attempts       []DeliveryAttempt `json:"attempts,omitempty"`
}
