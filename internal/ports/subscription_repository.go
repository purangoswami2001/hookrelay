package ports

import (
	"context"

	"github.com/purangoswami2001/hookrelay/internal/domain"
)

type SubscriptionRepository interface {
	Create(ctx context.Context, sub *domain.Subscription) error
	GetByID(ctx context.Context, id string) (*domain.Subscription, error)
	GetByIDAndUser(ctx context.Context, id, userID string) (*domain.Subscription, error)
	ListByUser(ctx context.Context, userID string) ([]domain.Subscription, error)
	Update(ctx context.Context, sub *domain.Subscription) error
	Delete(ctx context.Context, id, userID string) error
	ListByEventType(ctx context.Context, userID, eventType string) ([]domain.Subscription, error)
}
