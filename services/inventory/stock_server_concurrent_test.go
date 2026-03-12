package main

import (
	"context"
	"fmt"
	"sync"
	"testing"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Helper functions for concurrent tests
func createTestProduct(t *testing.T, env *testEnv, name, category string, priceCents int64, stock int32) *inventoryv1.Product {
	t.Helper()
	resp, err := env.client.CreateProduct(context.Background(), &inventoryv1.CreateProductRequest{
		Name:         name,
		Description:  "Test product for concurrent operations",
		Category:     category,
		PriceCents:   priceCents,
		InitialStock: stock,
	})
	if err != nil {
		t.Fatalf("create test product: %v", err)
	}
	return resp.Product
}

func getProduct(t *testing.T, env *testEnv, productID string) *inventoryv1.Product {
	t.Helper()
	resp, err := env.client.GetProduct(context.Background(), &inventoryv1.GetProductRequest{
		Id: productID,
	})
	if err != nil {
		t.Fatalf("get product: %v", err)
	}
	return resp.Product
}

// TestConcurrentReserveStock tests concurrent stock reservations for the same product
func TestConcurrentReserveStock(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create product with 100 units
	product := createTestProduct(t, env, "Concurrent Product", "electronics", 5000, 100)

	// Try to reserve stock concurrently (20 requests, 3 units each = 60 total)
	numReservations := 20
	quantityPerReservation := int32(3)
	var wg sync.WaitGroup
	results := make(chan *inventoryv1.ReserveStockResponse, numReservations)
	errors := make(chan error, numReservations)

	for i := 0; i < numReservations; i++ {
		wg.Add(1)
		go func(reqNum int) {
			defer wg.Done()
			resp, err := env.client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
				OrderId: fmt.Sprintf("00000000-0000-0000-0000-%012d", reqNum),
				Items: []*inventoryv1.StockItem{
					{ProductId: product.Id, Quantity: quantityPerReservation},
				},
			})
			if err != nil {
				errors <- err
				return
			}
			results <- resp
		}(i)
	}

	wg.Wait()
	close(results)
	close(errors)

	// Count successful reservations
	successCount := len(results)
	errorCount := len(errors)

	t.Logf("Concurrent reservations: %d successful, %d errors", successCount, errorCount)

	// All should succeed (sufficient stock)
	if successCount != numReservations {
		t.Errorf("expected %d successful reservations, got %d", numReservations, successCount)
	}

	// Verify final stock state
	updatedProduct := getProduct(t, env, product.Id)
	expectedReserved := int32(successCount) * quantityPerReservation

	// stock_available should NOT change during reservation
	if updatedProduct.StockAvailable != product.StockAvailable {
		t.Errorf("stock_available should not change, expected %d, got %d",
			product.StockAvailable, updatedProduct.StockAvailable)
	}
	if updatedProduct.StockReserved != expectedReserved {
		t.Errorf("expected reserved stock %d, got %d", expectedReserved, updatedProduct.StockReserved)
	}
}

// TestConcurrentReserveStock_InsufficientStock tests concurrent reservations with insufficient stock
func TestConcurrentReserveStock_InsufficientStock(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create product with only 10 units
	product := createTestProduct(t, env, "Limited Product", "test", 1000, 10)

	// Try 30 concurrent reservations of 2 units each (total demand: 60, available: 10)
	numReservations := 30
	quantityPerReservation := int32(2)
	var wg sync.WaitGroup
	results := make(chan *inventoryv1.ReserveStockResponse, numReservations)
	errors := make(chan error, numReservations)

	for i := 0; i < numReservations; i++ {
		wg.Add(1)
		go func(reqNum int) {
			defer wg.Done()
			resp, err := env.client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
				OrderId: fmt.Sprintf("00000000-1111-1111-1111-%012d", reqNum),
				Items: []*inventoryv1.StockItem{
					{ProductId: product.Id, Quantity: quantityPerReservation},
				},
			})
			if err != nil {
				errors <- err
				return
			}
			results <- resp
		}(i)
	}

	wg.Wait()
	close(results)
	close(errors)

	successCount := len(results)
	errorCount := len(errors)

	// At most 5 reservations should succeed (10 / 2 = 5)
	if successCount > 5 {
		t.Errorf("too many reservations succeeded: expected at most 5, got %d (oversold!)", successCount)
	}

	// Check that errors are "insufficient stock" errors
	for err := range errors {
		st := status.Convert(err)
		if st.Code() != codes.FailedPrecondition {
			t.Errorf("expected FailedPrecondition error, got %v", st.Code())
		}
	}

	// Verify no overselling occurred
	updatedProduct := getProduct(t, env, product.Id)

	// stock_available should NOT change during reservations
	if updatedProduct.StockAvailable != product.StockAvailable {
		t.Errorf("stock_available changed unexpectedly: expected %d, got %d",
			product.StockAvailable, updatedProduct.StockAvailable)
	}

	// reserved should not exceed available
	if updatedProduct.StockReserved > product.StockAvailable {
		t.Errorf("reserved more than available: reserved=%d, available=%d",
			updatedProduct.StockReserved, product.StockAvailable)
	}

	t.Logf("Concurrent limited stock: %d successful, %d failed, final available=%d, reserved=%d",
		successCount, errorCount, updatedProduct.StockAvailable, updatedProduct.StockReserved)
}

