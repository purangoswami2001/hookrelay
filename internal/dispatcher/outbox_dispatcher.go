package dispatcher

import (
	"context"
	"log/slog"
	"time"

	"github.com/purangoswami2001/hookrelay/internal/observability"
	"github.com/purangoswami2001/hookrelay/internal/ports"
)

type OutboxDispatcher struct {
	repository ports.OutboxRepository
	queue      ports.Queue
	logger     *slog.Logger
	batchSize  int
	interval   time.Duration
}

func NewOutboxDispatcher(
	repository ports.OutboxRepository,
	queue ports.Queue,
	logger *slog.Logger,
) *OutboxDispatcher {
	return &OutboxDispatcher{
		repository: repository,
		queue:      queue,
		logger:     logger,
		batchSize:  100,
		interval:   2 * time.Second,
	}
}

func (d *OutboxDispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	d.logger.Info("outbox dispatcher started", "poll_interval", d.interval)

	for {
		if err := d.DispatchBatch(ctx); err != nil && ctx.Err() == nil {
			d.logger.Error(
				"outbox batch dispatch failed",
				"error", err,
			)
		}

		select {
		case <-ctx.Done():
			d.logger.Info("outbox dispatcher stopping")
			return
		case <-ticker.C:
		}
	}
}

func (d *OutboxDispatcher) DispatchBatch(ctx context.Context) error {
	d.logger.Debug("polling outbox", "batch_size", d.batchSize)

	// Promote delayed deliveries due for retry into the active queue
	if promoted, err := d.queue.PromoteDue(ctx); err != nil {
		d.logger.Error("failed to promote scheduled deliveries", "error", err)
	} else if promoted > 0 {
		observability.ScheduledPromotionsTotal.Add(float64(promoted))
		d.logger.Info("promoted scheduled deliveries to active queue", "count", promoted)
	}

	records, err := d.repository.ListUnpublished(ctx, d.batchSize)
	if err != nil {
		return err
	}

	if len(records) == 0 {
		return nil
	}

	d.logger.Debug("found unpublished outbox records", "count", len(records))

	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := d.queue.Enqueue(ctx, record.DeliveryID); err != nil {
			d.logger.Error(
				"failed to enqueue delivery",
				"delivery_id", record.DeliveryID,
				"outbox_id", record.ID,
				"error", err,
			)
			continue
		}

		if err := d.repository.MarkPublished(ctx, record.ID); err != nil {
			d.logger.Error(
				"failed to mark outbox record published",
				"outbox_id", record.ID,
				"delivery_id", record.DeliveryID,
				"error", err,
			)
			continue
		}

		d.logger.Debug(
			"dispatched delivery to queue",
			"delivery_id", record.DeliveryID,
		)
	}

	// if len(records) == d.batchSize { go d.DispatchBatch(ctx) }

	return nil
}
