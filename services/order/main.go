package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"
	"github.com/ahargunyllib/micromart/pkg/config"
	"github.com/ahargunyllib/micromart/pkg/grpcutil"
	"github.com/ahargunyllib/micromart/pkg/logger"
	redispkg "github.com/ahargunyllib/micromart/pkg/redis"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

func main() {
	log := logger.New("order-service")

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

	// Inventory Service client with circuit breaker and retry
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

	// Saga orchestrator
	repo := NewRepository(db)
	saga := NewSagaOrchestrator(db, inventoryClient, log)

	// Resume any interrupted sagas from previous crash
	if err := saga.Resume(context.Background()); err != nil {
		log.Error("failed to resume sagas", slog.String("error", err.Error()))
	}

	// gRPC server
	server := NewServer(repo, inventoryClient, saga, redisClient)
	grpcPort := config.Get("GRPC_PORT", "50051")
	srv := grpcutil.NewServer(log)
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
