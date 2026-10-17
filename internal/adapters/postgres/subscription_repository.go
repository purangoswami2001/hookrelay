package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/purangoswami2001/hookrelay/internal/domain"
)

type SubscriptionRepository struct {
	db *pgxpool.Pool
}

func NewSubscriptionRepository(db *pgxpool.Pool) *SubscriptionRepository {
	return &SubscriptionRepository{db: db}
}

func (r *SubscriptionRepository) Create(
	ctx context.Context,
	sub *domain.Subscription,
) error {
	query := `
		INSERT INTO subscriptions (
			url,
			event_types,
			secret,
			active
		)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text, created_at
	`

	err := r.db.QueryRow(
		ctx,
		query,
		sub.URL,
		sub.EventTypes,
		sub.Secret,
		sub.Active,
	).Scan(&sub.ID, &sub.CreatedAt)

	if err != nil {
		return fmt.Errorf("create subscription: %w", err)
	}

	return nil
}

func (r *SubscriptionRepository) GetByID(
	ctx context.Context,
	id string,
) (*domain.Subscription, error) {
	query := `
		SELECT
			id::text,
			url,
			event_types,
			secret,
			active,
			created_at
		FROM subscriptions
		WHERE id = $1
	`

	var sub domain.Subscription

	err := r.db.QueryRow(ctx, query, id).Scan(
		&sub.ID,
		&sub.URL,
		&sub.EventTypes,
		&sub.Secret,
		&sub.Active,
		&sub.CreatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: subscription %s not found", pgx.ErrNoRows, id)
	}
	if err != nil {
		return nil, fmt.Errorf("get subscription: %w", err)
	}

	return &sub, nil
}

func (r *SubscriptionRepository) List(
	ctx context.Context,
) ([]domain.Subscription, error) {
	query := `
		SELECT
			id::text,
			url,
			event_types,
			secret,
			active,
			created_at
		FROM subscriptions
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	defer rows.Close()

	subscriptions := make([]domain.Subscription, 0)

	for rows.Next() {
		var sub domain.Subscription

		if err := rows.Scan(
			&sub.ID,
			&sub.URL,
			&sub.EventTypes,
			&sub.Secret,
			&sub.Active,
			&sub.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}

		subscriptions = append(subscriptions, sub)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscriptions: %w", err)
	}

	return subscriptions, nil
}

func (r *SubscriptionRepository) Update(
	ctx context.Context,
	sub *domain.Subscription,
) error {
	query := `
		UPDATE subscriptions
		SET
			url = $2,
			event_types = $3,
			active = $4,
			updated_at = NOW()
		WHERE id = $1
	`

	tag, err := r.db.Exec(
		ctx,
		query,
		sub.ID,
		sub.URL,
		sub.EventTypes,
		sub.Active,
	)
	if err != nil {
		return fmt.Errorf("update subscription: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("subscription %s not found", sub.ID)
	}

	return nil
}

func (r *SubscriptionRepository) Delete(
	ctx context.Context,
	id string,
) error {
	query := `DELETE FROM subscriptions WHERE id = $1`

	tag, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("subscription %s not found", id)
	}

	return nil
}

func (r *SubscriptionRepository) ListByEventType(
	ctx context.Context,
	eventType string,
) ([]domain.Subscription, error) {
	query := `
		SELECT
			id::text,
			url,
			event_types,
			secret,
			active,
			created_at
		FROM subscriptions
		WHERE active = TRUE
		  AND $1 = ANY(event_types)
		ORDER BY created_at DESC
	`

	rows, err := r.db.Query(ctx, query, eventType)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions by event type: %w", err)
	}
	defer rows.Close()

	subscriptions := make([]domain.Subscription, 0)

	for rows.Next() {
		var sub domain.Subscription

		if err := rows.Scan(
			&sub.ID,
			&sub.URL,
			&sub.EventTypes,
			&sub.Secret,
			&sub.Active,
			&sub.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}

		subscriptions = append(subscriptions, sub)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscriptions: %w", err)
	}

	return subscriptions, nil
}
