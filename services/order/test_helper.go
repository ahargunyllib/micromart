package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"
	redispkg "github.com/ahargunyllib/micromart/pkg/redis"
	"github.com/jmoiron/sqlx"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type testEnv struct {
	orderClient     orderv1.OrderServiceClient
	inventoryClient inventoryv1.InventoryServiceClient
	orderDB         *sqlx.DB
	inventoryDB     *sqlx.DB
	redis           *redispkg.Client
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()

	orderMigrationsDir, _ := filepath.Abs("../../migrations/order")
	inventoryMigrationsDir, _ := filepath.Abs("../../migrations/inventory")

	// Start two Postgres containers
	orderPG, err := postgres.Run(ctx,
		"postgres:18.2-alpine",
		postgres.WithDatabase("order_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start order postgres: %v", err)
	}
	t.Cleanup(func() { orderPG.Terminate(ctx) })

	inventoryPG, err := postgres.Run(ctx,
		"postgres:18.2-alpine",
		postgres.WithDatabase("inventory_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start inventory postgres: %v", err)
	}
	t.Cleanup(func() { inventoryPG.Terminate(ctx) })

	// Start Redis container
	redisContainer, err := redis.Run(ctx,
		"redis:8.0-alpine",
		redis.WithSnapshotting(10, 1),
		redis.WithLogLevel(redis.LogLevelVerbose),
	)
	if err != nil {
		t.Fatalf("start redis: %v", err)
	}
	t.Cleanup(func() { redisContainer.Terminate(ctx) })

	// Connect to databases
	orderConnStr, _ := orderPG.ConnectionString(ctx, "sslmode=disable")
	inventoryConnStr, _ := inventoryPG.ConnectionString(ctx, "sslmode=disable")

	orderDB, err := sqlx.Connect("pgx", orderConnStr)
	if err != nil {
		t.Fatalf("connect order db: %v", err)
	}
	t.Cleanup(func() { orderDB.Close() })

	inventoryDB, err := sqlx.Connect("pgx", inventoryConnStr)
	if err != nil {
		t.Fatalf("connect inventory db: %v", err)
	}
	t.Cleanup(func() { inventoryDB.Close() })

	// Connect to Redis
	redisAddr, _ := redisContainer.Endpoint(ctx, "")
	redisClient, err := redispkg.NewClient(redisAddr)
	if err != nil {
		t.Fatalf("connect redis: %v", err)
	}
	t.Cleanup(func() { redisClient.Close() })

	// Run migrations
	runMigrations(t, orderDB, orderMigrationsDir)
	runMigrations(t, inventoryDB, inventoryMigrationsDir)

	// Add stock_reservations table for test inventory service
	_, err = inventoryDB.Exec(`
		CREATE TABLE IF NOT EXISTS stock_reservations (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			order_id VARCHAR(255) NOT NULL UNIQUE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS reservation_items (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			reservation_id UUID NOT NULL REFERENCES stock_reservations(id) ON DELETE CASCADE,
			product_id UUID NOT NULL,
			quantity INT NOT NULL CHECK (quantity > 0),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_reservation_items_reservation_id ON reservation_items(reservation_id);
	`)
	if err != nil {
		t.Fatalf("create stock_reservations table: %v", err)
	}

	// Start a real Inventory Service gRPC server
	inventoryRepo := newInventoryRepository(inventoryDB)
	inventoryServer := newInventoryServer(inventoryRepo)

	inventoryLis, _ := net.Listen("tcp", "localhost:0")
	inventoryGRPC := grpc.NewServer()
	inventoryv1.RegisterInventoryServiceServer(inventoryGRPC, inventoryServer)
	go inventoryGRPC.Serve(inventoryLis)
	t.Cleanup(func() { inventoryGRPC.GracefulStop() })

	// Create inventory client for the Order Service
	inventoryConn, err := grpc.NewClient(inventoryLis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial inventory: %v", err)
	}
	t.Cleanup(func() { inventoryConn.Close() })

	inventoryClient := inventoryv1.NewInventoryServiceClient(inventoryConn)

	// Start Order Service gRPC server
	orderRepo := NewRepository(orderDB)

	// Create saga orchestrator (use no-op logger for tests)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	saga := NewSagaOrchestrator(orderDB, inventoryClient, log)

	orderServer := NewServer(orderRepo, inventoryClient, saga, redisClient)

	orderLis, _ := net.Listen("tcp", "localhost:0")
	orderGRPC := grpc.NewServer()
	orderv1.RegisterOrderServiceServer(orderGRPC, orderServer)
	go orderGRPC.Serve(orderLis)
	t.Cleanup(func() { orderGRPC.GracefulStop() })

	// Create order client for tests
	orderConn, err := grpc.NewClient(orderLis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial order: %v", err)
	}
	t.Cleanup(func() { orderConn.Close() })

	orderClient := orderv1.NewOrderServiceClient(orderConn)

	return &testEnv{
		orderClient:     orderClient,
		inventoryClient: inventoryClient,
		orderDB:         orderDB,
		inventoryDB:     inventoryDB,
		redis:           redisClient,
	}
}

