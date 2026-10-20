package ports

import (
	"context"

	"github.com/purangoswami2001/hookrelay/internal/domain"
)

type UserRepository interface {
	Create(ctx context.Context, user *domain.User) error
	GetByID(ctx context.Context, id string) (*domain.User, error)
	GetByAPIKeyHash(ctx context.Context, apiKeyHash string) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	UpdateAPIKey(ctx context.Context, userID, newAPIKeyHash, newAPIKeyPrefix string) error
}
