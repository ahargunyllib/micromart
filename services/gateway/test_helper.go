package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jmoiron/sqlx"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type testEnv struct {
	router      http.Handler
	orderDB     *sqlx.DB
	inventoryDB *sqlx.DB
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()

	orderMigrations, _ := filepath.Abs("../../migrations/order")
	inventoryMigrations, _ := filepath.Abs("../../migrations/inventory")

	orderPG, err := postgres.Run(ctx, "postgres:18.2-alpine",
		postgres.WithDatabase("order_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start order pg: %v", err)
	}
	t.Cleanup(func() { orderPG.Terminate(ctx) })

	inventoryPG, err := postgres.Run(ctx, "postgres:18.2-alpine",
		postgres.WithDatabase("inventory_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start inventory pg: %v", err)
	}
	t.Cleanup(func() { inventoryPG.Terminate(ctx) })

	orderConnStr, _ := orderPG.ConnectionString(ctx, "sslmode=disable")
	inventoryConnStr, _ := inventoryPG.ConnectionString(ctx, "sslmode=disable")

	orderDB, _ := sqlx.Connect("pgx", orderConnStr)
	inventoryDB, _ := sqlx.Connect("pgx", inventoryConnStr)
	t.Cleanup(func() { orderDB.Close(); inventoryDB.Close() })

	runMigrations(t, orderDB, orderMigrations)
	runMigrations(t, inventoryDB, inventoryMigrations)

	inventoryLis, _ := net.Listen("tcp", "localhost:0")
	inventoryGRPC := grpc.NewServer()
	inventoryv1.RegisterInventoryServiceServer(inventoryGRPC, &fullInventoryServer{db: inventoryDB})
	go inventoryGRPC.Serve(inventoryLis)
	t.Cleanup(func() { inventoryGRPC.GracefulStop() })

	inventoryConn, _ := grpc.NewClient(inventoryLis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	t.Cleanup(func() { inventoryConn.Close() })
	inventoryClient := inventoryv1.NewInventoryServiceClient(inventoryConn)

	orderLis, _ := net.Listen("tcp", "localhost:0")
	orderGRPC := grpc.NewServer()
	orderv1.RegisterOrderServiceServer(orderGRPC, &fullOrderServer{
		db:              orderDB,
		inventoryClient: inventoryClient,
	})
	go orderGRPC.Serve(orderLis)
	t.Cleanup(func() { orderGRPC.GracefulStop() })

	orderConn, _ := grpc.NewClient(orderLis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	t.Cleanup(func() { orderConn.Close() })
	orderClient := orderv1.NewOrderServiceClient(orderConn)

	productHandler := NewProductHandler(inventoryClient)
	orderHandler := NewOrderHandler(orderClient)

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(10 * time.Second))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

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

	return &testEnv{
		router:      r,
		orderDB:     orderDB,
		inventoryDB: inventoryDB,
	}
}

func (e *testEnv) doRequest(method, path string, body any) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = &bytes.Buffer{}
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(rec.Body).Decode(&v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return v
}

