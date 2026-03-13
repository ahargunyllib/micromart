package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	metricspkg "github.com/ahargunyllib/micromart/pkg/metrics"
)

// TracingMiddleware creates a span for each HTTP request.
func TracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tracer := otel.Tracer("gateway")
		ctx, span := tracer.Start(r.Context(), fmt.Sprintf("%s %s", r.Method, r.URL.Path),
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.path", r.URL.Path),
			),
		)
		defer span.End()

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// MetricsMiddleware records HTTP request duration and count.
func MetricsMiddleware(m *metricspkg.Metrics) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()
            ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
            next.ServeHTTP(ww, r)

            status := fmt.Sprintf("%d", ww.Status())
            m.HTTPRequestDuration.WithLabelValues(r.Method, r.URL.Path, status).Observe(time.Since(start).Seconds())
            m.HTTPRequestsTotal.WithLabelValues(r.Method, r.URL.Path, status).Inc()
        })
    }
}
