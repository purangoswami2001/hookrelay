package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/purangoswami2001/hookrelay/internal/domain"
	"github.com/purangoswami2001/hookrelay/internal/ports"
)

type DeliveryRepository struct {
	db *pgxpool.Pool
}

func NewDeliveryRepository(db *pgxpool.Pool) *DeliveryRepository {
	return &DeliveryRepository{db: db}
}

func (r *DeliveryRepository) Create(
	ctx context.Context,
	delivery *domain.Delivery,
) error {
	query := `
		INSERT INTO deliveries (
			event_id,
			subscription_id,
			status,
			attempt_count
		)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text, created_at, updated_at
	`

	err := r.db.QueryRow(
		ctx,
		query,
		delivery.EventID,
		delivery.SubscriptionID,
		delivery.Status,
		delivery.AttemptCount,
	).Scan(
		&delivery.ID,
		&delivery.CreatedAt,
		&delivery.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("create delivery: %w", err)
	}

	return nil
}

func (r *DeliveryRepository) GetByID(
	ctx context.Context,
	id string,
) (*domain.Delivery, error) {
	query := `
		SELECT
			id::text,
			event_id::text,
			subscription_id::text,
			status,
			attempt_count,
			next_attempt_at,
			COALESCE(last_http_status, 0),
			COALESCE(last_error, ''),
			created_at,
			updated_at
		FROM deliveries
		WHERE id = $1
	`

	var d domain.Delivery
	var nextAttempt pgtype.Timestamptz

	err := r.db.QueryRow(ctx, query, id).Scan(
		&d.ID,
		&d.EventID,
		&d.SubscriptionID,
		&d.Status,
		&d.AttemptCount,
		&nextAttempt,
		&d.LastHTTPStatus,
		&d.LastError,
		&d.CreatedAt,
		&d.UpdatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: delivery %s not found", pgx.ErrNoRows, id)
	}
	if err != nil {
		return nil, fmt.Errorf("get delivery: %w", err)
	}

	if nextAttempt.Valid {
		d.NextAttemptAt = &nextAttempt.Time
	}

	// load attempt logs
	attempts, err := r.GetAttempts(ctx, id)
	if err == nil && len(attempts) > 0 {
		d.Attempts = attempts
	}

	return &d, nil
}

