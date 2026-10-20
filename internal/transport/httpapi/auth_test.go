package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/purangoswami2001/hookrelay/internal/application/events"
	"github.com/purangoswami2001/hookrelay/internal/application/subscriptions"
	"github.com/purangoswami2001/hookrelay/internal/application/users"
	"github.com/purangoswami2001/hookrelay/internal/domain"
	"github.com/purangoswami2001/hookrelay/internal/ports"
	"github.com/purangoswami2001/hookrelay/internal/transport/httpapi"
)

// In-memory repositories for testing
type mockUserRepo struct {
	mu     sync.Mutex
	users  map[string]*domain.User
	byHash map[string]*domain.User
}

func (m *mockUserRepo) Create(ctx context.Context, u *domain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u.ID = "user-" + u.Email
	m.users[u.ID] = u
	m.byHash[u.APIKeyHash] = u
	return nil
}

func (m *mockUserRepo) GetByID(ctx context.Context, id string) (*domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.users[id], nil
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
	return nil, nil
}

func (m *mockUserRepo) UpdateAPIKey(ctx context.Context, userID, newHash, newPrefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.users[userID]
	if u == nil {
		return users.ErrInvalidUser
	}
	delete(m.byHash, u.APIKeyHash)
	u.APIKeyHash = newHash
	u.APIKeyPrefix = newPrefix
	m.byHash[newHash] = u
	return nil
}

type mockSubRepo struct {
	mu   sync.Mutex
	subs map[string]*domain.Subscription
}

func (m *mockSubRepo) Create(ctx context.Context, sub *domain.Subscription) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sub.ID = "sub-1"
	m.subs[sub.ID] = sub
	return nil
}

func (m *mockSubRepo) GetByID(ctx context.Context, id string) (*domain.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.subs[id], nil
}

func (m *mockSubRepo) GetByIDAndUser(ctx context.Context, id, userID string) (*domain.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.subs[id]
	if s != nil && s.UserID == userID {
		return s, nil
	}
	return nil, nil
}

func (m *mockSubRepo) ListByUser(ctx context.Context, userID string) ([]domain.Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []domain.Subscription
	for _, s := range m.subs {
		if s.UserID == userID {
			result = append(result, *s)
		}
	}
	return result, nil
}

func (m *mockSubRepo) Update(ctx context.Context, sub *domain.Subscription) error {
	return nil
}

func (m *mockSubRepo) Delete(ctx context.Context, id, userID string) error {
	return nil
}

func (m *mockSubRepo) ListByEventType(ctx context.Context, userID, eventType string) ([]domain.Subscription, error) {
	return nil, nil
}

type mockEventRepo struct{}

func (m *mockEventRepo) Create(ctx context.Context, event *domain.Event) error { return nil }
func (m *mockEventRepo) GetByID(ctx context.Context, id string) (*domain.Event, error) {
	return nil, nil
}
func (m *mockEventRepo) GetByIDAndUser(ctx context.Context, id, userID string) (*domain.Event, error) {
	return nil, nil
}
func (m *mockEventRepo) ListByUser(ctx context.Context, userID string, limit, offset int) ([]domain.Event, error) {
	return []domain.Event{}, nil
}
func (m *mockEventRepo) Publish(ctx context.Context, event *domain.Event) error {
	event.ID = "event-1"
	return nil
}

type mockDeliveryRepo struct{}

