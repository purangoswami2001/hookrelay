package ports

import (
	"context"

	"github.com/purangoswami2001/hookrelay/internal/domain"
)

type EventRepository interface {
	Create(ctx context.Context, event *domain.Event) error
	GetByID(ctx context.Context, id string) (*domain.Event, error)
	GetByIDAndUser(ctx context.Context, id, userID string) (*domain.Event, error)
	ListByUser(ctx context.Context, userID string, limit, offset int) ([]domain.Event, error)
}
