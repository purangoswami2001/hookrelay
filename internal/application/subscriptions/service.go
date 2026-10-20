package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/purangoswami2001/hookrelay/internal/domain"
	"github.com/purangoswami2001/hookrelay/internal/ports"
)

var ErrInvalidSubscription = errors.New("invalid subscription")

type Service struct {
	repository ports.SubscriptionRepository
	logger     *slog.Logger
}

func NewService(
	repository ports.SubscriptionRepository,
	logger *slog.Logger,
) *Service {
	return &Service{
		repository: repository,
		logger:     logger,
	}
}

func (s *Service) Create(
	ctx context.Context,
	subscription *domain.Subscription,
) error {
	if subscription == nil {
		return fmt.Errorf("%w: subscription is required", ErrInvalidSubscription)
	}

	if err := validateSubscription(subscription); err != nil {
		return err
	}

	subscription.URL = strings.TrimSpace(subscription.URL)
	subscription.Active = true

	s.logger.Debug("saving subscription", "user_id", subscription.UserID, "url", subscription.URL, "events", subscription.EventTypes)

	if err := s.repository.Create(ctx, subscription); err != nil {
		s.logger.Error("create subscription failed", "error", err)
		return fmt.Errorf("create subscription: %w", err)
	}

	s.logger.Info(
		"subscription created",
		"subscription_id", subscription.ID,
		"user_id", subscription.UserID,
		"active", subscription.Active,
	)

	return nil
}

func (s *Service) GetByID(
	ctx context.Context,
	id string,
) (*domain.Subscription, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: ID is required", ErrInvalidSubscription)
	}

	return s.repository.GetByID(ctx, id)
}

func (s *Service) GetByIDAndUser(
	ctx context.Context,
	id string,
	userID string,
) (*domain.Subscription, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: ID is required", ErrInvalidSubscription)
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("%w: user ID is required", ErrInvalidSubscription)
	}

	return s.repository.GetByIDAndUser(ctx, id, userID)
}

func (s *Service) ListByUser(
	ctx context.Context,
	userID string,
) ([]domain.Subscription, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("%w: user ID is required", ErrInvalidSubscription)
	}

	return s.repository.ListByUser(ctx, userID)
}

func (s *Service) Update(
	ctx context.Context,
	subscription *domain.Subscription,
) error {
	if subscription == nil {
		return fmt.Errorf("%w: subscription is required", ErrInvalidSubscription)
	}

	if strings.TrimSpace(subscription.ID) == "" {
		return fmt.Errorf("%w: ID is required", ErrInvalidSubscription)
	}
	if strings.TrimSpace(subscription.UserID) == "" {
		return fmt.Errorf("%w: user ID is required", ErrInvalidSubscription)
	}

	if err := validateSubscription(subscription); err != nil {
		return err
	}

	subscription.URL = strings.TrimSpace(subscription.URL)

	s.logger.Debug("updating subscription", "id", subscription.ID, "user_id", subscription.UserID, "active", subscription.Active)

	if err := s.repository.Update(ctx, subscription); err != nil {
		return fmt.Errorf("update subscription: %w", err)
	}

	s.logger.Info("subscription updated", "subscription_id", subscription.ID, "user_id", subscription.UserID)
	return nil
}

func (s *Service) Delete(ctx context.Context, id string, userID string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%w: ID is required", ErrInvalidSubscription)
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return fmt.Errorf("%w: user ID is required", ErrInvalidSubscription)
	}

	if err := s.repository.Delete(ctx, id, userID); err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}

	s.logger.Info("subscription deleted", "subscription_id", id, "user_id", userID)
	return nil
}

func validateSubscription(subscription *domain.Subscription) error {
	parsedURL, err := url.ParseRequestURI(strings.TrimSpace(subscription.URL))
	if err != nil {
		return fmt.Errorf("%w: valid URL is required", ErrInvalidSubscription)
	}

	if (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") ||
		parsedURL.Hostname() == "" ||
		parsedURL.User != nil {
		return fmt.Errorf("%w: URL must be an HTTP or HTTPS URL without credentials", ErrInvalidSubscription)
	}

	if len(subscription.EventTypes) == 0 {
		return fmt.Errorf("%w: at least one event type is required", ErrInvalidSubscription)
	}

	seen := make(map[string]struct{}, len(subscription.EventTypes))

	for i, eventType := range subscription.EventTypes {
		eventType = strings.TrimSpace(eventType)
		if eventType == "" {
			return fmt.Errorf("%w: event types cannot be empty", ErrInvalidSubscription)
		}

		if len(eventType) > 200 {
			return fmt.Errorf("%w: event type cannot exceed 200 characters", ErrInvalidSubscription)
		}

		if _, exists := seen[eventType]; exists {
			return fmt.Errorf("%w: duplicate event type %q", ErrInvalidSubscription, eventType)
		}

		seen[eventType] = struct{}{}
		subscription.EventTypes[i] = eventType
	}

	return nil
}
