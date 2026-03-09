package grpcutil

import (
	"context"
	"log/slog"

	"github.com/sony/gobreaker/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CircuitBreakerInterceptor wraps gRPC unary client calls with a circuit breaker.
func CircuitBreakerInterceptor(cb *gobreaker.CircuitBreaker[any], log *slog.Logger) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		_, err := cb.Execute(func() (any, error) {
			err := invoker(ctx, method, req, reply, cc, opts...)
			if err != nil {
				// Only count transient errors as failures
				code := status.Code(err)
				if isTransient(code) {
					return nil, err
				}
				// Non-transient errors (NotFound, InvalidArgument, etc.)
				// should not trip the circuit breaker
				return nil, &nonTransientError{err: err}
			}
			return nil, nil
		})

		if err != nil {
			// Unwrap non-transient errors
			var nte *nonTransientError
			if ok := isNonTransientError(err, &nte); ok {
				return nte.err
			}

			// Circuit breaker is open
			if err == gobreaker.ErrOpenState || err == gobreaker.ErrTooManyRequests {
				log.Warn("circuit breaker open",
					slog.String("method", method),
					slog.String("state", "open"),
				)
				return status.Errorf(codes.Unavailable, "service unavailable: circuit breaker open")
			}

			return err
		}

		return nil
	}
}

// NewCircuitBreaker creates a circuit breaker with sensible defaults.
func NewCircuitBreaker(name string, log *slog.Logger) *gobreaker.CircuitBreaker[any] {
	return gobreaker.NewCircuitBreaker[any](gobreaker.Settings{
		Name:        name,
		MaxRequests: 3,  // half-open: allow 3 requests to test recovery
		Interval:    0,  // don't clear counts in closed state
		Timeout:     10, // 10 seconds in open state before trying half-open
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// Trip after 5 consecutive failures
			return counts.ConsecutiveFailures >= 5
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			log.Warn("circuit breaker state change",
				slog.String("name", name),
				slog.String("from", from.String()),
				slog.String("to", to.String()),
			)
		},
	})
}

// isTransient returns true for gRPC codes that represent transient failures.
func isTransient(code codes.Code) bool {
	switch code {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted:
		return true
	default:
		return false
	}
}

// nonTransientError wraps errors that should pass through the circuit breaker.
type nonTransientError struct {
	err error
}

func (e *nonTransientError) Error() string {
	return e.err.Error()
}

func isNonTransientError(err error, target **nonTransientError) bool {
	nte, ok := err.(*nonTransientError)
	if ok {
		*target = nte
	}
	return ok
}
