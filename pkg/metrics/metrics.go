package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all custom Prometheus metrics for a service.
type Metrics struct {
	// gRPC
	GRPCRequestDuration *prometheus.HistogramVec
	GRPCRequestsTotal   *prometheus.CounterVec

	// Order-specific
	OrdersCreatedTotal *prometheus.CounterVec
	SagaDuration       *prometheus.HistogramVec
	SagaResultTotal    *prometheus.CounterVec

	// Inventory-specific
	StockReservationsTotal *prometheus.CounterVec
	LockWaitDuration       *prometheus.HistogramVec

	// HTTP (for gateway)
	HTTPRequestDuration *prometheus.HistogramVec
	HTTPRequestsTotal   *prometheus.CounterVec
}

// New creates a Metrics instance with all custom metrics registered.
func New(service string) *Metrics {
	return &Metrics{
		GRPCRequestDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "grpc_request_duration_seconds",
			Help:    "Duration of gRPC requests in seconds",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		}, []string{"method", "code"}),

		GRPCRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "grpc_requests_total",
			Help: "Total number of gRPC requests",
		}, []string{"method", "code"}),

		OrdersCreatedTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "orders_created_total",
			Help: "Total number of orders created",
		}, []string{"status"}),

		SagaDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "saga_duration_seconds",
			Help:    "Duration of saga execution in seconds",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"result"}),

		SagaResultTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "saga_result_total",
			Help: "Total saga executions by result",
		}, []string{"result"}),

		StockReservationsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "stock_reservations_total",
			Help: "Total stock reservation attempts",
		}, []string{"result"}),

		LockWaitDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lock_wait_duration_seconds",
			Help:    "Time spent waiting for distributed locks",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5},
		}, []string{"resource"}),

		HTTPRequestDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Duration of HTTP requests",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		}, []string{"method", "path", "status"}),

		HTTPRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests",
		}, []string{"method", "path", "status"}),
	}
}

// Handler returns the Prometheus HTTP handler for scraping.
func Handler() http.Handler {
	return promhttp.Handler()
}
