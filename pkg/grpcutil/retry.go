package grpcutil

import (
	"context"
	"log/slog"
	"math"
	"math/rand"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// RetryInterceptor retries transient gRPC failures with exponential backoff and jitter.
func RetryInterceptor(maxRetries int, baseDelay time.Duration, log *slog.Logger) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		var lastErr error

		for attempt := 0; attempt <= maxRetries; attempt++ {
			lastErr = invoker(ctx, method, req, reply, cc, opts...)
			if lastErr == nil {
				return nil
			}

			code := status.Code(lastErr)
			if !isTransient(code) {
				// Non-transient error — don't retry
				return lastErr
			}

			if attempt < maxRetries {
				delay := backoffWithJitter(baseDelay, attempt)

				log.Warn("retrying gRPC call",
					slog.String("method", method),
					slog.Int("attempt", attempt+1),
					slog.Int("max_retries", maxRetries),
					slog.Duration("delay", delay),
					slog.String("error", lastErr.Error()),
				)

				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
				}
			}
		}

		return lastErr
	}
}

// backoffWithJitter calculates exponential backoff with full jitter.
// delay = random(0, baseDelay * 2^attempt)
func backoffWithJitter(baseDelay time.Duration, attempt int) time.Duration {
	maxDelay := float64(baseDelay) * math.Pow(2, float64(attempt))
	// Cap at 10 seconds
	if maxDelay > float64(10*time.Second) {
		maxDelay = float64(10 * time.Second)
	}
	return time.Duration(rand.Float64() * maxDelay)
}