func runMigrations(t *testing.T, db *sqlx.DB, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".sql" || !containsStr(entry.Name(), ".up.") {
			continue
		}
		content, _ := os.ReadFile(filepath.Join(dir, entry.Name()))
		if _, err := db.Exec(string(content)); err != nil {
			t.Fatalf("migration %s: %v", entry.Name(), err)
		}
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

type fullInventoryServer struct {
	inventoryv1.UnimplementedInventoryServiceServer
	db *sqlx.DB
}

func (s *fullInventoryServer) CreateProduct(ctx context.Context, req *inventoryv1.CreateProductRequest) (*inventoryv1.CreateProductResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	var p struct {
		ID             string    `db:"id"`
		Name           string    `db:"name"`
		Description    string    `db:"description"`
		Category       string    `db:"category"`
		PriceCents     int64     `db:"price_cents"`
		StockAvailable int32     `db:"stock_available"`
		StockReserved  int32     `db:"stock_reserved"`
		Active         bool      `db:"active"`
		CreatedAt      time.Time `db:"created_at"`
		UpdatedAt      time.Time `db:"updated_at"`
	}
	err := s.db.QueryRowxContext(ctx, `
		INSERT INTO products (name, description, category, price_cents, stock_available)
		VALUES ($1, $2, $3, $4, $5) RETURNING *`,
		req.Name, req.Description, req.Category, req.PriceCents, req.InitialStock,
	).StructScan(&p)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return &inventoryv1.CreateProductResponse{Product: &inventoryv1.Product{
		Id: p.ID, Name: p.Name, Description: p.Description, Category: p.Category,
		PriceCents: p.PriceCents, StockAvailable: p.StockAvailable, StockReserved: p.StockReserved,
		Active: p.Active, CreatedAt: timestamppb.New(p.CreatedAt), UpdatedAt: timestamppb.New(p.UpdatedAt),
	}}, nil
}

func (s *fullInventoryServer) GetProduct(ctx context.Context, req *inventoryv1.GetProductRequest) (*inventoryv1.GetProductResponse, error) {
	var p struct {
		ID             string    `db:"id"`
		Name           string    `db:"name"`
		Description    string    `db:"description"`
		Category       string    `db:"category"`
		PriceCents     int64     `db:"price_cents"`
		StockAvailable int32     `db:"stock_available"`
		StockReserved  int32     `db:"stock_reserved"`
		Active         bool      `db:"active"`
		CreatedAt      time.Time `db:"created_at"`
		UpdatedAt      time.Time `db:"updated_at"`
	}
	err := s.db.GetContext(ctx, &p, `SELECT * FROM products WHERE id = $1`, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "product %s not found", req.Id)
	}
	return &inventoryv1.GetProductResponse{Product: &inventoryv1.Product{
		Id: p.ID, Name: p.Name, Description: p.Description, Category: p.Category,
		PriceCents: p.PriceCents, StockAvailable: p.StockAvailable, StockReserved: p.StockReserved,
		Active: p.Active, CreatedAt: timestamppb.New(p.CreatedAt), UpdatedAt: timestamppb.New(p.UpdatedAt),
	}}, nil
}

func (s *fullInventoryServer) ListProducts(ctx context.Context, req *inventoryv1.ListProductsRequest) (*inventoryv1.ListProductsResponse, error) {
	var products []struct {
		ID             string    `db:"id"`
		Name           string    `db:"name"`
		Description    string    `db:"description"`
		Category       string    `db:"category"`
		PriceCents     int64     `db:"price_cents"`
		StockAvailable int32     `db:"stock_available"`
		StockReserved  int32     `db:"stock_reserved"`
		Active         bool      `db:"active"`
		CreatedAt      time.Time `db:"created_at"`
		UpdatedAt      time.Time `db:"updated_at"`
	}
	err := s.db.SelectContext(ctx, &products, `SELECT * FROM products WHERE active = TRUE ORDER BY id LIMIT 100`)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	result := make([]*inventoryv1.Product, len(products))
	for i, p := range products {
		result[i] = &inventoryv1.Product{
			Id: p.ID, Name: p.Name, Description: p.Description, Category: p.Category,
			PriceCents: p.PriceCents, StockAvailable: p.StockAvailable, StockReserved: p.StockReserved,
			Active: p.Active, CreatedAt: timestamppb.New(p.CreatedAt), UpdatedAt: timestamppb.New(p.UpdatedAt),
		}
	}
	return &inventoryv1.ListProductsResponse{Products: result}, nil
}

