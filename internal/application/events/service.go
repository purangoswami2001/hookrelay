package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/purangoswami2001/hookrelay/internal/domain"
	"github.com/purangoswami2001/hookrelay/internal/ports"
)

type Service struct {
	publisher ports.EventPublisher
	logger    *slog.Logger
}

func NewService(
	publisher ports.EventPublisher,
	logger *slog.Logger,
) *Service {
	return &Service{
		publisher: publisher,
		logger:    logger,
	}
}

func (s *Service) Publish(
	ctx context.Context,
	userID string,
	eventType string,
	payload json.RawMessage,
	idempotencyKey string,
) (*domain.Event, error) {
	eventType = strings.TrimSpace(eventType)

	if eventType == "" {
		return nil, fmt.Errorf("event type is required")
	}

	if len(payload) == 0 || !json.Valid(payload) {
		return nil, fmt.Errorf("payload must be valid JSON")
	}

	// must be a JSON object {...}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil || object == nil {
		return nil, fmt.Errorf("payload must be a JSON object")
	}

	if len(idempotencyKey) > 200 {
		return nil, fmt.Errorf("idempotency key cannot exceed 200 characters")
	}

	s.logger.Debug("publishing event", "user_id", userID, "type", eventType, "has_idempotency_key", idempotencyKey != "")

	event := &domain.Event{
		UserID:         userID,
		EventType:      eventType,
		Payload:        payload,
		IdempotencyKey: idempotencyKey,
	}

	if err := s.publisher.Publish(ctx, event); err != nil {
		s.logger.Error(
			"event publishing failed",
			"user_id", userID,
			"event_type", eventType,
			"error", err,
		)
		return nil, err
	}

	s.logger.Info(
		"event published",
		"event_id", event.ID,
		"user_id", event.UserID,
		"event_type", event.EventType,
	)

	return event, nil
}
