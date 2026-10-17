package deliveries

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/purangoswami2001/hookrelay/internal/adapters/httpclient"
	"github.com/purangoswami2001/hookrelay/internal/domain"
	"github.com/purangoswami2001/hookrelay/internal/ports"
)

type Service struct {
	deliveries    ports.DeliveryRepository
	events        ports.EventRepository
	subscriptions ports.SubscriptionRepository
	sender        ports.WebhookSender
	logger        *slog.Logger
}

func NewService(
	deliveryRepo ports.DeliveryRepository,
	eventRepo ports.EventRepository,
	subscriptionRepo ports.SubscriptionRepository,
	sender ports.WebhookSender,
	logger *slog.Logger,
) *Service {
	return &Service{
		deliveries:    deliveryRepo,
		events:        eventRepo,
		subscriptions: subscriptionRepo,
		sender:        sender,
		logger:        logger,
	}
}

func (s *Service) Process(
	ctx context.Context,
	deliveryID string,
) error {
	// claim delivery atomically
	if err := s.deliveries.MarkProcessing(ctx, deliveryID); err != nil {
		s.logger.Warn(
			"delivery could not be claimed",
			"delivery_id", deliveryID,
			"error", err,
		)
		return nil
	}

	s.logger.Debug("claimed delivery", "delivery_id", deliveryID)

	delivery, err := s.deliveries.GetByID(ctx, deliveryID)
	if err != nil {
		return fmt.Errorf("load delivery: %w", err)
	}

	event, err := s.events.GetByID(ctx, delivery.EventID)
	if err != nil {
		return fmt.Errorf("load event: %w", err)
	}

	subscription, err := s.subscriptions.GetByID(ctx, delivery.SubscriptionID)
	if err != nil {
		return fmt.Errorf("load subscription: %w", err)
	}

	payload, err := json.Marshal(struct {
		ID        string          `json:"id"`
		Type      string          `json:"type"`
		Data      json.RawMessage `json:"data"`
		CreatedAt time.Time       `json:"created_at"`
	}{
		ID:        event.ID,
		Type:      event.EventType,
		Data:      event.Payload,
		CreatedAt: event.CreatedAt,
	})
	if err != nil {
		return s.failPermanently(ctx, delivery, 0, err.Error())
	}

	startedAt := time.Now()
	timestamp := startedAt.UTC()

	signature := httpclient.SignPayload(subscription.Secret, payload, timestamp)
	headers := map[string]string{
		"X-HookRelay-Event-ID":  event.ID,
		"X-HookRelay-Delivery":  delivery.ID,
		"X-HookRelay-Timestamp": fmt.Sprint(timestamp.Unix()),
		"X-HookRelay-Signature": signature,
	}

	s.logger.Debug("sending webhook", "delivery_id", delivery.ID, "url", subscription.URL, "bytes", len(payload))

	s.logger.Info(
		"webhook delivery started",
		"delivery_id", delivery.ID,
		"event_id", event.ID,
		"attempt", delivery.AttemptCount,
	)

	statusCode, sendErr := s.sender.Send(
		ctx,
		subscription.URL,
		payload,
		headers,
	)

	duration := time.Since(startedAt).Milliseconds()

	s.logger.Debug("response received", "delivery_id", delivery.ID, "status", statusCode, "duration_ms", duration)

	attempt := &domain.DeliveryAttempt{
		DeliveryID:    delivery.ID,
		AttemptNumber: delivery.AttemptCount,
		HTTPStatus:    statusCode,
		DurationMS:    duration,
	}

	if sendErr != nil {
		attempt.Error = sendErr.Error()
	}

	if err := s.deliveries.RecordAttempt(ctx, attempt); err != nil {
		return fmt.Errorf("record delivery attempt: %w", err)
	}

	// 2xx = success
	if sendErr == nil && statusCode >= 200 && statusCode < 300 {
		if err := s.deliveries.MarkDelivered(ctx, delivery.ID, statusCode); err != nil {
			return fmt.Errorf("mark delivery successful: %w", err)
		}

		s.logger.Info(
			"webhook delivery succeeded",
			"delivery_id", delivery.ID,
			"status_code", statusCode,
			"duration_ms", duration,
		)

		// metrics.DeliveriesSucceeded.Inc()
		return nil
	}

	reason := fmt.Sprintf("HTTP status %d", statusCode)
	if sendErr != nil {
		reason = sendErr.Error()
	}

	if !ShouldRetry(statusCode, sendErr) ||
		delivery.AttemptCount >= DefaultMaxAttempts {
		return s.failPermanently(ctx, delivery, statusCode, reason)
	}

	nextAttempt := time.Now().Add(RetryDelay(delivery.AttemptCount))

	if err := s.deliveries.ScheduleRetry(
		ctx,
		delivery.ID,
		nextAttempt,
		statusCode,
		reason,
	); err != nil {
		return fmt.Errorf("schedule retry: %w", err)
	}

	s.logger.Warn(
		"webhook delivery scheduled for retry",
		"delivery_id", delivery.ID,
		"attempt", delivery.AttemptCount,
		"next_attempt_at", nextAttempt,
		"error", reason,
	)

	return nil
}

func (s *Service) failPermanently(
	ctx context.Context,
	delivery *domain.Delivery,
	statusCode int,
	reason string,
) error {
	if err := s.deliveries.MarkDeadLetter(
		ctx,
		delivery.ID,
		statusCode,
		reason,
	); err != nil {
		return fmt.Errorf("mark delivery dead letter: %w", err)
	}

	s.logger.Error(
		"webhook delivery moved to dead letter",
		"delivery_id", delivery.ID,
		"error", reason,
	)

	return nil
}
