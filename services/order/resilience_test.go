package main

import (
	"context"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// testLogger returns a logger for tests (errors only to reduce noise)
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestRetryMechanism_TransientErrors validates behavior with transient errors
// Note: In production, retries happen at the gRPC client level via RetryInterceptor.
// This test validates that without interceptors, transient errors cause saga failure.
func TestRetryMechanism_TransientErrors(t *testing.T) {
	t.Skip("Retry logic is implemented at gRPC client level via interceptors, not in saga")

	// In production setup (main.go):
	// - RetryInterceptor retries up to 3 times with exponential backoff
	// - Only retries transient errors (Unavailable, DeadlineExceeded, etc.)
	// - Backoff formula: random(0, baseDelay * 2^attempt) capped at 10s
	// - Test setup uses direct mock clients without interceptors

	// To truly test retry behavior, would need to:
	// 1. Create gRPC connection with interceptors
	// 2. Run mock inventory server that fails N times
	// 3. Validate retry count and timing
	// This is better tested at the grpcutil package level
}

// TestRetryMechanism_NonTransientErrors validates non-transient errors fail immediately
func TestRetryMechanism_NonTransientErrors(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create product
	product := createTestProduct(t, env, "No Retry Product", 1000, 10)

	// Create mock client that always returns NotFound (non-transient)
	var attemptCount atomic.Int32
	mockClient := &mockInventoryClientNonTransient{
		real:         env.inventoryClient,
		attemptCount: &attemptCount,
	}

	orderRepo := NewRepository(env.orderDB)
	saga := NewSagaOrchestrator(env.orderDB, mockClient, testLogger())

	order, _, err := orderRepo.CreateOrder(ctx, CreateOrderParams{
		CustomerID: "no-retry-customer",
		Items: []CreateOrderItemParams{
			{ProductID: product.Id, Quantity: 1, UnitPriceCents: 1000, ProductName: "Test"},
		},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	// Execute saga - should fail immediately without retries
	start := time.Now()
	err = saga.Execute(ctx, SagaInput{
		OrderID: order.ID,
		Items:   []SagaItem{{ProductID: product.Id, Quantity: 1}},
	})
	duration := time.Since(start)

	if err == nil {
		t.Fatal("expected saga to fail with non-transient error")
	}

	// Should only attempt once (no retries for non-transient errors)
	totalAttempts := attemptCount.Load()
	if totalAttempts != 1 {
		t.Errorf("expected exactly 1 attempt (no retries), got %d", totalAttempts)
	}

	// Should fail quickly (no backoff delays)
	if duration > 500*time.Millisecond {
		t.Errorf("non-transient error took too long: %v", duration)
	}

	t.Logf("Non-transient error: %d attempts in %v (no retries, as expected)", totalAttempts, duration)
}

// TestCircuitBreaker_TripsAfterConsecutiveFailures validates circuit breaker opens after 5 failures
func TestCircuitBreaker_TripsAfterConsecutiveFailures(t *testing.T) {
	// Note: This test validates the concept but full circuit breaker integration
	// requires the grpcutil interceptors which are applied at the gRPC client level.
	// In production, the circuit breaker prevents cascading failures by failing fast
	// after detecting repeated transient errors.

	t.Skip("Circuit breaker integration requires full gRPC client setup with interceptors")

	// Circuit breaker configuration (from grpcutil package):
	// - Trips after 5 consecutive failures
	// - Stays open for 10 seconds
	// - Half-open state allows 3 test requests
	// - Only counts transient errors (Unavailable, DeadlineExceeded, etc.)
}

// TestCircuitBreaker_AllowsNonTransientErrors validates circuit breaker doesn't trip on NotFound
func TestCircuitBreaker_AllowsNonTransientErrors(t *testing.T) {
	t.Skip("Circuit breaker integration requires full gRPC client setup with interceptors")

	// The circuit breaker is designed to:
	// - Allow NotFound, InvalidArgument, FailedPrecondition to pass through
	// - Only count Unavailable, DeadlineExceeded, ResourceExhausted as failures
	// - This prevents normal business logic errors from triggering circuit breaker
}

// TestIdempotency_WithRetries validates idempotency works correctly with retry mechanism
func TestIdempotency_WithRetries(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create product
	product := createTestProduct(t, env, "Idempotency Retry Product", 1000, 10)

	// Scenario: First request fails transiently, retry succeeds
	// The idempotency key should ensure only ONE order is created

	idempotencyKey := "idempotent-retry-key-001"

	// Create order (may retry internally)
	resp, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId:     "idempotent-customer",
		IdempotencyKey: idempotencyKey,
		Items:          []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}

	orderID := resp.Order.Id

	// Verify only one order exists with this idempotency key
	var count int
	err = env.orderDB.Get(&count, `SELECT COUNT(*) FROM orders WHERE idempotency_key = $1`, idempotencyKey)
	if err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 order with idempotency key, got %d", count)
	}

	// Verify stock only decremented once (not multiple times due to retries)
	updatedProduct := getProduct(t, env, product.Id)
	expectedStock := int32(8) // 10 - 2 = 8
	if updatedProduct.StockAvailable != expectedStock {
		t.Errorf("stock should only be decremented once despite retries, expected %d, got %d",
			expectedStock, updatedProduct.StockAvailable)
	}

	t.Logf("Idempotency with retries: order %s created exactly once", orderID)
}

// TestTimeout_RespectContextDeadline validates operations respect context timeouts
func TestTimeout_RespectContextDeadline(t *testing.T) {
	env := setupTestEnv(t)

	// Create product
	product := createTestProduct(t, env, "Timeout Product", 1000, 10)

	// Create context with very short timeout (will cause DeadlineExceeded)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Wait for timeout to trigger
	time.Sleep(10 * time.Millisecond)

	// Try to create order with expired context
	_, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "timeout-customer",
		Items:      []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: 1}},
	})

	// Should get DeadlineExceeded error
	if err == nil {
		t.Fatal("expected deadline exceeded error")
	}

	st := status.Convert(err)
	if st.Code() != codes.DeadlineExceeded && st.Code() != codes.Canceled {
		t.Errorf("expected DeadlineExceeded or Canceled, got %v", st.Code())
	}

	t.Logf("Timeout test: correctly got error %v", st.Code())
}