func runMigrations(t *testing.T, db *sqlx.DB, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations dir %s: %v", dir, err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		if !containsStr(entry.Name(), ".up.") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read migration %s: %v", entry.Name(), err)
		}
		if _, err := db.Exec(string(content)); err != nil {
			t.Fatalf("execute migration %s: %v", entry.Name(), err)
		}
	}
}

type inventoryRepository struct {
	db *sqlx.DB
}

func newInventoryRepository(db *sqlx.DB) *inventoryRepository {
	return &inventoryRepository{db: db}
}

type inventoryServer struct {
	inventoryv1.UnimplementedInventoryServiceServer
	repo *inventoryRepository
}

func newInventoryServer(repo *inventoryRepository) *inventoryServer {
	return &inventoryServer{repo: repo}
}

func (s *inventoryServer) CreateProduct(ctx context.Context, req *inventoryv1.CreateProductRequest) (*inventoryv1.CreateProductResponse, error) {
	var id, name, description, category string
	var priceCents int64
	var stockAvailable, stockReserved int32
	var active bool
	var createdAt, updatedAt time.Time

	err := s.repo.db.QueryRowContext(ctx, `
		INSERT INTO products (name, description, category, price_cents, stock_available)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, description, category, price_cents, stock_available, stock_reserved, active, created_at, updated_at`,
		req.Name, req.Description, req.Category, req.PriceCents, req.InitialStock,
	).Scan(&id, &name, &description, &category, &priceCents, &stockAvailable, &stockReserved, &active, &createdAt, &updatedAt)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create product: %v", err)
	}

	return &inventoryv1.CreateProductResponse{
		Product: &inventoryv1.Product{
			Id: id, Name: name, Description: description, Category: category,
			PriceCents: priceCents, StockAvailable: stockAvailable, StockReserved: stockReserved,
			Active: active,
		},
	}, nil
}

func (s *inventoryServer) GetProduct(ctx context.Context, req *inventoryv1.GetProductRequest) (*inventoryv1.GetProductResponse, error) {
	var id, name, description, category string
	var priceCents int64
	var stockAvailable, stockReserved int32
	var active bool

	err := s.repo.db.QueryRowContext(ctx, `
		SELECT id, name, description, category, price_cents, stock_available, stock_reserved, active
		FROM products WHERE id = $1`, req.Id,
	).Scan(&id, &name, &description, &category, &priceCents, &stockAvailable, &stockReserved, &active)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "product %s not found", req.Id)
	}

	return &inventoryv1.GetProductResponse{
		Product: &inventoryv1.Product{
			Id: id, Name: name, Description: description, Category: category,
			PriceCents: priceCents, StockAvailable: stockAvailable, StockReserved: stockReserved,
			Active: active,
		},
	}, nil
}

func (s *inventoryServer) ReserveStock(ctx context.Context, req *inventoryv1.ReserveStockRequest) (*inventoryv1.ReserveStockResponse, error) {
	// Start transaction
	tx, err := s.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "begin tx: %v", err)
	}
	defer tx.Rollback()

	// Reserve each item
	for _, item := range req.Items {
		// Check stock availability
		var available int32
		err = tx.QueryRowContext(ctx, `
			SELECT stock_available FROM products WHERE id = $1 FOR UPDATE`,
			item.ProductId,
		).Scan(&available)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "product %s not found", item.ProductId)
		}

		if available < item.Quantity {
			return nil, status.Errorf(codes.FailedPrecondition, "insufficient stock for product %s", item.ProductId)
		}

		// Update stock
		_, err = tx.ExecContext(ctx, `
			UPDATE products SET stock_available = stock_available - $1, stock_reserved = stock_reserved + $1
			WHERE id = $2`,
			item.Quantity, item.ProductId,
		)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "reserve stock: %v", err)
		}

		// Insert reservation record - matches actual inventory service schema
		_, err = tx.ExecContext(ctx, `
			INSERT INTO stock_reservations (order_id, product_id, quantity, status)
			VALUES ($1, $2, $3, $4)`,
			req.OrderId, item.ProductId, item.Quantity, "RESERVED",
		)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "create reservation: %v", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, status.Errorf(codes.Internal, "commit tx: %v", err)
	}

	// Return order_id as reservation_id (matches actual inventory service behavior)
	return &inventoryv1.ReserveStockResponse{ReservationId: req.OrderId}, nil
}

