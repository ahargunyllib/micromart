package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Lock acquires a distributed lock with the given key and TTL.
// Returns a release function that must be called when done.
func (c *Client) Lock(ctx context.Context, key string, ttl time.Duration) (func(), error) {
	lockKey := "lock:" + key

	// SET NX EX — only sets if key doesn't exist
	result, err := c.rdb.SetArgs(ctx, lockKey, "1", redis.SetArgs{
		Mode: "NX",
		TTL:  ttl,
	}).Result()
	ok := result == "OK"
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	if !ok {
		return nil, ErrLockNotAcquired
	}

	release := func() {
		// Best effort release — if it fails, TTL will clean it up
		c.rdb.Del(context.Background(), lockKey)
	}

	return release, nil
}

// LockWithRetry attempts to acquire a lock, retrying with backoff.
func (c *Client) LockWithRetry(ctx context.Context, key string, ttl time.Duration, maxRetries int, retryInterval time.Duration) (func(), error) {
	for i := 0; i <= maxRetries; i++ {
		release, err := c.Lock(ctx, key, ttl)
		if err == nil {
			return release, nil
		}
		if !errors.Is(err, ErrLockNotAcquired) {
			return nil, err
		}

		if i < maxRetries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(retryInterval):
			}
		}
	}

	return nil, ErrLockNotAcquired
}
