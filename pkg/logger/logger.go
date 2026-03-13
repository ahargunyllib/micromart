package logger

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

// New creates a structured JSON logger with the given service name.
func New(service string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	return slog.New(handler).With(slog.String("service", service))
}

// WithTraceID returns a logger with the trace ID from the context.
func WithTraceID(log *slog.Logger, ctx context.Context) *slog.Logger {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().HasTraceID() {
		return log.With(slog.String("trace_id", span.SpanContext().TraceID().String()))
	}
	return log
}
