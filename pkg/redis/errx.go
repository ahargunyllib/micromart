package redis

import "errors"

var (
	ErrLockNotAcquired = errors.New("lock not acquired")
)
