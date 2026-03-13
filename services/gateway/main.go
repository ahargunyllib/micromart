package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"
	"github.com/ahargunyllib/micromart/pkg/config"
	"github.com/ahargunyllib/micromart/pkg/grpcutil"
	"github.com/ahargunyllib/micromart/pkg/logger"
	metricspkg "github.com/ahargunyllib/micromart/pkg/metrics"
	otelpkg "github.com/ahargunyllib/micromart/pkg/otel"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	log := logger.New("gateway")

	// OpenTelemetry
	otlpEndpoint := config.Get("OTLP_ENDPOINT", "localhost:4317")
	_, otelShutdown, err := otelpkg.Init(context.Background(), "gateway", otlpEndpoint)
	if err != nil {
		log.Warn("failed to init otel, tracing disabled", slog.String("error", err.Error()))
		otelShutdown = func() {}
	} else {
		log.Info("opentelemetry initialized", slog.String("endpoint", otlpEndpoint))
	}
	defer otelShutdown()

	// Prometheus metrics
	m := metricspkg.New("order-service")

	port := config.Get("PORT", "8080")

	// gRPC connections
	orderAddr := config.MustGet("ORDER_SERVICE_ADDR")
	orderConn, err := grpcutil.Dial(context.Background(), orderAddr)
	if err != nil {
		log.Error("failed to connect to order service", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer orderConn.Close()
	log.Info("connected to order service", slog.String("addr", orderAddr))

	inventoryAddr := config.MustGet("INVENTORY_SERVICE_ADDR")
	inventoryConn, err := grpcutil.Dial(context.Background(), inventoryAddr)
	if err != nil {
		log.Error("failed to connect to inventory service", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer inventoryConn.Close()
	log.Info("connected to inventory service", slog.String("addr", inventoryAddr))

	orderClient := orderv1.NewOrderServiceClient(orderConn)
	inventoryClient := inventoryv1.NewInventoryServiceClient(inventoryConn)

	productHandler := NewProductHandler(inventoryClient)
	orderHandler := NewOrderHandler(orderClient)

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(TracingMiddleware)
	r.Use(MetricsMiddleware(m))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Handle("/metrics", metricspkg.Handler())

	r.Route("/api/v1", func(r chi.Router) {
		r.Route("/products", func(r chi.Router) {
			r.Post("/", productHandler.Create)
			r.Get("/", productHandler.List)
			r.Get("/search", productHandler.Search)
			r.Get("/{id}", productHandler.Get)
			r.Put("/{id}", productHandler.Update)
		})
		r.Route("/orders", func(r chi.Router) {
			r.Post("/", orderHandler.Create)
			r.Get("/", orderHandler.List)
			r.Get("/{id}", orderHandler.Get)
			r.Post("/{id}/cancel", orderHandler.Cancel)
		})
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", port),
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("gateway starting", slog.String("port", port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("HTTP server error", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down gateway")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("forced shutdown", slog.String("error", err.Error()))
	}
}
