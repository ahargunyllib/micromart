package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestSagaSuccess validates the happy path of saga execution
func TestSagaSuccess(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create test product
	product := createTestProduct(t, env, "Test Product", 1000, 10)

	// Create an order to run saga against
	orderResp, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "customer-1",
		Items:      []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	// Verify order status is COMPLETED
	if orderResp.Order.Status != orderv1.OrderStatus_ORDER_STATUS_COMPLETED {
		t.Errorf("expected order status %v, got %v", orderv1.OrderStatus_ORDER_STATUS_COMPLETED, orderResp.Order.Status)
	}

	// Verify saga state is COMPLETED
	var saga SagaState
	err = env.orderDB.Get(&saga, `SELECT * FROM saga_state WHERE order_id = $1`, orderResp.Order.Id)
	if err != nil {
		t.Fatalf("query saga state: %v", err)
	}
	if saga.Status != SagaStatusCompleted {
		t.Errorf("expected saga status %s, got %s", SagaStatusCompleted, saga.Status)
	}
	if saga.CurrentStep != string(StepConfirmOrder) {
		t.Errorf("expected current step %s, got %s", StepConfirmOrder, saga.CurrentStep)
	}

	// Verify stock was decremented correctly
	updatedProduct := getProduct(t, env, product.Id)
	expectedStock := product.StockAvailable - 2
	if updatedProduct.StockAvailable != expectedStock {
		t.Errorf("expected stock %d, got %d", expectedStock, updatedProduct.StockAvailable)
	}
	// Reserved should be 0 (reserved then decremented)
	if updatedProduct.StockReserved != 0 {
		t.Errorf("expected reserved stock 0, got %d", updatedProduct.StockReserved)
	}
}

// TestSagaFailure_InsufficientStock tests saga failure during inventory reservation
func TestSagaFailure_InsufficientStock(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create product with limited stock
	product := createTestProduct(t, env, "Limited Product", 1000, 2)

	// Try to order more than available stock
	orderResp, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "customer-1",
		Items:      []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: 5}},
	})

	// CreateOrder should succeed (creates order in DB), but saga should fail
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	// Verify order status is FAILED
	if orderResp.Order.Status != orderv1.OrderStatus_ORDER_STATUS_FAILED {
		t.Errorf("expected order status %v, got %v", orderv1.OrderStatus_ORDER_STATUS_FAILED, orderResp.Order.Status)
	}

	// Verify saga state is FAILED
	var saga SagaState
	err = env.orderDB.Get(&saga, `SELECT * FROM saga_state WHERE order_id = $1`, orderResp.Order.Id)
	if err != nil {
		t.Fatalf("query saga state: %v", err)
	}
	if saga.Status != SagaStatusFailed {
		t.Errorf("expected saga status %s, got %s", SagaStatusFailed, saga.Status)
	}
	if !saga.FailureReason.Valid {
		t.Error("expected failure reason to be set")
	}

	// Verify stock was NOT changed (reservation failed early)
	updatedProduct := getProduct(t, env, product.Id)
	if updatedProduct.StockAvailable != product.StockAvailable {
		t.Errorf("stock should not change on reservation failure, expected %d, got %d",
			product.StockAvailable, updatedProduct.StockAvailable)
	}
	if updatedProduct.StockReserved != 0 {
		t.Errorf("no stock should be reserved on failure, got %d", updatedProduct.StockReserved)
	}
}

