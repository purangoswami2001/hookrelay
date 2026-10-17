package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/purangoswami2001/hookrelay/internal/ports"
)

type OutboxRepository struct {
	db *pgxpool.Pool
}

func NewOutboxRepository(db *pgxpool.Pool) *OutboxRepository {
	return &OutboxRepository{db: db}
}

func (r *OutboxRepository) ListUnpublished(
	ctx context.Context,
	limit int,
) ([]ports.OutboxRecord, error) {
	if limit <= 0 {
		limit = 100
	}

	// append FOR UPDATE SKIP LOCKED if running multiple dispatcher replicas
	query := `
		SELECT id::text, delivery_id::text
		FROM outbox
		WHERE published_at IS NULL
		ORDER BY created_at ASC
		LIMIT $1
	`

	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("list unpublished outbox records: %w", err)
	}
	defer rows.Close()

	records := make([]ports.OutboxRecord, 0)

	for rows.Next() {
		var record ports.OutboxRecord

		if err := rows.Scan(&record.ID, &record.DeliveryID); err != nil {
			return nil, fmt.Errorf("scan outbox record: %w", err)
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox records: %w", err)
	}

	return records, nil
}

func (r *OutboxRepository) MarkPublished(
	ctx context.Context,
	id string,
) error {
	query := `
		UPDATE outbox
		SET published_at = NOW()
		WHERE id = $1 AND published_at IS NULL
	`

	result, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("mark outbox record published: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("outbox record %q not found or already published", id)
	}

	return nil
}