func (s *fullInventoryServer) SearchProducts(ctx context.Context, req *inventoryv1.SearchProductsRequest) (*inventoryv1.SearchProductsResponse, error) {
	var products []struct {
		ID             string    `db:"id"`
		Name           string    `db:"name"`
		Description    string    `db:"description"`
		Category       string    `db:"category"`
		PriceCents     int64     `db:"price_cents"`
		StockAvailable int32     `db:"stock_available"`
		StockReserved  int32     `db:"stock_reserved"`
		Active         bool      `db:"active"`
		CreatedAt      time.Time `db:"created_at"`
		UpdatedAt      time.Time `db:"updated_at"`
	}
	err := s.db.SelectContext(ctx, &products, `
		SELECT * FROM products
		WHERE to_tsvector('english', name) @@ plainto_tsquery('english', $1) AND active = TRUE
		ORDER BY id LIMIT 100`, req.Query)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	result := make([]*inventoryv1.Product, len(products))
	for i, p := range products {
		result[i] = &inventoryv1.Product{
			Id: p.ID, Name: p.Name, Description: p.Description, Category: p.Category,
			PriceCents: p.PriceCents, StockAvailable: p.StockAvailable, StockReserved: p.StockReserved,
			Active: p.Active, CreatedAt: timestamppb.New(p.CreatedAt), UpdatedAt: timestamppb.New(p.UpdatedAt),
		}
	}
	return &inventoryv1.SearchProductsResponse{Products: result}, nil
}

func (s *fullInventoryServer) UpdateProduct(ctx context.Context, req *inventoryv1.UpdateProductRequest) (*inventoryv1.UpdateProductResponse, error) {
	var p struct {
		ID             string    `db:"id"`
		Name           string    `db:"name"`
		Description    string    `db:"description"`
		Category       string    `db:"category"`
		PriceCents     int64     `db:"price_cents"`
		StockAvailable int32     `db:"stock_available"`
		StockReserved  int32     `db:"stock_reserved"`
		Active         bool      `db:"active"`
		CreatedAt      time.Time `db:"created_at"`
		UpdatedAt      time.Time `db:"updated_at"`
	}
	if req.Name != nil {
		s.db.ExecContext(ctx, `UPDATE products SET name = $1, updated_at = NOW() WHERE id = $2`, *req.Name, req.Id)
	}
	if req.PriceCents != nil {
		s.db.ExecContext(ctx, `UPDATE products SET price_cents = $1, updated_at = NOW() WHERE id = $2`, *req.PriceCents, req.Id)
	}
	if req.Active != nil {
		s.db.ExecContext(ctx, `UPDATE products SET active = $1, updated_at = NOW() WHERE id = $2`, *req.Active, req.Id)
	}
	err := s.db.GetContext(ctx, &p, `SELECT * FROM products WHERE id = $1`, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "not found")
	}
	return &inventoryv1.UpdateProductResponse{Product: &inventoryv1.Product{
		Id: p.ID, Name: p.Name, Description: p.Description, Category: p.Category,
		PriceCents: p.PriceCents, StockAvailable: p.StockAvailable, StockReserved: p.StockReserved,
		Active: p.Active, CreatedAt: timestamppb.New(p.CreatedAt), UpdatedAt: timestamppb.New(p.UpdatedAt),
	}}, nil
}

type fullOrderServer struct {
	orderv1.UnimplementedOrderServiceServer
	db              *sqlx.DB
	inventoryClient inventoryv1.InventoryServiceClient
}

