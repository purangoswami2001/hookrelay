package ports

import (
	"context"
	"time"

	"github.com/purangoswami2001/hookrelay/internal/domain"
)

type DeliveryFilter struct {
	UserID         string
	EventID        string
	SubscriptionID string
	Limit          int
	Offset         int
}

type ReplayFilter struct {
	UserID         string
	EventID        string
	SubscriptionID string
	Limit          int
}

type DeliveryRepository interface {
	Create(ctx context.Context, delivery *domain.Delivery) error
	GetByID(ctx context.Context, id string) (*domain.Delivery, error)
	GetByIDAndUser(ctx context.Context, id, userID string) (*domain.Delivery, error)
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
	ResetForRetry(ctx context.Context, id string, userID string) error
	ReplayDeadLetters(ctx context.Context, filter ReplayFilter) ([]string, error)

	RecordAttempt(ctx context.Context, attempt *domain.DeliveryAttempt) error
}

