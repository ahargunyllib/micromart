package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/ahargunyllib/micromart/pkg/config"
	"github.com/ahargunyllib/micromart/pkg/grpcutil"
	"github.com/ahargunyllib/micromart/pkg/logger"
	"github.com/jmoiron/sqlx"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	log := logger.New("order-service")

	db, err := sqlx.Connect("pgx", config.MustGet("DATABASE_URL"))
	if err != nil {
		log.Error("failed to connect to database", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer db.Close()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	log.Info("connected to database")

	grpcPort := config.Get("GRPC_PORT", "50051")
	srv := grpcutil.NewServer(log)

	// TODO: Register OrderService server in Phase 2
	// orderv1.RegisterOrderServiceServer(srv, ...)

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