func (s *fullOrderServer) CreateOrder(ctx context.Context, req *orderv1.CreateOrderRequest) (*orderv1.CreateOrderResponse, error) {
	if req.CustomerId == "" {
		return nil, status.Error(codes.InvalidArgument, "customer_id is required")
	}
	if len(req.Items) == 0 {
		return nil, status.Error(codes.InvalidArgument, "items cannot be empty")
	}

	type itemInfo struct {
		productID      string
		quantity       int32
		unitPriceCents int64
		productName    string
	}
	var items []itemInfo
	var totalCents int64

	for _, item := range req.Items {
		if item.Quantity <= 0 {
			return nil, status.Error(codes.InvalidArgument, "quantity must be positive")
		}
		resp, err := s.inventoryClient.GetProduct(ctx, &inventoryv1.GetProductRequest{Id: item.ProductId})
		if err != nil {
			st := status.Convert(err)
			if st.Code() == codes.NotFound {
				return nil, status.Errorf(codes.NotFound, "product %s not found", item.ProductId)
			}
			return nil, status.Errorf(codes.Internal, "get product: %v", err)
		}
		if !resp.Product.Active {
			return nil, status.Errorf(codes.FailedPrecondition, "product %s is not active", item.ProductId)
		}
		items = append(items, itemInfo{
			productID: item.ProductId, quantity: item.Quantity,
			unitPriceCents: resp.Product.PriceCents, productName: resp.Product.Name,
		})
		totalCents += resp.Product.PriceCents * int64(item.Quantity)
	}

	tx, _ := s.db.BeginTxx(ctx, nil)
	defer tx.Rollback()

	// Idempotency check
	if req.IdempotencyKey != "" {
		var existingID string
		err := tx.QueryRowContext(ctx, `SELECT id FROM orders WHERE idempotency_key = $1`, req.IdempotencyKey).Scan(&existingID)
		if err == nil {
			var order struct {
				ID         string    `db:"id"`
				CustomerID string    `db:"customer_id"`
				Status     string    `db:"status"`
				TotalCents int64     `db:"total_cents"`
				CreatedAt  time.Time `db:"created_at"`
				UpdatedAt  time.Time `db:"updated_at"`
			}
			tx.GetContext(ctx, &order, `SELECT id, customer_id, status, total_cents, created_at, updated_at FROM orders WHERE id = $1`, existingID)
			tx.Rollback()
			return &orderv1.CreateOrderResponse{Order: &orderv1.Order{
				Id: order.ID, CustomerId: order.CustomerID, Status: orderv1.OrderStatus_ORDER_STATUS_PENDING,
				TotalCents: order.TotalCents, CreatedAt: timestamppb.New(order.CreatedAt), UpdatedAt: timestamppb.New(order.UpdatedAt),
			}}, nil
		}
	}

	var orderID string
	var createdAt, updatedAt time.Time
	err := tx.QueryRowContext(ctx, `
		INSERT INTO orders (customer_id, status, total_cents, idempotency_key)
		VALUES ($1, 'PENDING', $2, $3)
		RETURNING id, created_at, updated_at`,
		req.CustomerId, totalCents, nilIfEmpty(req.IdempotencyKey),
	).Scan(&orderID, &createdAt, &updatedAt)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "insert order: %v", err)
	}

	protoItems := make([]*orderv1.OrderItem, len(items))
	for i, item := range items {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO order_items (order_id, product_id, quantity, unit_price_cents)
			VALUES ($1, $2, $3, $4)`,
			orderID, item.productID, item.quantity, item.unitPriceCents)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "insert item: %v", err)
		}
		protoItems[i] = &orderv1.OrderItem{
			ProductId: item.productID, ProductName: item.productName,
			Quantity: item.quantity, UnitPriceCents: item.unitPriceCents,
		}
	}

	tx.Commit()

	return &orderv1.CreateOrderResponse{Order: &orderv1.Order{
		Id: orderID, CustomerId: req.CustomerId, Status: orderv1.OrderStatus_ORDER_STATUS_PENDING,
		Items: protoItems, TotalCents: totalCents,
		CreatedAt: timestamppb.New(createdAt), UpdatedAt: timestamppb.New(updatedAt),
	}}, nil
}

func (s *fullOrderServer) GetOrder(ctx context.Context, req *orderv1.GetOrderRequest) (*orderv1.GetOrderResponse, error) {
	var order struct {
		ID         string    `db:"id"`
		CustomerID string    `db:"customer_id"`
		Status     string    `db:"status"`
		TotalCents int64     `db:"total_cents"`
		CreatedAt  time.Time `db:"created_at"`
		UpdatedAt  time.Time `db:"updated_at"`
	}
	err := s.db.GetContext(ctx, &order, `SELECT id, customer_id, status, total_cents, created_at, updated_at FROM orders WHERE id = $1`, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "order not found")
	}

	var dbItems []struct {
		ProductID      string `db:"product_id"`
		Quantity       int32  `db:"quantity"`
		UnitPriceCents int64  `db:"unit_price_cents"`
	}
	s.db.SelectContext(ctx, &dbItems, `SELECT product_id, quantity, unit_price_cents FROM order_items WHERE order_id = $1`, order.ID)

	protoItems := make([]*orderv1.OrderItem, len(dbItems))
	for i, item := range dbItems {
		name := ""
		resp, err := s.inventoryClient.GetProduct(ctx, &inventoryv1.GetProductRequest{Id: item.ProductID})
		if err == nil {
			name = resp.Product.Name
		}
		protoItems[i] = &orderv1.OrderItem{
			ProductId: item.ProductID, ProductName: name,
			Quantity: item.Quantity, UnitPriceCents: item.UnitPriceCents,
		}
	}

	return &orderv1.GetOrderResponse{Order: &orderv1.Order{
		Id: order.ID, CustomerId: order.CustomerID,
		Status: orderv1.OrderStatus_ORDER_STATUS_PENDING, Items: protoItems,
		TotalCents: order.TotalCents,
		CreatedAt:  timestamppb.New(order.CreatedAt), UpdatedAt: timestamppb.New(order.UpdatedAt),
	}}, nil
}

func (s *fullOrderServer) ListOrders(ctx context.Context, req *orderv1.ListOrdersRequest) (*orderv1.ListOrdersResponse, error) {
	var orders []struct {
		ID         string    `db:"id"`
		CustomerID string    `db:"customer_id"`
		Status     string    `db:"status"`
		TotalCents int64     `db:"total_cents"`
		CreatedAt  time.Time `db:"created_at"`
		UpdatedAt  time.Time `db:"updated_at"`
	}
	s.db.SelectContext(ctx, &orders, `
		SELECT id, customer_id, status, total_cents, created_at, updated_at
		FROM orders WHERE customer_id = $1 ORDER BY id LIMIT 100`, req.CustomerId)

	result := make([]*orderv1.Order, len(orders))
	for i, o := range orders {
		result[i] = &orderv1.Order{
			Id: o.ID, CustomerId: o.CustomerID, Status: orderv1.OrderStatus_ORDER_STATUS_PENDING,
			TotalCents: o.TotalCents,
			CreatedAt:  timestamppb.New(o.CreatedAt), UpdatedAt: timestamppb.New(o.UpdatedAt),
		}
	}
	return &orderv1.ListOrdersResponse{Orders: result}, nil
}

func (s *fullOrderServer) CancelOrder(ctx context.Context, req *orderv1.CancelOrderRequest) (*orderv1.CancelOrderResponse, error) {
	var order struct {
		ID         string    `db:"id"`
		CustomerID string    `db:"customer_id"`
		Status     string    `db:"status"`
		TotalCents int64     `db:"total_cents"`
		CreatedAt  time.Time `db:"created_at"`
		UpdatedAt  time.Time `db:"updated_at"`
	}
	err := s.db.GetContext(ctx, &order, `SELECT id, customer_id, status, total_cents, created_at, updated_at FROM orders WHERE id = $1`, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "order not found")
	}
	s.db.ExecContext(ctx, `UPDATE orders SET status = 'CANCELLED', updated_at = NOW() WHERE id = $1`, req.Id)
	return &orderv1.CancelOrderResponse{Order: &orderv1.Order{
		Id: order.ID, CustomerId: order.CustomerID, Status: orderv1.OrderStatus_ORDER_STATUS_CANCELLED,
		TotalCents: order.TotalCents,
		CreatedAt:  timestamppb.New(order.CreatedAt), UpdatedAt: timestamppb.New(order.UpdatedAt),
	}}, nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
