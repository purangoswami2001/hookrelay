package users_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/purangoswami2001/hookrelay/internal/application/users"
	"github.com/purangoswami2001/hookrelay/internal/domain"
)

type mockUserRepo struct {
	mu     sync.Mutex
	users  map[string]*domain.User
	byHash map[string]*domain.User
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{
		users:  make(map[string]*domain.User),
		byHash: make(map[string]*domain.User),
	}
}

func (m *mockUserRepo) Create(ctx context.Context, u *domain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, existing := range m.users {
		if existing.Email == u.Email {
			return users.ErrUserExists
		}
	}

	u.ID = "user-123"
	m.users[u.ID] = u
	m.byHash[u.APIKeyHash] = u
	return nil
}

func (m *mockUserRepo) GetByID(ctx context.Context, id string) (*domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	u, ok := m.users[id]
	if !ok {
		return nil, users.ErrInvalidUser
	}
	return u, nil
}

func (m *mockUserRepo) GetByAPIKeyHash(ctx context.Context, hash string) (*domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	u, ok := m.byHash[hash]
	if !ok {
		return nil, users.ErrInvalidUser
	}
	return u, nil
}

func (m *mockUserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, u := range m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, users.ErrInvalidUser
}

func (m *mockUserRepo) UpdateAPIKey(ctx context.Context, userID, newHash, newPrefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	u, ok := m.users[userID]
	if !ok {
		return users.ErrInvalidUser
	}

	delete(m.byHash, u.APIKeyHash)
	u.APIKeyHash = newHash
	u.APIKeyPrefix = newPrefix
	m.byHash[newHash] = u
	return nil
}

func TestUserService_RegisterAndAuth(t *testing.T) {
	repo := newMockUserRepo()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := users.NewService(repo, logger)
	ctx := context.Background()

	// 1. Invalid email
	_, _, err := service.Register(ctx, "invalid-email", "Alice")
	if err == nil {
		t.Fatal("expected error for invalid email")
	}

	// 2. Empty name
	_, _, err = service.Register(ctx, "alice@example.com", "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}

	// 3. Successful registration
	user, apiKey, err := service.Register(ctx, "alice@example.com", "Alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if user.ID == "" || apiKey == "" {
		t.Fatalf("expected non-empty user ID and api key")
	}
	if len(apiKey) < 40 {
		t.Fatalf("expected long api key, got %s", apiKey)
	}

	// 4. Duplicate registration
	_, _, err = service.Register(ctx, "alice@example.com", "Alice Duplicate")
	if err == nil {
		t.Fatal("expected error for duplicate email")
	}

	// 5. Authenticate with valid API key
	authUser, err := service.AuthenticateByAPIKey(ctx, apiKey)
	if err != nil {
		t.Fatalf("authentication failed: %v", err)
	}
	if authUser.ID != user.ID {
		t.Fatalf("expected user ID %s, got %s", user.ID, authUser.ID)
	}

	// 6. Authenticate with invalid API key
	_, err = service.AuthenticateByAPIKey(ctx, "hr_live_invalidkey")
	if err == nil {
		t.Fatal("expected error for invalid api key")
	}

	// 7. Regenerate API key
	newKey, newPrefix, err := service.RegenerateAPIKey(ctx, user.ID)
	if err != nil {
		t.Fatalf("failed to regenerate api key: %v", err)
	}
	if newKey == apiKey {
		t.Fatal("expected new key to be different from old key")
	}
	if newPrefix == "" {
		t.Fatal("expected non-empty prefix")
	}

	// 8. Old key should now be rejected
	_, err = service.AuthenticateByAPIKey(ctx, apiKey)
	if err == nil {
		t.Fatal("expected old key to be rejected after regeneration")
	}

	// 9. New key should succeed
	authUserAfterRegen, err := service.AuthenticateByAPIKey(ctx, newKey)
	if err != nil {
		t.Fatalf("failed to authenticate with new key: %v", err)
	}
	if authUserAfterRegen.ID != user.ID {
		t.Fatalf("expected user ID %s, got %s", user.ID, authUserAfterRegen.ID)
	}
}
