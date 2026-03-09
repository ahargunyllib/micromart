package grpcutil

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sony/gobreaker/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// DialOptions configures the gRPC client connection.
type DialOptions struct {
	CircuitBreaker *gobreaker.CircuitBreaker[any]
	MaxRetries     int
	RetryBaseDelay time.Duration
	Logger         *slog.Logger
}

func Dial(ctx context.Context, addr string) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return conn, nil
}

// DialWithResilience creates a gRPC client with circuit breaker and retry interceptors.
func DialWithResilience(ctx context.Context, addr string, opts DialOptions) (*grpc.ClientConn, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	interceptors := []grpc.UnaryClientInterceptor{}

	// Retry goes first (outer) — it wraps the circuit breaker
	if opts.MaxRetries > 0 && opts.Logger != nil {
		baseDelay := opts.RetryBaseDelay
		if baseDelay == 0 {
			baseDelay = 100 * time.Millisecond
		}
		interceptors = append(interceptors, RetryInterceptor(opts.MaxRetries, baseDelay, opts.Logger))
	}

	// Circuit breaker goes second (inner)
	if opts.CircuitBreaker != nil && opts.Logger != nil {
		interceptors = append(interceptors, CircuitBreakerInterceptor(opts.CircuitBreaker, opts.Logger))
	}

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	if len(interceptors) > 0 {
		dialOpts = append(dialOpts, grpc.WithChainUnaryInterceptor(interceptors...))
	}

	conn, err := grpc.NewClient(addr, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return conn, nil
}
