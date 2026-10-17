package ports

import (
	"context"
	"time"

	"github.com/purangoswami2001/hookrelay/internal/domain"
)

type DeliveryFilter struct {
	EventID string
	Limit   int
	Offset  int
}

type DeliveryRepository interface {
	Create(ctx context.Context, delivery *domain.Delivery) error
	GetByID(ctx context.Context, id string) (*domain.Delivery, error)
	List(ctx context.Context, filter DeliveryFilter) ([]domain.Delivery, error)
	GetAttempts(ctx context.Context, deliveryID string) ([]domain.DeliveryAttempt, error)

	MarkProcessing(ctx context.Context, id string) error
	MarkDelivered(ctx context.Context, id string, statusCode int) error
	ScheduleRetry(
		ctx context.Context,
		id string,
		nextAttempt time.Time,
		statusCode int,
		reason string,
	) error
	MarkDeadLetter(ctx context.Context, id string, statusCode int, reason string) error

	RecordAttempt(ctx context.Context, attempt *domain.DeliveryAttempt) error
}
