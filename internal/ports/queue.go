package ports

import "context"

type Queue interface {
	Enqueue(ctx context.Context, deliveryID string) error
	Dequeue(ctx context.Context) (deliveryID string, err error)
}
