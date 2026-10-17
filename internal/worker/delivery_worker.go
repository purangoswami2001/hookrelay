package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/purangoswami2001/hookrelay/internal/ports"
)

type DeliveryProcessor interface {
	Process(ctx context.Context, deliveryID string) error
}

type DeliveryWorker struct {
	queue     ports.Queue
	processor DeliveryProcessor
	logger    *slog.Logger
}

func NewDeliveryWorker(
	queue ports.Queue,
	processor DeliveryProcessor,
	logger *slog.Logger,
) *DeliveryWorker {
	return &DeliveryWorker{
		queue:     queue,
		processor: processor,
		logger:    logger,
	}
}

func (w *DeliveryWorker) Run(ctx context.Context, workerID int) {
	w.logger.Info("delivery worker started", "worker_id", workerID)

	for {
		if ctx.Err() != nil {
			w.logger.Info("delivery worker stopping", "worker_id", workerID)
			return
		}

		deliveryID, err := w.queue.Dequeue(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}

			w.logger.Error(
				"failed to dequeue delivery",
				"worker_id", workerID,
				"error", err,
			)

			// backoff before retrying dequeue
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}

			continue
		}

		w.logger.Debug("dequeued delivery", "worker_id", workerID, "delivery_id", deliveryID)

		if err := w.processor.Process(ctx, deliveryID); err != nil {
			w.logger.Error(
				"delivery processing failed",
				"worker_id", workerID,
				"delivery_id", deliveryID,
				"error", err,
			)
		} else {
			w.logger.Debug("finished delivery", "worker_id", workerID, "delivery_id", deliveryID)
		}
	}
}