func (s *inventoryServer) ReleaseStock(ctx context.Context, req *inventoryv1.ReleaseStockRequest) (*inventoryv1.ReleaseStockResponse, error) {
	// Start transaction
	tx, err := s.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "begin tx: %v", err)
	}
	defer tx.Rollback()

	// Get reservations for this order (reservation_id is order_id)
	type resItem struct {
		ProductID string `db:"product_id"`
		Quantity  int32  `db:"quantity"`
	}
	var items []resItem
	rows, err := tx.QueryContext(ctx, `
		SELECT product_id, quantity FROM stock_reservations
		WHERE order_id = $1 AND status = 'RESERVED'`,
		req.ReservationId,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "query reservations: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item resItem
		if err := rows.Scan(&item.ProductID, &item.Quantity); err != nil {
			return nil, status.Errorf(codes.Internal, "scan reservation: %v", err)
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, status.Errorf(codes.NotFound, "reservation %s not found", req.ReservationId)
	}

	// Release each item - restore to available and decrement reserved
	for _, item := range items {
		_, err = tx.ExecContext(ctx, `
			UPDATE products SET stock_available = stock_available + $1, stock_reserved = stock_reserved - $1
			WHERE id = $2`,
			item.Quantity, item.ProductID,
		)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "release stock: %v", err)
		}
	}

	// Update reservation status to RELEASED
	_, err = tx.ExecContext(ctx, `
		UPDATE stock_reservations SET status = 'RELEASED'
		WHERE order_id = $1 AND status = 'RESERVED'`,
		req.ReservationId,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "update reservation status: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, status.Errorf(codes.Internal, "commit tx: %v", err)
	}

	return &inventoryv1.ReleaseStockResponse{}, nil
}

func (s *inventoryServer) DecrementStock(ctx context.Context, req *inventoryv1.DecrementStockRequest) (*inventoryv1.DecrementStockResponse, error) {
	// Start transaction
	tx, err := s.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "begin tx: %v", err)
	}
	defer tx.Rollback()

	// Get reservations for this order (reservation_id is order_id)
	type resItem struct {
		ProductID string `db:"product_id"`
		Quantity  int32  `db:"quantity"`
	}
	var items []resItem
	rows, err := tx.QueryContext(ctx, `
		SELECT product_id, quantity FROM stock_reservations
		WHERE order_id = $1 AND status = 'RESERVED'`,
		req.ReservationId,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "query reservations: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item resItem
		if err := rows.Scan(&item.ProductID, &item.Quantity); err != nil {
			return nil, status.Errorf(codes.Internal, "scan reservation: %v", err)
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, status.Errorf(codes.NotFound, "reservation %s not found", req.ReservationId)
	}

	// Decrement reserved stock only (available was already decremented during reservation)
	for _, item := range items {
		_, err = tx.ExecContext(ctx, `
			UPDATE products
			SET stock_reserved = stock_reserved - $1
			WHERE id = $2`,
			item.Quantity, item.ProductID,
		)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "decrement stock: %v", err)
		}
	}

	// Update reservation status to DECREMENTED
	_, err = tx.ExecContext(ctx, `
		UPDATE stock_reservations SET status = 'DECREMENTED'
		WHERE order_id = $1 AND status = 'RESERVED'`,
		req.ReservationId,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "update reservation status: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, status.Errorf(codes.Internal, "commit tx: %v", err)
	}

	return &inventoryv1.DecrementStockResponse{}, nil
}

// --- Helper to create a product via inventory service ---

func createTestProduct(t *testing.T, env *testEnv, name string, priceCents int64, stock int32) *inventoryv1.Product {
	t.Helper()
	resp, err := env.inventoryClient.CreateProduct(context.Background(), &inventoryv1.CreateProductRequest{
		Name:         name,
		Category:     "test",
		PriceCents:   priceCents,
		InitialStock: stock,
	})
	if err != nil {
		t.Fatalf("create test product: %v", err)
	}
	return resp.Product
}

// -- - Helper to get product details via inventory service ---
func getProduct(t *testing.T, env *testEnv, id string) *inventoryv1.Product {
	t.Helper()
	resp, err := env.inventoryClient.GetProduct(context.Background(), &inventoryv1.GetProductRequest{Id: id})
	if err != nil {
		t.Fatalf("get product: %v", err)
	}
	return resp.Product
}

// --- Tests ---

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