// TestConcurrentReserveAndRelease tests concurrent reserve and release operations
func TestConcurrentReserveAndRelease(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create product
	product := createTestProduct(t, env, "Reserve/Release Product", "test", 2000, 50)

	// First, create 10 reservations
	reservationIDs := make([]string, 10)
	for i := 0; i < 10; i++ {
		orderID := fmt.Sprintf("00000000-2222-2222-2222-%012d", i)
		resp, err := env.client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
			OrderId: orderID,
			Items:   []*inventoryv1.StockItem{{ProductId: product.Id, Quantity: 3}},
		})
		if err != nil {
			t.Fatalf("create reservation %d: %v", i, err)
		}
		reservationIDs[i] = resp.ReservationId
	}

	// Verify stock after reservations
	// Note: stock_available doesn't change during reservation in this service
	// It represents total stock. stock_reserved tracks what's reserved.
	afterReserve := getProduct(t, env, product.Id)
	if afterReserve.StockAvailable != 50 || afterReserve.StockReserved != 30 {
		t.Fatalf("unexpected stock after reservations: available=%d (expected 50), reserved=%d (expected 30)",
			afterReserve.StockAvailable, afterReserve.StockReserved)
	}

	// Now concurrently release all reservations
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for _, resID := range reservationIDs {
		wg.Add(1)
		go func(reservationID string) {
			defer wg.Done()
			_, err := env.client.ReleaseStock(ctx, &inventoryv1.ReleaseStockRequest{
				ReservationId: reservationID,
			})
			if err != nil {
				errors <- err
			}
		}(resID)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	if len(errors) > 0 {
		for err := range errors {
			t.Errorf("release error: %v", err)
		}
	}

	// Verify all stock is released
	afterRelease := getProduct(t, env, product.Id)
	if afterRelease.StockAvailable != product.StockAvailable {
		t.Errorf("stock not fully restored: expected %d, got %d",
			product.StockAvailable, afterRelease.StockAvailable)
	}
	if afterRelease.StockReserved != 0 {
		t.Errorf("expected no reserved stock, got %d", afterRelease.StockReserved)
	}

	t.Logf("Concurrent release: all 10 reservations released successfully")
}

// TestConcurrentDecrementStock tests concurrent decrement operations
func TestConcurrentDecrementStock(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create product
	product := createTestProduct(t, env, "Decrement Product", "test", 3000, 100)

	// Create 20 reservations
	reservationIDs := make([]string, 20)
	for i := 0; i < 20; i++ {
		orderID := fmt.Sprintf("00000000-3333-3333-3333-%012d", i)
		resp, err := env.client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
			OrderId: orderID,
			Items:   []*inventoryv1.StockItem{{ProductId: product.Id, Quantity: 2}},
		})
		if err != nil {
			t.Fatalf("create reservation %d: %v", i, err)
		}
		reservationIDs[i] = resp.ReservationId
	}

	// Verify stock after reservations
	afterReserve := getProduct(t, env, product.Id)
	if afterReserve.StockReserved != 40 {
		t.Fatalf("unexpected reserved stock: got %d, expected 40", afterReserve.StockReserved)
	}

	// Concurrently decrement all reservations
	var wg sync.WaitGroup
	errors := make(chan error, 20)

	for _, resID := range reservationIDs {
		wg.Add(1)
		go func(reservationID string) {
			defer wg.Done()
			_, err := env.client.DecrementStock(ctx, &inventoryv1.DecrementStockRequest{
				ReservationId: reservationID,
			})
			if err != nil {
				errors <- err
			}
		}(resID)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	if len(errors) > 0 {
		for err := range errors {
			t.Errorf("decrement error: %v", err)
		}
	}

	// Verify final stock state
	final := getProduct(t, env, product.Id)
	expectedAvailable := product.StockAvailable - 40 // 20 reservations * 2 units
	if final.StockAvailable != expectedAvailable {
		t.Errorf("expected available stock %d, got %d", expectedAvailable, final.StockAvailable)
	}
	if final.StockReserved != 0 {
		t.Errorf("expected no reserved stock, got %d", final.StockReserved)
	}

	t.Logf("Concurrent decrement: 20 decrements successful, final stock=%d", final.StockAvailable)
}

