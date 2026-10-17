package ports

import (
	"context"

	"github.com/purangoswami2001/hookrelay/internal/domain"
)

type SubscriptionRepository interface {
	Create(ctx context.Context, sub *domain.Subscription) error
	GetByID(ctx context.Context, id string) (*domain.Subscription, error)
	List(ctx context.Context) ([]domain.Subscription, error)
	Update(ctx context.Context, sub *domain.Subscription) error
	Delete(ctx context.Context, id string) error
	ListByEventType(ctx context.Context, eventType string) ([]domain.Subscription, error)
}
