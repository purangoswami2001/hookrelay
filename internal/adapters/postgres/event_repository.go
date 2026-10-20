package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/purangoswami2001/hookrelay/internal/domain"
)

type EventRepository struct {
	db *pgxpool.Pool
}

func NewEventRepository(db *pgxpool.Pool) *EventRepository {
	return &EventRepository{db: db}
}

func (r *EventRepository) Create(
	ctx context.Context,
	event *domain.Event,
) error {
	query := `
		INSERT INTO events (
			user_id,
			event_type,
			payload,
			idempotency_key
		)
		VALUES (NULLIF($1, '')::uuid, $2, $3, NULLIF($4, ''))
		RETURNING id::text, created_at
	`

	err := r.db.QueryRow(
		ctx,
		query,
		event.UserID,
		event.EventType,
		[]byte(event.Payload),
		event.IdempotencyKey,
	).Scan(&event.ID, &event.CreatedAt)

	if err != nil {
		return fmt.Errorf("create event: %w", err)
	}

	return nil
}

func (r *EventRepository) GetByID(
	ctx context.Context,
	id string,
) (*domain.Event, error) {
	query := `
		SELECT
			id::text,
			COALESCE(user_id::text, ''),
			event_type,
			payload,
			COALESCE(idempotency_key, ''),
			created_at
		FROM events
		WHERE id = $1
	`

	var event domain.Event

	err := r.db.QueryRow(ctx, query, id).Scan(
		&event.ID,
		&event.UserID,
		&event.EventType,
		&event.Payload,
		&event.IdempotencyKey,
		&event.CreatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: event %s not found", pgx.ErrNoRows, id)
	}
	if err != nil {
		return nil, fmt.Errorf("get event: %w", err)
	}

	return &event, nil
}

func (r *EventRepository) GetByIDAndUser(
	ctx context.Context,
	id string,
	userID string,
) (*domain.Event, error) {
	query := `
		SELECT
			id::text,
			COALESCE(user_id::text, ''),
			event_type,
			payload,
			COALESCE(idempotency_key, ''),
			created_at
		FROM events
		WHERE id = $1 AND user_id = $2
	`

	var event domain.Event

	err := r.db.QueryRow(ctx, query, id, userID).Scan(
		&event.ID,
		&event.UserID,
		&event.EventType,
		&event.Payload,
		&event.IdempotencyKey,
		&event.CreatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: event %s not found", pgx.ErrNoRows, id)
	}
	if err != nil {
		return nil, fmt.Errorf("get event by user: %w", err)
	}

	return &event, nil
}

func (r *EventRepository) ListByUser(
	ctx context.Context,
	userID string,
	limit int,
	offset int,
) ([]domain.Event, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT
			id::text,
			COALESCE(user_id::text, ''),
			event_type,
			payload,
			COALESCE(idempotency_key, ''),
			created_at
		FROM events
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list events by user: %w", err)
	}
	defer rows.Close()

	eventsList := make([]domain.Event, 0)
	for rows.Next() {
		var event domain.Event
		if err := rows.Scan(
			&event.ID,
			&event.UserID,
			&event.EventType,
			&event.Payload,
			&event.IdempotencyKey,
			&event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		eventsList = append(eventsList, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}

	return eventsList, nil
}

// Publish creates event, deliveries, and outbox rows in a single atomic transaction scoped to the user
func (r *EventRepository) Publish(
	ctx context.Context,
	event *domain.Event,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin event publishing transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. insert event
	err = tx.QueryRow(
		ctx,
		`INSERT INTO events (user_id, event_type, payload, idempotency_key)
		 VALUES (NULLIF($1, '')::uuid, $2, $3, NULLIF($4, ''))
		 RETURNING id::text, created_at`,
		event.UserID,
		event.EventType,
		[]byte(event.Payload),
		event.IdempotencyKey,
	).Scan(&event.ID, &event.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	// 2. find active subscribers for this event type and this user
	var rows pgx.Rows
	if event.UserID != "" {
		rows, err = tx.Query(
			ctx,
			`SELECT id::text
			 FROM subscriptions
			 WHERE active = TRUE
			   AND user_id = $1::uuid
			   AND $2 = ANY(event_types)`,
			event.UserID,
			event.EventType,
		)
	} else {
		rows, err = tx.Query(
			ctx,
			`SELECT id::text
			 FROM subscriptions
			 WHERE active = TRUE AND $1 = ANY(event_types)`,
			event.EventType,
		)
	}
	if err != nil {
		return fmt.Errorf("find matching subscriptions: %w", err)
	}

	var subscriptionIDs []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan matching subscription: %w", err)
		}
		subscriptionIDs = append(subscriptionIDs, id)
	}

	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate matching subscriptions: %w", err)
	}
	rows.Close()

	// 3. create delivery and outbox records scoped to the user
	for _, subscriptionID := range subscriptionIDs {
		var deliveryID string

		err := tx.QueryRow(
			ctx,
			`INSERT INTO deliveries (user_id, event_id, subscription_id, status)
			 VALUES (NULLIF($1, '')::uuid, $2, $3, 'pending')
			 RETURNING id::text`,
			event.UserID,
			event.ID,
			subscriptionID,
		).Scan(&deliveryID)
		if err != nil {
			return fmt.Errorf("insert delivery: %w", err)
		}

		_, err = tx.Exec(
			ctx,
			`INSERT INTO outbox (delivery_id) VALUES ($1)`,
			deliveryID,
		)
		if err != nil {
			return fmt.Errorf("insert outbox record: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit event publishing transaction: %w", err)
	}

	return nil
}