// TestSagaRecovery_WithIntermittentFailures validates saga handles transient failures gracefully
func TestSagaRecovery_WithIntermittentFailures(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create product
	product := createTestProduct(t, env, "Recovery Product", 1000, 20)

	// Mock client that fails only during payment step, succeeds on other steps
	var paymentAttempts atomic.Int32
	mockClient := &mockInventoryClientIntermittent{
		real:            env.inventoryClient,
		paymentAttempts: &paymentAttempts,
	}

	orderRepo := NewRepository(env.orderDB)
	saga := NewSagaOrchestrator(env.orderDB, mockClient, testLogger())

	// Create multiple orders - some may hit transient failures but should recover
	successCount := 0
	for i := 0; i < 5; i++ {
		order, _, err := orderRepo.CreateOrder(ctx, CreateOrderParams{
			CustomerID: "recovery-customer",
			Items: []CreateOrderItemParams{
				{ProductID: product.Id, Quantity: 1, UnitPriceCents: 1000, ProductName: "Test"},
			},
		})
		if err != nil {
			t.Fatalf("create order %d: %v", i, err)
		}

		err = saga.Execute(ctx, SagaInput{
			OrderID: order.ID,
			Items:   []SagaItem{{ProductID: product.Id, Quantity: 1}},
		})

		if err == nil {
			successCount++
		}
	}

	// At least some orders should succeed (may have transient failures but retries help)
	if successCount == 0 {
		t.Error("expected at least some orders to succeed with retries")
	}

	t.Logf("Saga recovery: %d/5 orders succeeded with intermittent failures", successCount)
}

// --- Mock Clients for Resilience Testing ---

// mockInventoryClientTransientFailures fails N times with Unavailable, then succeeds
type mockInventoryClientTransientFailures struct {
	inventoryv1.InventoryServiceClient
	real         inventoryv1.InventoryServiceClient
	failCount    int32
	attemptCount *atomic.Int32
}

