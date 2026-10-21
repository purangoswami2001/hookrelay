package ports

import (
	"context"
	"time"
)

type Queue interface {
	Enqueue(ctx context.Context, deliveryID string) error
	Dequeue(ctx context.Context) (deliveryID string, err error)
	EnqueueScheduled(ctx context.Context, deliveryID string, executeAt time.Time) error
	PromoteDue(ctx context.Context) (int64, error)
	Length(ctx context.Context) (active int64, delayed int64, err error)
}