func (m *mockDeliveryRepo) Create(ctx context.Context, d *domain.Delivery) error { return nil }
func (m *mockDeliveryRepo) GetByID(ctx context.Context, id string) (*domain.Delivery, error) {
	return nil, nil
}
func (m *mockDeliveryRepo) GetByIDAndUser(ctx context.Context, id, userID string) (*domain.Delivery, error) {
	return nil, nil
}
func (m *mockDeliveryRepo) List(ctx context.Context, filter ports.DeliveryFilter) ([]domain.Delivery, error) {
	return []domain.Delivery{}, nil
}
func (m *mockDeliveryRepo) GetAttempts(ctx context.Context, deliveryID string) ([]domain.DeliveryAttempt, error) {
	return nil, nil
}
func (m *mockDeliveryRepo) MarkProcessing(ctx context.Context, id string) error { return nil }
func (m *mockDeliveryRepo) MarkDelivered(ctx context.Context, id string, statusCode int) error {
	return nil
}
func (m *mockDeliveryRepo) ScheduleRetry(ctx context.Context, id string, nextAttempt time.Time, statusCode int, reason string) error {
	return nil
}
func (m *mockDeliveryRepo) MarkDeadLetter(ctx context.Context, id string, statusCode int, reason string) error {
	return nil
}
func (m *mockDeliveryRepo) RecordAttempt(ctx context.Context, attempt *domain.DeliveryAttempt) error {
	return nil
}

func setupTestServer() (http.Handler, *users.Service) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	userRepo := &mockUserRepo{
		users:  make(map[string]*domain.User),
		byHash: make(map[string]*domain.User),
	}
	userService := users.NewService(userRepo, logger)

	subRepo := &mockSubRepo{subs: make(map[string]*domain.Subscription)}
	subService := subscriptions.NewService(subRepo, logger)

	eventRepo := &mockEventRepo{}
	eventService := events.NewService(eventRepo, logger)

	deliveryRepo := &mockDeliveryRepo{}

	router := httpapi.NewRouter(
		userService,
		eventService,
		eventRepo,
		subService,
		deliveryRepo,
		logger,
	)

	return router, userService
}

func TestRouter_AuthAndTenantScoping(t *testing.T) {
	router, _ := setupTestServer()

	// 1. Health check should be public
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /health, got %d", rec.Code)
	}

	// 2. Accessing protected endpoint without API key should return 401
	req = httptest.NewRequest("GET", "/api/v1/subscriptions", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for /api/v1/subscriptions without key, got %d", rec.Code)
	}

	// 3. Register a user via public endpoint
	regBody := bytes.NewBufferString(`{"email":"bob@example.com","name":"Bob"}`)
	req = httptest.NewRequest("POST", "/api/v1/users/register", regBody)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for register, got %d: %s", rec.Code, rec.Body.String())
	}

	var regResp struct {
		User   domain.User `json:"user"`
		APIKey string      `json:"api_key"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&regResp); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	apiKey := regResp.APIKey
	if apiKey == "" {
		t.Fatal("expected api_key in response")
	}

	// 4. Access /api/v1/users/me with Bearer token
	req = httptest.NewRequest("GET", "/api/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/v1/users/me, got %d", rec.Code)
	}

	// 5. Access with X-API-Key header
	req = httptest.NewRequest("GET", "/api/v1/subscriptions", nil)
	req.Header.Set("X-API-Key", apiKey)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /api/v1/subscriptions with X-API-Key, got %d", rec.Code)
	}

	// 6. Create subscription with API key
	subBody := bytes.NewBufferString(`{"url":"https://example.com/webhook","event_types":["order.created"]}`)
	req = httptest.NewRequest("POST", "/api/v1/subscriptions", subBody)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for create subscription, got %d: %s", rec.Code, rec.Body.String())
	}

	// 7. Regenerate key
	req = httptest.NewRequest("POST", "/api/v1/users/me/regenerate-key", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for regenerate key, got %d: %s", rec.Code, rec.Body.String())
	}

	var regenResp struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&regenResp); err != nil {
		t.Fatalf("decode regenerate response: %v", err)
	}
	newKey := regenResp.APIKey

	// 8. Old key should now be 401 Unauthorized
	req = httptest.NewRequest("GET", "/api/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with terminated old key, got %d", rec.Code)
	}

	// 9. New key should succeed with 200 OK
	req = httptest.NewRequest("GET", "/api/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+newKey)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with new key, got %d", rec.Code)
	}
}
