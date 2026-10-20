package domain

import "time"

type Subscription struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id,omitempty"`
	URL        string    `json:"url"`
	EventTypes []string  `json:"event_types"`
	Secret     string    `json:"-"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
}