func (r *DeliveryRepository) List(
	ctx context.Context,
	filter ports.DeliveryFilter,
) ([]domain.Delivery, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	baseQuery := `
		SELECT
			id::text,
			event_id::text,
			subscription_id::text,
			status,
			attempt_count,
			next_attempt_at,
			COALESCE(last_http_status, 0),
			COALESCE(last_error, ''),
			created_at,
			updated_at
		FROM deliveries
	`

	var (
		rows pgx.Rows
		err  error
	)

	if filter.EventID != "" {
		query := baseQuery + `
		WHERE event_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
		`
		rows, err = r.db.Query(ctx, query, filter.EventID, limit, offset)
	} else {
		query := baseQuery + `
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
		`
		rows, err = r.db.Query(ctx, query, limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	defer rows.Close()

	deliveries := make([]domain.Delivery, 0)

	for rows.Next() {
		var d domain.Delivery
		var nextAttempt pgtype.Timestamptz

		if err := rows.Scan(
			&d.ID,
			&d.EventID,
			&d.SubscriptionID,
			&d.Status,
			&d.AttemptCount,
			&nextAttempt,
			&d.LastHTTPStatus,
			&d.LastError,
			&d.CreatedAt,
			&d.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan delivery: %w", err)
		}

		if nextAttempt.Valid {
			d.NextAttemptAt = &nextAttempt.Time
		}

		deliveries = append(deliveries, d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deliveries: %w", err)
	}

	return deliveries, nil
}

// MarkProcessing claims a delivery using optimistic locking
func (r *DeliveryRepository) MarkProcessing(
	ctx context.Context,
	id string,
) error {
	query := `
		UPDATE deliveries
		SET
			status = 'processing',
			attempt_count = attempt_count + 1,
			updated_at = NOW()
		WHERE id = $1
		  AND status IN ('pending', 'retry_waiting')
		  AND (
			next_attempt_at IS NULL
			OR next_attempt_at <= NOW()
		  )
	`

	tag, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("mark delivery processing: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf(
			"delivery %s is not eligible for processing",
			id,
		)
	}

	return nil
}

func (r *DeliveryRepository) MarkDelivered(
	ctx context.Context,
	id string,
	statusCode int,
) error {
	query := `
		UPDATE deliveries
		SET
			status = 'delivered',
			last_http_status = $2,
			last_error = NULL,
			next_attempt_at = NULL,
			updated_at = NOW()
		WHERE id = $1
		  AND status = 'processing'
	`

	tag, err := r.db.Exec(ctx, query, id, statusCode)
	if err != nil {
		return fmt.Errorf("mark delivery delivered: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delivery %s is not processing", id)
	}

	return nil
}

func (r *DeliveryRepository) ScheduleRetry(
	ctx context.Context,
	id string,
	nextAttempt time.Time,
	statusCode int,
	reason string,
) error {
	query := `
		UPDATE deliveries
		SET
			status = 'retry_waiting',
			next_attempt_at = $2,
			last_http_status = NULLIF($3, 0),
			last_error = $4,
			updated_at = NOW()
		WHERE id = $1
		  AND status = 'processing'
	`

	tag, err := r.db.Exec(
		ctx,
		query,
		id,
		nextAttempt,
		statusCode,
		reason,
	)
	if err != nil {
		return fmt.Errorf("schedule delivery retry: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delivery %s is not processing", id)
	}

	return nil
}

func (r *DeliveryRepository) MarkDeadLetter(
	ctx context.Context,
	id string,
	statusCode int,
	reason string,
) error {
	query := `
		UPDATE deliveries
		SET
			status = 'dead_letter',
			last_http_status = NULLIF($2, 0),
			last_error = $3,
			next_attempt_at = NULL,
			updated_at = NOW()
		WHERE id = $1
		  AND status = 'processing'
	`

	tag, err := r.db.Exec(ctx, query, id, statusCode, reason)
	if err != nil {
		return fmt.Errorf("mark delivery dead letter: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delivery %s is not processing", id)
	}

	return nil
}

func (r *DeliveryRepository) RecordAttempt(
	ctx context.Context,
	attempt *domain.DeliveryAttempt,
) error {
	query := `
		INSERT INTO delivery_attempts (
			delivery_id,
			attempt_number,
			http_status,
			duration_ms,
			error
		)
		VALUES ($1, $2, NULLIF($3, 0), $4, NULLIF($5, ''))
		RETURNING id::text, created_at
	`

	err := r.db.QueryRow(
		ctx,
		query,
		attempt.DeliveryID,
		attempt.AttemptNumber,
		attempt.HTTPStatus,
		attempt.DurationMS,
		attempt.Error,
	).Scan(&attempt.ID, &attempt.CreatedAt)

	if err != nil {
		return fmt.Errorf("record delivery attempt: %w", err)
	}

	return nil
}

func (r *DeliveryRepository) GetAttempts(
	ctx context.Context,
	deliveryID string,
) ([]domain.DeliveryAttempt, error) {
	query := `
		SELECT
			id::text,
			delivery_id::text,
			attempt_number,
			COALESCE(http_status, 0),
			duration_ms,
			COALESCE(error, ''),
			created_at
		FROM delivery_attempts
		WHERE delivery_id = $1
		ORDER BY attempt_number ASC
	`

	rows, err := r.db.Query(ctx, query, deliveryID)
	if err != nil {
		return nil, fmt.Errorf("get delivery attempts: %w", err)
	}
	defer rows.Close()

	attempts := make([]domain.DeliveryAttempt, 0)
	for rows.Next() {
		var a domain.DeliveryAttempt
		if err := rows.Scan(
			&a.ID,
			&a.DeliveryID,
			&a.AttemptNumber,
			&a.HTTPStatus,
			&a.DurationMS,
			&a.Error,
			&a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan delivery attempt: %w", err)
		}
		attempts = append(attempts, a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate delivery attempts: %w", err)
	}

	return attempts, nil
}
