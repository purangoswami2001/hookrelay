package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type Queue struct {
	client *goredis.Client
	key    string
}

func NewQueue(addr string) *Queue {
	client := goredis.NewClient(&goredis.Options{
		Addr:         addr,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	})

	return &Queue{
		client: client,
		key:    "hookrelay:deliveries",
	}
}

func (q *Queue) Ping(ctx context.Context) error {
	if err := q.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping Redis: %w", err)
	}
	return nil
}

func (q *Queue) Enqueue(
	ctx context.Context,
	deliveryID string,
) error {
	if deliveryID == "" {
		return fmt.Errorf("delivery ID cannot be empty")
	}

	if err := q.client.LPush(ctx, q.key, deliveryID).Err(); err != nil {
		return fmt.Errorf("enqueue delivery: %w", err)
	}

	return nil
}

func (q *Queue) Dequeue(ctx context.Context) (string, error) {
	// blocks until an item is available
	result, err := q.client.BRPop(ctx, 0, q.key).Result()
	if err != nil {
		return "", fmt.Errorf("dequeue delivery: %w", err)
	}

	if len(result) != 2 {
		return "", fmt.Errorf("unexpected Redis queue response")
	}

	return result[1], nil
}

func (q *Queue) Close() error {
	return q.client.Close()
}
