package grpcutil

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	metrics "github.com/ahargunyllib/micromart/pkg/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LoggingUnaryInterceptor logs gRPC unary calls with duration, status, and trace ID.
func LoggingUnaryInterceptor(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start)
		code := status.Code(err)

		attrs := []any{
			slog.String("method", info.FullMethod),
			slog.Duration("duration", duration),
			slog.String("code", code.String()),
		}

		// Add trace ID if available
		span := trace.SpanFromContext(ctx)
		if span.SpanContext().HasTraceID() {
			attrs = append(attrs, slog.String("trace_id", span.SpanContext().TraceID().String()))
		}

		if err != nil {
			log.Error("gRPC call failed", append(attrs, slog.String("error", err.Error()))...)
		} else {
			log.Info("gRPC call completed", attrs...)
		}

		return resp, err
	}
}

// RecoveryUnaryInterceptor recovers from panics in gRPC handlers.
func RecoveryUnaryInterceptor(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				log.Error("gRPC handler panic",
					slog.Any("panic", r),
					slog.String("method", info.FullMethod),
					slog.String("stack", string(debug.Stack())),
				)
				err = status.Errorf(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

// TracingUnaryInterceptor creates spans for incoming gRPC requests.
func TracingUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		tracer := otel.Tracer("grpc-server")
		ctx, span := tracer.Start(ctx, info.FullMethod,
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		resp, err := handler(ctx, req)
		if err != nil {
			span.SetStatus(otelcodes.Error, err.Error())
			span.SetAttributes(attribute.String("grpc.code", status.Code(err).String()))
		} else {
			span.SetStatus(otelcodes.Ok, "")
			span.SetAttributes(attribute.String("grpc.code", "OK"))
		}

		return resp, err
	}
}

// MetricsUnaryInterceptor records Prometheus metrics for gRPC calls.
func MetricsUnaryInterceptor(m *metrics.Metrics) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start).Seconds()
		code := status.Code(err).String()

		m.GRPCRequestDuration.WithLabelValues(info.FullMethod, code).Observe(duration)
		m.GRPCRequestsTotal.WithLabelValues(info.FullMethod, code).Inc()

		return resp, err
	}
}

// NewServer creates a gRPC server with standard interceptors.
func NewServer(log *slog.Logger) *grpc.Server {
	return grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			RecoveryUnaryInterceptor(log),
			TracingUnaryInterceptor(),
			LoggingUnaryInterceptor(log),
		),
	)
}

// NewServerWithMetrics creates a gRPC server with tracing, metrics, logging, and recovery.
func NewServerWithMetrics(log *slog.Logger, m *metrics.Metrics) *grpc.Server {
	return grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			RecoveryUnaryInterceptor(log),
			TracingUnaryInterceptor(),
			MetricsUnaryInterceptor(m),
			LoggingUnaryInterceptor(log),
		),
	)
}
