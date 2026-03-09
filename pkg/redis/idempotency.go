package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// GetIdempotencyResult checks if a result exists for the given key.
// Returns the cached JSON result, or empty string if not found.
func (c *Client) GetIdempotencyResult(ctx context.Context, key string) (string, error) {
	val, err := c.rdb.Get(ctx, idempotencyKey(key)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get idempotency key: %w", err)
	}
	return val, nil
}

// SetIdempotencyResult stores a result for the given key with a TTL.
func (c *Client) SetIdempotencyResult(ctx context.Context, key string, result string, ttl time.Duration) error {
	err := c.rdb.Set(ctx, idempotencyKey(key), result, ttl).Err()
	if err != nil {
		return fmt.Errorf("set idempotency key: %w", err)
	}
	return nil
}

func idempotencyKey(key string) string {
	return "idempotency:" + key
}
