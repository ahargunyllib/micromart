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
	"time"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"
	"github.com/ahargunyllib/micromart/pkg/config"
	"github.com/ahargunyllib/micromart/pkg/grpcutil"
	"github.com/ahargunyllib/micromart/pkg/logger"
	metricspkg "github.com/ahargunyllib/micromart/pkg/metrics"
	otelpkg "github.com/ahargunyllib/micromart/pkg/otel"
	redispkg "github.com/ahargunyllib/micromart/pkg/redis"
	"github.com/jmoiron/sqlx"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	log := logger.New("order-service")

	// OpenTelemetry
	otlpEndpoint := config.Get("OTLP_ENDPOINT", "localhost:4317")
	_, otelShutdown, err := otelpkg.Init(context.Background(), "order-service", otlpEndpoint)
	if err != nil {
		log.Warn("failed to init otel, tracing disabled", slog.String("error", err.Error()))
		otelShutdown = func() {}
	} else {
		log.Info("opentelemetry initialized", slog.String("endpoint", otlpEndpoint))
	}
	defer otelShutdown()

	// Prometheus metrics
	m := metricspkg.New("order-service")

	// Start metrics HTTP server
	metricsPort := config.Get("METRICS_PORT", "9091")
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metricspkg.Handler())
		log.Info("metrics server starting", slog.String("port", metricsPort))
		if err := http.ListenAndServe(fmt.Sprintf(":%s", metricsPort), mux); err != nil {
			log.Error("metrics server error", slog.String("error", err.Error()))
		}
	}()

	// Database
	db, err := sqlx.Connect("pgx", config.MustGet("DATABASE_URL"))
	if err != nil {
		log.Error("failed to connect to database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer db.Close()
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	log.Info("connected to database")

	// Redis
	redisClient, err := redispkg.NewClient(config.MustGet("REDIS_ADDR"))
	if err != nil {
		log.Error("failed to connect to redis", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer redisClient.Close()
	log.Info("connected to redis")

	// ClickHouse (optional — don't fail if unavailable)
	var chClient *ClickHouseClient
	chAddr := config.Get("CLICKHOUSE_ADDR", "")
	if chAddr != "" {
		chClient, err = NewClickHouseClient(
			chAddr,
			config.Get("CLICKHOUSE_DATABASE", "default"),
			config.Get("CLICKHOUSE_USER", "micromart"),
			config.Get("CLICKHOUSE_PASSWORD", "micromart"),
			log,
		)
		if err != nil {
			log.Warn("clickhouse unavailable, analytics disabled", slog.String("error", err.Error()))
		} else {
			if err := chClient.CreateTables(context.Background()); err != nil {
				log.Warn("clickhouse table creation failed", slog.String("error", err.Error()))
			}
			chClient.StartConsumer(100, 5*time.Second)
			defer chClient.Close()
			log.Info("clickhouse connected", slog.String("addr", chAddr))
		}
	}

	// Inventory Service client with resilience
	inventoryAddr := config.MustGet("INVENTORY_SERVICE_ADDR")
	cb := grpcutil.NewCircuitBreaker("inventory-service", log)
	inventoryConn, err := grpcutil.DialWithResilience(context.Background(), inventoryAddr, grpcutil.DialOptions{
		CircuitBreaker: cb,
		MaxRetries:     3,
		RetryBaseDelay: 100 * time.Millisecond,
		Logger:         log,
	})
	if err != nil {
		log.Error("failed to connect to inventory service", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer inventoryConn.Close()
	log.Info("connected to inventory service", slog.String("addr", inventoryAddr))

	inventoryClient := inventoryv1.NewInventoryServiceClient(inventoryConn)

	// Saga + Server
	repo := NewRepository(db)
	saga := NewSagaOrchestrator(db, inventoryClient, log, m, chClient)

	// Resume any interrupted sagas from previous crash
	if err := saga.Resume(context.Background()); err != nil {
		log.Error("failed to resume sagas", slog.String("error", err.Error()))
	}

	// gRPC server
	server := NewServer(repo, inventoryClient, saga, redisClient, m)
	grpcPort := config.Get("GRPC_PORT", "50051")
	srv := grpcutil.NewServerWithMetrics(log, m)
	orderv1.RegisterOrderServiceServer(srv, server)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", grpcPort))
	if err != nil {
		log.Error("failed to listen", slog.String("error", err.Error()))
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("order service starting", slog.String("port", grpcPort))
		if err := srv.Serve(lis); err != nil {
			log.Error("gRPC server error", slog.String("error", err.Error()))
		}
	}()

	<-ctx.Done()
	log.Info("shutting down order service")
	srv.GracefulStop()
}