// TestSagaFailure_PaymentProcessing tests saga failure during payment step with compensation
func TestSagaFailure_PaymentProcessing(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create test product
	product := createTestProduct(t, env, "Test Product", 1000, 10)

	// Create order repository and saga with custom payment failure
	orderRepo := NewRepository(env.orderDB)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	saga := NewSagaOrchestrator(env.orderDB, env.inventoryClient, log)

	// Override payment processor to fail
	saga.processPayment = func(ctx context.Context, orderID string) error {
		return errors.New("payment gateway unavailable")
	}

	// Create order directly in DB
	order, _, err := orderRepo.CreateOrder(ctx, CreateOrderParams{
		CustomerID: "customer-1",
		Items: []CreateOrderItemParams{
			{ProductID: product.Id, Quantity: 2, UnitPriceCents: 1000, ProductName: "Test"},
		},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	// Execute saga (should fail at payment step)
	sagaErr := saga.Execute(ctx, SagaInput{
		OrderID: order.ID,
		Items:   []SagaItem{{ProductID: product.Id, Quantity: 2}},
	})
	if sagaErr == nil {
		t.Fatal("expected saga to fail, but it succeeded")
	}

	// Verify order status is FAILED
	order, _, err = orderRepo.GetOrder(ctx, order.ID)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if order.Status != OrderStatusFailed {
		t.Errorf("expected order status %s, got %s", OrderStatusFailed, order.Status)
	}

	// Verify saga state
	var sagaState SagaState
	err = env.orderDB.Get(&sagaState, `SELECT * FROM saga_state WHERE order_id = $1`, order.ID)
	if err != nil {
		t.Fatalf("query saga state: %v", err)
	}
	if sagaState.Status != SagaStatusFailed {
		t.Errorf("expected saga status %s, got %s", SagaStatusFailed, sagaState.Status)
	}
	if sagaState.CurrentStep != string(StepProcessPayment) {
		t.Errorf("expected failed at step %s, got %s", StepProcessPayment, sagaState.CurrentStep)
	}

	// CRITICAL: Verify compensation occurred - stock should be released
	updatedProduct := getProduct(t, env, product.Id)
	if updatedProduct.StockReserved != 0 {
		t.Errorf("expected reserved stock to be released (0), got %d", updatedProduct.StockReserved)
	}
	if updatedProduct.StockAvailable != product.StockAvailable {
		t.Errorf("expected stock available to be restored to %d, got %d",
			product.StockAvailable, updatedProduct.StockAvailable)
	}

	// Verify reservation status is RELEASED
	var reservationStatus string
	err = env.inventoryDB.Get(&reservationStatus,
		`SELECT status FROM stock_reservations WHERE order_id = $1`, order.ID)
	if err != nil {
		t.Fatalf("query reservation status: %v", err)
	}
	if reservationStatus != "RELEASED" {
		t.Errorf("expected reservation status RELEASED, got %s", reservationStatus)
	}
}

// TestSagaFailure_ConfirmOrder tests saga failure during confirm step with compensation
func TestSagaFailure_ConfirmOrder(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create test product
	product := createTestProduct(t, env, "Test Product", 1000, 10)

	// Create a mock inventory client that fails on DecrementStock
	mockInventoryClient := &mockInventoryClientFailConfirm{
		real: env.inventoryClient,
	}

	// Create order repository and saga with mock client
	orderRepo := NewRepository(env.orderDB)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	saga := NewSagaOrchestrator(env.orderDB, mockInventoryClient, log)

	// Create order directly in DB
	order, _, err := orderRepo.CreateOrder(ctx, CreateOrderParams{
		CustomerID: "customer-1",
		Items: []CreateOrderItemParams{
			{ProductID: product.Id, Quantity: 2, UnitPriceCents: 1000, ProductName: "Test"},
		},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	// Execute saga (should fail at confirm step)
	sagaErr := saga.Execute(ctx, SagaInput{
		OrderID: order.ID,
		Items:   []SagaItem{{ProductID: product.Id, Quantity: 2}},
	})
	if sagaErr == nil {
		t.Fatal("expected saga to fail, but it succeeded")
	}

	// Verify order status is FAILED
	order, _, err = orderRepo.GetOrder(ctx, order.ID)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if order.Status != OrderStatusFailed {
		t.Errorf("expected order status %s, got %s", OrderStatusFailed, order.Status)
	}

	// Verify saga state
	var sagaState SagaState
	err = env.orderDB.Get(&sagaState, `SELECT * FROM saga_state WHERE order_id = $1`, order.ID)
	if err != nil {
		t.Fatalf("query saga state: %v", err)
	}
	if sagaState.Status != SagaStatusFailed {
		t.Errorf("expected saga status %s, got %s", SagaStatusFailed, sagaState.Status)
	}

	// CRITICAL: Verify compensation occurred - stock should be released
	updatedProduct := getProduct(t, env, product.Id)
	if updatedProduct.StockReserved != 0 {
		t.Errorf("expected reserved stock to be released (0), got %d", updatedProduct.StockReserved)
	}
	if updatedProduct.StockAvailable != product.StockAvailable {
		t.Errorf("expected stock available to be restored to %d, got %d",
			product.StockAvailable, updatedProduct.StockAvailable)
	}
}

// TestSagaIdempotency_AlreadyCompleted tests that completed sagas don't re-execute
func TestSagaIdempotency_AlreadyCompleted(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create test product
	product := createTestProduct(t, env, "Test Product", 1000, 10)

	// Create order and complete saga
	order1Resp, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "customer-1",
		Items:      []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if order1Resp.Order.Status != orderv1.OrderStatus_ORDER_STATUS_COMPLETED {
		t.Fatalf("expected first order to complete, got status %v", order1Resp.Order.Status)
	}

	// Create saga orchestrator to execute saga again
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	saga := NewSagaOrchestrator(env.orderDB, env.inventoryClient, log)

	// Try to execute saga again with same order ID
	err = saga.Execute(ctx, SagaInput{
		OrderID: order1Resp.Order.Id,
		Items:   []SagaItem{{ProductID: product.Id, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("saga should skip already completed order without error, got: %v", err)
	}

	// Verify stock was only decremented once (not twice)
	updatedProduct := getProduct(t, env, product.Id)
	expectedStock := product.StockAvailable - 2 // Should be -2, not -4
	if updatedProduct.StockAvailable != expectedStock {
		t.Errorf("stock should only be decremented once, expected %d, got %d",
			expectedStock, updatedProduct.StockAvailable)
	}

	// Verify only one saga state exists
	var count int
	err = env.orderDB.Get(&count, `SELECT COUNT(*) FROM saga_state WHERE order_id = $1`, order1Resp.Order.Id)
	if err != nil {
		t.Fatalf("count saga states: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 saga state, got %d", count)
	}
}

// TestSagaResume_InterruptedSagas tests the Resume() function for interrupted sagas
func TestSagaResume_InterruptedSagas(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create test product
	product := createTestProduct(t, env, "Test Product", 1000, 10)

	// Create order in DB
	orderRepo := NewRepository(env.orderDB)
	order, _, err := orderRepo.CreateOrder(ctx, CreateOrderParams{
		CustomerID: "customer-1",
		Items: []CreateOrderItemParams{
			{ProductID: product.Id, Quantity: 2, UnitPriceCents: 1000, ProductName: "Test"},
		},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	// Manually create an IN_PROGRESS saga state (simulating interrupted saga)
	var sagaID string
	err = env.orderDB.QueryRow(`
		INSERT INTO saga_state (order_id, current_step, status, reservation_id)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		order.ID, string(StepProcessPayment), SagaStatusInProgress, order.ID,
	).Scan(&sagaID)
	if err != nil {
		t.Fatalf("create interrupted saga: %v", err)
	}

	// Manually create a stock reservation (simulating step 1 completed)
	_, err = env.inventoryDB.Exec(`
		INSERT INTO stock_reservations (order_id, product_id, quantity, status)
		VALUES ($1, $2, $3, $4)`,
		order.ID, product.Id, 2, "RESERVED",
	)
	if err != nil {
		t.Fatalf("create reservation: %v", err)
	}

	// Update product stock to reflect reservation
	_, err = env.inventoryDB.Exec(`
		UPDATE products SET stock_available = stock_available - 2, stock_reserved = stock_reserved + 2
		WHERE id = $1`,
		product.Id,
	)
	if err != nil {
		t.Fatalf("update stock: %v", err)
	}

	// Create saga orchestrator and call Resume()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	saga := NewSagaOrchestrator(env.orderDB, env.inventoryClient, log)

	err = saga.Resume(ctx)
	if err != nil {
		t.Fatalf("resume sagas: %v", err)
	}

	// Verify saga was marked as FAILED
	var sagaState SagaState
	err = env.orderDB.Get(&sagaState, `SELECT * FROM saga_state WHERE id = $1`, sagaID)
	if err != nil {
		t.Fatalf("query saga state: %v", err)
	}
	if sagaState.Status != SagaStatusFailed {
		t.Errorf("expected resumed saga to be marked FAILED, got %s", sagaState.Status)
	}

	// Verify order was marked as FAILED
	order, _, err = orderRepo.GetOrder(ctx, order.ID)
	if err != nil {
		t.Fatalf("get order: %v", err)
	}
	if order.Status != OrderStatusFailed {
		t.Errorf("expected order to be marked FAILED, got %s", order.Status)
	}

	// CRITICAL: Verify compensation - stock should be released
	updatedProduct := getProduct(t, env, product.Id)
	if updatedProduct.StockReserved != 0 {
		t.Errorf("expected reserved stock to be released (0), got %d", updatedProduct.StockReserved)
	}
	if updatedProduct.StockAvailable != product.StockAvailable {
		t.Errorf("expected stock to be fully restored to %d, got %d",
			product.StockAvailable, updatedProduct.StockAvailable)
	}
}

// TestSagaStateTransitions validates that saga steps are persisted correctly
func TestSagaStateTransitions(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create test product
	product := createTestProduct(t, env, "Test Product", 1000, 10)

	// Create and execute order
	orderResp, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "customer-1",
		Items:      []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	// Query final saga state
	var saga SagaState
	err = env.orderDB.Get(&saga, `SELECT * FROM saga_state WHERE order_id = $1`, orderResp.Order.Id)
	if err != nil {
		t.Fatalf("query saga state: %v", err)
	}

	// Verify final state
	if saga.Status != SagaStatusCompleted {
		t.Errorf("expected status COMPLETED, got %s", saga.Status)
	}
	if saga.CurrentStep != string(StepConfirmOrder) {
		t.Errorf("expected final step CONFIRM_ORDER, got %s", saga.CurrentStep)
	}
	if !saga.ReservationID.Valid {
		t.Error("expected reservation_id to be set")
	}
	if saga.FailureReason.Valid {
		t.Errorf("expected no failure reason, got %s", saga.FailureReason.String)
	}
}

// TestSagaMultipleItems tests saga with multiple products
func TestSagaMultipleItems(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create multiple products
	product1 := createTestProduct(t, env, "Product 1", 1000, 10)
	product2 := createTestProduct(t, env, "Product 2", 2000, 5)

	// Create order with multiple items
	orderResp, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "customer-1",
		Items: []*orderv1.CreateOrderItem{
			{ProductId: product1.Id, Quantity: 2},
			{ProductId: product2.Id, Quantity: 1},
		},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	// Verify order completed
	if orderResp.Order.Status != orderv1.OrderStatus_ORDER_STATUS_COMPLETED {
		t.Errorf("expected order status %v, got %v", orderv1.OrderStatus_ORDER_STATUS_COMPLETED, orderResp.Order.Status)
	}

	// Verify both products had stock decremented
	updated1 := getProduct(t, env, product1.Id)
	updated2 := getProduct(t, env, product2.Id)

	if updated1.StockAvailable != product1.StockAvailable-2 {
		t.Errorf("product1: expected stock %d, got %d", product1.StockAvailable-2, updated1.StockAvailable)
	}
	if updated2.StockAvailable != product2.StockAvailable-1 {
		t.Errorf("product2: expected stock %d, got %d", product2.StockAvailable-1, updated2.StockAvailable)
	}

	// Verify no reserved stock remains
	if updated1.StockReserved != 0 || updated2.StockReserved != 0 {
		t.Errorf("expected no reserved stock, got product1=%d, product2=%d",
			updated1.StockReserved, updated2.StockReserved)
	}
}

// TestSagaMultipleItems_PartialFailure tests saga when one product has insufficient stock
func TestSagaMultipleItems_PartialFailure(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create products - second one has insufficient stock
	product1 := createTestProduct(t, env, "Product 1", 1000, 10)
	product2 := createTestProduct(t, env, "Product 2", 2000, 1) // Only 1 in stock

	// Try to order more than available for product2
	orderResp, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "customer-1",
		Items: []*orderv1.CreateOrderItem{
			{ProductId: product1.Id, Quantity: 2},
			{ProductId: product2.Id, Quantity: 5}, // Insufficient!
		},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	// Order should be FAILED
	if orderResp.Order.Status != orderv1.OrderStatus_ORDER_STATUS_FAILED {
		t.Errorf("expected order status %v, got %v", orderv1.OrderStatus_ORDER_STATUS_FAILED, orderResp.Order.Status)
	}

	// CRITICAL: Verify NO products had stock changed (atomic transaction)
	updated1 := getProduct(t, env, product1.Id)
	updated2 := getProduct(t, env, product2.Id)

	if updated1.StockAvailable != product1.StockAvailable {
		t.Errorf("product1 stock should not change on failed reservation, expected %d, got %d",
			product1.StockAvailable, updated1.StockAvailable)
	}
	if updated2.StockAvailable != product2.StockAvailable {
		t.Errorf("product2 stock should not change on failed reservation, expected %d, got %d",
			product2.StockAvailable, updated2.StockAvailable)
	}
	if updated1.StockReserved != 0 || updated2.StockReserved != 0 {
		t.Errorf("no stock should be reserved on failure, got product1=%d, product2=%d",
			updated1.StockReserved, updated2.StockReserved)
	}
}

// --- Mock Inventory Client for Testing Failure Scenarios ---

// mockInventoryClientFailConfirm wraps real client but fails on DecrementStock
type mockInventoryClientFailConfirm struct {
	inventoryv1.InventoryServiceClient
	real inventoryv1.InventoryServiceClient
}

func (m *mockInventoryClientFailConfirm) ReserveStock(ctx context.Context, req *inventoryv1.ReserveStockRequest, opts ...grpc.CallOption) (*inventoryv1.ReserveStockResponse, error) {
	// Delegate to real implementation
	return m.real.ReserveStock(ctx, req, opts...)
}

func (m *mockInventoryClientFailConfirm) ReleaseStock(ctx context.Context, req *inventoryv1.ReleaseStockRequest, opts ...grpc.CallOption) (*inventoryv1.ReleaseStockResponse, error) {
	// Delegate to real implementation
	return m.real.ReleaseStock(ctx, req, opts...)
}

func (m *mockInventoryClientFailConfirm) DecrementStock(ctx context.Context, req *inventoryv1.DecrementStockRequest, opts ...grpc.CallOption) (*inventoryv1.DecrementStockResponse, error) {
	// Always fail
	return nil, status.Error(codes.Internal, "simulated decrement failure")
}

func (m *mockInventoryClientFailConfirm) GetProduct(ctx context.Context, req *inventoryv1.GetProductRequest, opts ...grpc.CallOption) (*inventoryv1.GetProductResponse, error) {
	// Delegate to real implementation
	return m.real.GetProduct(ctx, req, opts...)
}
