package ports

import (
	"context"

	"github.com/purangoswami2001/hookrelay/internal/domain"
)

type EventPublisher interface {
	Publish(ctx context.Context, event *domain.Event) error
}
