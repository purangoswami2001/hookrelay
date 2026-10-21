package redis

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// luaPromoteDue atomically moves up to limit ready jobs from delayed ZSET to active list
const luaPromoteDue = `
local now = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local jobs = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', now, 'LIMIT', 0, limit)
if #jobs > 0 then
    for _, id in ipairs(jobs) do
        redis.call('LPUSH', KEYS[2], id)
    end
    for _, id in ipairs(jobs) do
        redis.call('ZREM', KEYS[1], id)
    end
end
return #jobs
`

type Queue struct {
	client     *goredis.Client
	key        string
	delayedKey string
}

func NewQueue(addr string) *Queue {
	client := goredis.NewClient(&goredis.Options{
		Addr:         addr,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	})

	return &Queue{
		client:     client,
		key:        "hookrelay:deliveries",
		delayedKey: "hookrelay:deliveries:delayed",
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

func (q *Queue) EnqueueScheduled(
	ctx context.Context,
	deliveryID string,
	executeAt time.Time,
) error {
	if deliveryID == "" {
		return fmt.Errorf("delivery ID cannot be empty")
	}

	score := float64(executeAt.UnixMilli())
	if err := q.client.ZAdd(ctx, q.delayedKey, goredis.Z{
		Score:  score,
		Member: deliveryID,
	}).Err(); err != nil {
		return fmt.Errorf("enqueue scheduled delivery: %w", err)
	}

	return nil
}

func (q *Queue) PromoteDue(ctx context.Context) (int64, error) {
	nowMilli := time.Now().UnixMilli()
	res, err := q.client.Eval(ctx, luaPromoteDue, []string{q.delayedKey, q.key}, nowMilli, 100).Result()
	if err != nil {
		return 0, fmt.Errorf("promote due delayed deliveries: %w", err)
	}

	count, ok := res.(int64)
	if !ok {
		return 0, nil
	}
	return count, nil
}

func (q *Queue) Length(ctx context.Context) (int64, int64, error) {
	active, err := q.client.LLen(ctx, q.key).Result()
	if err != nil {
		return 0, 0, fmt.Errorf("get active queue length: %w", err)
	}

	delayed, err := q.client.ZCard(ctx, q.delayedKey).Result()
	if err != nil {
		return 0, 0, fmt.Errorf("get delayed queue length: %w", err)
	}

	return active, delayed, nil
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
