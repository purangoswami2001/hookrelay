package worker

import (
	"context"
	"log/slog"
	"sync"
)

type Job struct {
	DeliveryID string
}

type Handler func(context.Context, Job) error

type Pool struct {
	workerCount int
	jobs        <-chan Job
	handler     Handler
	logger      *slog.Logger
}

func NewPool(
	workerCount int,
	jobs <-chan Job,
	handler Handler,
	logger *slog.Logger,
) *Pool {
	if workerCount < 1 {
		workerCount = 1
	}

	return &Pool{
		workerCount: workerCount,
		jobs:        jobs,
		handler:     handler,
		logger:      logger,
	}
}

func (p *Pool) Run(ctx context.Context) {
	var wg sync.WaitGroup

	p.logger.Info("worker pool starting", "worker_count", p.workerCount)

	for i := 0; i < p.workerCount; i++ {
		wg.Add(1)

		go func(workerID int) {
			defer wg.Done()

			p.logger.Debug("pool worker spawned", "worker_id", workerID)

			for {
				select {
				case <-ctx.Done():
					return

				case job, ok := <-p.jobs:
					if !ok {
						return
					}

					p.logger.Debug("running job", "worker_id", workerID, "delivery_id", job.DeliveryID)

					if err := p.handler(ctx, job); err != nil {
						p.logger.Error(
							"job processing failed",
							"worker_id", workerID,
							"delivery_id", job.DeliveryID,
							"error", err,
						)
					}
				}
			}
		}(i + 1)
	}

	<-ctx.Done()
	wg.Wait()

	p.logger.Info("worker pool stopped")
}
