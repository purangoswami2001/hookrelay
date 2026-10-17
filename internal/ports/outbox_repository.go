package ports

import "context"

type OutboxRecord struct {
	ID         string
	DeliveryID string
}

type OutboxRepository interface {
	ListUnpublished(ctx context.Context, limit int) ([]OutboxRecord, error)
	MarkPublished(ctx context.Context, id string) error
}
