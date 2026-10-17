package ports

import (
	"context"

	"github.com/purangoswami2001/hookrelay/internal/domain"
)

type EventRepository interface {
	Create(ctx context.Context, event *domain.Event) error
	GetByID(ctx context.Context, id string) (*domain.Event, error)
}
