package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"
	"github.com/ahargunyllib/micromart/pkg/config"
	"github.com/ahargunyllib/micromart/pkg/grpcutil"
	"github.com/ahargunyllib/micromart/pkg/logger"
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

	inventoryAddr := config.MustGet("INVENTORY_SERVICE_ADDR")
	inventoryConn, err := grpcutil.Dial(context.Background(), inventoryAddr)
	if err != nil {
		log.Error("failed to connect to inventory service", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer inventoryConn.Close()
	log.Info("connected to inventory service", slog.String("addr", inventoryAddr))

	inventoryClient := inventoryv1.NewInventoryServiceClient(inventoryConn)

	repo := NewRepository(db)
	server := NewServer(repo, inventoryClient)

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
