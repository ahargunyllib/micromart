package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	"github.com/ahargunyllib/micromart/pkg/config"
	"github.com/ahargunyllib/micromart/pkg/grpcutil"
	"github.com/ahargunyllib/micromart/pkg/logger"
	metricspkg "github.com/ahargunyllib/micromart/pkg/metrics"
	otelpkg "github.com/ahargunyllib/micromart/pkg/otel"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

func main() {
	log := logger.New("inventory-service")

	// OpenTelemetry
	otlpEndpoint := config.Get("OTLP_ENDPOINT", "localhost:4317")
	_, otelShutdown, err := otelpkg.Init(context.Background(), "inventory-service", otlpEndpoint)
	if err != nil {
		log.Warn("failed to init otel, tracing disabled", slog.String("error", err.Error()))
		otelShutdown = func() {}
	} else {
		log.Info("opentelemetry initialized", slog.String("endpoint", otlpEndpoint))
	}
	defer otelShutdown()

	// Prometheus metrics
	m := metricspkg.New("inventory-service")

	// Start metrics HTTP server
	metricsPort := config.Get("METRICS_PORT", "9092")
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metricspkg.Handler())
		log.Info("metrics server starting", slog.String("port", metricsPort))
		if err := http.ListenAndServe(fmt.Sprintf(":%s", metricsPort), mux); err != nil {
			log.Error("metrics server error", slog.String("error", err.Error()))
		}
	}()

	db, err := sqlx.Connect("pgx", config.MustGet("DATABASE_URL"))
	if err != nil {
		log.Error("failed to connect to database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer db.Close()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	log.Info("connected to database")

	repo := NewRepository(db)
	server := NewServer(repo, m)

	grpcPort := config.Get("GRPC_PORT", "50052")
	srv := grpcutil.NewServerWithMetrics(log, m)
	inventoryv1.RegisterInventoryServiceServer(srv, server)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", grpcPort))
	if err != nil {
		log.Error("failed to listen", slog.String("error", err.Error()))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("inventory service starting", slog.String("port", grpcPort))
		if err := srv.Serve(lis); err != nil {
			log.Error("gRPC server error", slog.String("error", err.Error()))
		}
	}()

	<-ctx.Done()
	log.Info("shutting down inventory service")
	srv.GracefulStop()
}
