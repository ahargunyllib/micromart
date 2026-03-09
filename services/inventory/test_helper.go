package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type testEnv struct {
	client inventoryv1.InventoryServiceClient
	db     *sqlx.DB
	conn   *grpc.ClientConn
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()

	migrationsDir, err := filepath.Abs("../../migrations/inventory")
	if err != nil {
		t.Fatalf("resolve migrations path: %v", err)
	}

	pgContainer, err := postgres.Run(ctx,
		"postgres:18.2-alpine",
		postgres.WithDatabase("inventory_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.WithInitScripts(),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { pgContainer.Terminate(ctx) })

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get connection string: %v", err)
	}

	db, err := sqlx.Connect("pgx", connStr)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	runMigrations(t, db, migrationsDir)

	repo := NewRepository(db)
	server := NewServer(repo)

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	inventoryv1.RegisterInventoryServiceServer(grpcServer, server)

	go grpcServer.Serve(lis)
	t.Cleanup(func() { grpcServer.GracefulStop() })

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial grpc: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	client := inventoryv1.NewInventoryServiceClient(conn)

	return &testEnv{
		client: client,
		db:     db,
		conn:   conn,
	}
}

func runMigrations(t *testing.T, db *sqlx.DB, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}

	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		if !contains(entry.Name(), ".up.") {
			continue
		}

		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read migration %s: %v", entry.Name(), err)
		}

		_, err = db.Exec(string(content))
		if err != nil {
			t.Fatalf("execute migration %s: %v", entry.Name(), err)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