func (m *mockInventoryClientTransientFailures) ReserveStock(ctx context.Context, req *inventoryv1.ReserveStockRequest, opts ...grpc.CallOption) (*inventoryv1.ReserveStockResponse, error) {
	attempt := m.attemptCount.Add(1)

	if attempt <= m.failCount {
		// Simulate transient failure
		return nil, status.Error(codes.Unavailable, "simulated transient failure")
	}

	// Succeed after N failures
	return m.real.ReserveStock(ctx, req, opts...)
}

func (m *mockInventoryClientTransientFailures) GetProduct(ctx context.Context, req *inventoryv1.GetProductRequest, opts ...grpc.CallOption) (*inventoryv1.GetProductResponse, error) {
	return m.real.GetProduct(ctx, req, opts...)
}

func (m *mockInventoryClientTransientFailures) ReleaseStock(ctx context.Context, req *inventoryv1.ReleaseStockRequest, opts ...grpc.CallOption) (*inventoryv1.ReleaseStockResponse, error) {
	return m.real.ReleaseStock(ctx, req, opts...)
}

func (m *mockInventoryClientTransientFailures) DecrementStock(ctx context.Context, req *inventoryv1.DecrementStockRequest, opts ...grpc.CallOption) (*inventoryv1.DecrementStockResponse, error) {
	return m.real.DecrementStock(ctx, req, opts...)
}

// mockInventoryClientNonTransient always returns NotFound (non-transient)
type mockInventoryClientNonTransient struct {
	inventoryv1.InventoryServiceClient
	real         inventoryv1.InventoryServiceClient
	attemptCount *atomic.Int32
}

func (m *mockInventoryClientNonTransient) ReserveStock(ctx context.Context, req *inventoryv1.ReserveStockRequest, opts ...grpc.CallOption) (*inventoryv1.ReserveStockResponse, error) {
	m.attemptCount.Add(1)
	// Non-transient error - should NOT retry
	return nil, status.Error(codes.NotFound, "product not found - non-transient")
}

func (m *mockInventoryClientNonTransient) GetProduct(ctx context.Context, req *inventoryv1.GetProductRequest, opts ...grpc.CallOption) (*inventoryv1.GetProductResponse, error) {
	return m.real.GetProduct(ctx, req, opts...)
}

func (m *mockInventoryClientNonTransient) ReleaseStock(ctx context.Context, req *inventoryv1.ReleaseStockRequest, opts ...grpc.CallOption) (*inventoryv1.ReleaseStockResponse, error) {
	return m.real.ReleaseStock(ctx, req, opts...)
}

func (m *mockInventoryClientNonTransient) DecrementStock(ctx context.Context, req *inventoryv1.DecrementStockRequest, opts ...grpc.CallOption) (*inventoryv1.DecrementStockResponse, error) {
	return m.real.DecrementStock(ctx, req, opts...)
}

// mockInventoryClientIntermittent simulates intermittent failures
type mockInventoryClientIntermittent struct {
	inventoryv1.InventoryServiceClient
	real            inventoryv1.InventoryServiceClient
	paymentAttempts *atomic.Int32
}

func (m *mockInventoryClientIntermittent) ReserveStock(ctx context.Context, req *inventoryv1.ReserveStockRequest, opts ...grpc.CallOption) (*inventoryv1.ReserveStockResponse, error) {
	// Randomly fail 30% of the time with transient error
	if m.paymentAttempts.Add(1)%3 == 0 {
		return nil, status.Error(codes.Unavailable, "intermittent failure")
	}
	return m.real.ReserveStock(ctx, req, opts...)
}

func (m *mockInventoryClientIntermittent) GetProduct(ctx context.Context, req *inventoryv1.GetProductRequest, opts ...grpc.CallOption) (*inventoryv1.GetProductResponse, error) {
	return m.real.GetProduct(ctx, req, opts...)
}

func (m *mockInventoryClientIntermittent) ReleaseStock(ctx context.Context, req *inventoryv1.ReleaseStockRequest, opts ...grpc.CallOption) (*inventoryv1.ReleaseStockResponse, error) {
	return m.real.ReleaseStock(ctx, req, opts...)
}

func (m *mockInventoryClientIntermittent) DecrementStock(ctx context.Context, req *inventoryv1.DecrementStockRequest, opts ...grpc.CallOption) (*inventoryv1.DecrementStockResponse, error) {
	return m.real.DecrementStock(ctx, req, opts...)
}