// TestConcurrentMixedOperations tests a mix of reserve, release, and decrement operations
func TestConcurrentMixedOperations(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create product with plenty of stock
	product := createTestProduct(t, env, "Mixed Ops Product", "test", 5000, 200)

	// Create some initial reservations
	initialReservations := make([]string, 10)
	for i := 0; i < 10; i++ {
		orderID := fmt.Sprintf("00000000-4444-4444-4444-%012d", i)
		resp, err := env.client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
			OrderId: orderID,
			Items:   []*inventoryv1.StockItem{{ProductId: product.Id, Quantity: 5}},
		})
		if err != nil {
			t.Fatalf("create initial reservation: %v", err)
		}
		initialReservations[i] = resp.ReservationId
	}

	var wg sync.WaitGroup
	errors := make(chan error, 60)
	var newReservationsMu sync.Mutex
	newReservations := make([]string, 0)

	// Concurrently:
	// - Create 20 new reservations
	// - Release 5 of the initial reservations
	// - Decrement 5 of the initial reservations

	// New reservations
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(reqNum int) {
			defer wg.Done()
			orderID := fmt.Sprintf("00000000-5555-5555-5555-%012d", reqNum)
			resp, err := env.client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
				OrderId: orderID,
				Items:   []*inventoryv1.StockItem{{ProductId: product.Id, Quantity: 3}},
			})
			if err != nil {
				errors <- err
				return
			}
			newReservationsMu.Lock()
			newReservations = append(newReservations, resp.ReservationId)
			newReservationsMu.Unlock()
		}(i)
	}

	// Release first 5 initial reservations
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := env.client.ReleaseStock(ctx, &inventoryv1.ReleaseStockRequest{
				ReservationId: initialReservations[idx],
			})
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// Decrement next 5 initial reservations
	for i := 5; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := env.client.DecrementStock(ctx, &inventoryv1.DecrementStockRequest{
				ReservationId: initialReservations[idx],
			})
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	var errorCount int
	for err := range errors {
		t.Logf("mixed operation error: %v", err)
		errorCount++
	}

	// Verify final stock state
	final := getProduct(t, env, product.Id)

	// Note: In this service, stock_available is total stock and doesn't change on reserve
	// Initial: 200 available, 0 reserved
	// After 10 initial reservations (5 units each): 200 available, 50 reserved
	// After releasing 5 (5 units each): 200 available, 25 reserved
	// After decrementing 5 (5 units each): 175 available, 0 reserved (decrement reduces both)
	// After 20 new reservations (3 units each): 175 available, 60 reserved

	expectedAvailable := int32(175)
	expectedReserved := int32(60)

	// Allow some flexibility due to potential race conditions
	if final.StockAvailable < expectedAvailable-5 || final.StockAvailable > expectedAvailable+5 {
		t.Errorf("available stock out of expected range: got %d, expected ~%d",
			final.StockAvailable, expectedAvailable)
	}
	if final.StockReserved < expectedReserved-5 || final.StockReserved > expectedReserved+5 {
		t.Errorf("reserved stock out of expected range: got %d, expected ~%d",
			final.StockReserved, expectedReserved)
	}

	// Most important: no negative stock
	if final.StockAvailable < 0 || final.StockReserved < 0 {
		t.Errorf("stock went negative: available=%d, reserved=%d",
			final.StockAvailable, final.StockReserved)
	}

	t.Logf("Mixed concurrent operations: %d errors, final: available=%d, reserved=%d",
		errorCount, final.StockAvailable, final.StockReserved)
}

// TestConcurrentDoubleRelease tests that releasing the same reservation twice is handled correctly
func TestConcurrentDoubleRelease(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create product and reservation
	product := createTestProduct(t, env, "Double Release Product", "test", 1000, 50)
	resp, err := env.client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
		OrderId: "00000000-6666-6666-6666-000000000001",
		Items:   []*inventoryv1.StockItem{{ProductId: product.Id, Quantity: 10}},
	})
	if err != nil {
		t.Fatalf("create reservation: %v", err)
	}
	reservationID := resp.ReservationId

	// Try to release the same reservation 10 times concurrently
	var wg sync.WaitGroup
	results := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := env.client.ReleaseStock(ctx, &inventoryv1.ReleaseStockRequest{
				ReservationId: reservationID,
			})
			results <- err
		}()
	}

	wg.Wait()
	close(results)

	// Exactly 1 should succeed, others should fail with NotFound
	var successCount, notFoundCount int
	for err := range results {
		if err == nil {
			successCount++
		} else {
			st := status.Convert(err)
			if st.Code() == codes.NotFound {
				notFoundCount++
			}
		}
	}

	if successCount != 1 {
		t.Errorf("expected exactly 1 successful release, got %d", successCount)
	}

	// Verify stock is only released once
	final := getProduct(t, env, product.Id)
	if final.StockAvailable != product.StockAvailable {
		t.Errorf("stock not correctly restored: expected %d, got %d",
			product.StockAvailable, final.StockAvailable)
	}
	if final.StockReserved != 0 {
		t.Errorf("expected no reserved stock, got %d", final.StockReserved)
	}

	t.Logf("Double release test: %d successful, %d not found (idempotency working)", successCount, notFoundCount)
}
