package main

import (
	"context"
	"sync"
	"testing"

	orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"
)

// TestConcurrentOrders_SameProduct tests multiple concurrent orders for the same product
func TestConcurrentOrders_SameProduct(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create product with 100 units
	product := createTestProduct(t, env, "Concurrent Product", 1000, 100)

	// Create 10 concurrent orders, each ordering 5 units (total 50 units)
	numOrders := 10
	quantityPerOrder := int32(5)
	var wg sync.WaitGroup
	results := make(chan *orderv1.CreateOrderResponse, numOrders)
	errors := make(chan error, numOrders)

	for i := 0; i < numOrders; i++ {
		wg.Add(1)
		go func(orderNum int) {
			defer wg.Done()
			resp, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
				CustomerId: "concurrent-customer",
				Items:      []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: quantityPerOrder}},
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

	// Check for errors
	var errorCount int
	for err := range errors {
		t.Logf("order error: %v", err)
		errorCount++
	}

	// Count successful orders
	var successCount int
	var completedCount int
	for resp := range results {
		successCount++
		if resp.Order.Status == orderv1.OrderStatus_ORDER_STATUS_COMPLETED {
			completedCount++
		}
	}

	if successCount+errorCount != numOrders {
		t.Errorf("expected %d total responses, got %d", numOrders, successCount+errorCount)
	}

	// Verify final stock state
	updatedProduct := getProduct(t, env, product.Id)
	expectedStock := product.StockAvailable - (int32(completedCount) * quantityPerOrder)
	if updatedProduct.StockAvailable != expectedStock {
		t.Errorf("expected stock %d, got %d (completed orders: %d)", expectedStock, updatedProduct.StockAvailable, completedCount)
	}

	// No reserved stock should remain
	if updatedProduct.StockReserved != 0 {
		t.Errorf("expected no reserved stock, got %d", updatedProduct.StockReserved)
	}

	t.Logf("Concurrent orders: %d total, %d successful, %d completed, %d errors",
		numOrders, successCount, completedCount, errorCount)
}

// TestConcurrentOrders_InsufficientStock tests race condition when stock runs out
func TestConcurrentOrders_InsufficientStock(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create product with only 10 units
	product := createTestProduct(t, env, "Limited Product", 1000, 10)

	// Try to create 20 concurrent orders, each wanting 2 units (total demand: 40 units)
	numOrders := 20
	quantityPerOrder := int32(2)
	var wg sync.WaitGroup
	results := make(chan *orderv1.CreateOrderResponse, numOrders)
	errors := make(chan error, numOrders)

	for i := 0; i < numOrders; i++ {
		wg.Add(1)
		go func(orderNum int) {
			defer wg.Done()
			resp, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
				CustomerId: "concurrent-customer",
				Items:      []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: quantityPerOrder}},
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

	// Collect results
	var completedCount, failedCount int
	for resp := range results {
		switch resp.Order.Status {
		case orderv1.OrderStatus_ORDER_STATUS_COMPLETED:
			completedCount++
		case orderv1.OrderStatus_ORDER_STATUS_FAILED:
			failedCount++
		}
	}

	for range errors {
		// Errors are also expected
	}

	// At most 5 orders should complete (10 units / 2 per order = 5)
	if completedCount > 5 {
		t.Errorf("expected at most 5 completed orders, got %d (oversold!)", completedCount)
	}

	// Verify stock is not negative
	updatedProduct := getProduct(t, env, product.Id)
	if updatedProduct.StockAvailable < 0 {
		t.Errorf("stock went negative: %d", updatedProduct.StockAvailable)
	}
	if updatedProduct.StockReserved < 0 {
		t.Errorf("reserved stock went negative: %d", updatedProduct.StockReserved)
	}

	// No reserved stock should remain after all sagas complete
	if updatedProduct.StockReserved != 0 {
		t.Errorf("expected no reserved stock after completion, got %d", updatedProduct.StockReserved)
	}

	t.Logf("Concurrent limited stock: completed=%d, failed=%d, final_stock=%d",
		completedCount, failedCount, updatedProduct.StockAvailable)
}

// TestConcurrentOrders_Idempotency tests concurrent duplicate requests with same idempotency key
func TestConcurrentOrders_Idempotency(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create product
	product := createTestProduct(t, env, "Test Product", 1000, 100)

	// Make 10 concurrent requests with the SAME idempotency key
	numRequests := 10
	idempotencyKey := "test-idempotency-key-12345"
	var wg sync.WaitGroup
	results := make(chan *orderv1.CreateOrderResponse, numRequests)
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
				CustomerId:     "idempotent-customer",
				IdempotencyKey: idempotencyKey,
				Items:          []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: 5}},
			})
			if err != nil {
				errors <- err
				return
			}
			results <- resp
		}()
	}

	wg.Wait()
	close(results)
	close(errors)

	// Collect results
	orderIDs := make(map[string]int)
	for resp := range results {
		orderIDs[resp.Order.Id]++
	}

	for err := range errors {
		t.Logf("idempotency error: %v", err)
	}

	// ALL responses should have the SAME order ID (idempotency working)
	if len(orderIDs) != 1 {
		t.Errorf("expected exactly 1 unique order ID, got %d: %v", len(orderIDs), orderIDs)
	}

	// Stock should only be decremented ONCE (5 units, not 50)
	updatedProduct := getProduct(t, env, product.Id)
	expectedStock := product.StockAvailable - 5
	if updatedProduct.StockAvailable != expectedStock {
		t.Errorf("stock should only be decremented once, expected %d, got %d",
			expectedStock, updatedProduct.StockAvailable)
	}

	// Verify only ONE order exists in database
	var orderCount int
	err := env.orderDB.Get(&orderCount, `SELECT COUNT(*) FROM orders WHERE idempotency_key = $1`, idempotencyKey)
	if err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if orderCount != 1 {
		t.Errorf("expected exactly 1 order in DB, got %d", orderCount)
	}

	t.Logf("Idempotency test: %d requests -> %d unique order(s)", numRequests, len(orderIDs))
}

// TestConcurrentOrders_DifferentCustomers tests concurrent orders from different customers
func TestConcurrentOrders_DifferentCustomers(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create product
	product := createTestProduct(t, env, "Popular Product", 5000, 1000)

	// Create 50 concurrent orders from different customers
	numCustomers := 50
	var wg sync.WaitGroup
	results := make(chan *orderv1.CreateOrderResponse, numCustomers)
	errors := make(chan error, numCustomers)

	for i := 0; i < numCustomers; i++ {
		wg.Add(1)
		go func(customerNum int) {
			defer wg.Done()
			resp, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
				CustomerId: "customer-" + string(rune('A'+customerNum%26)) + "-" + string(rune('0'+customerNum/26)),
				Items:      []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: 10}},
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

	// Count successful orders
	var completedCount int
	customerOrders := make(map[string]int)
	for resp := range results {
		if resp.Order.Status == orderv1.OrderStatus_ORDER_STATUS_COMPLETED {
			completedCount++
			customerOrders[resp.Order.CustomerId]++
		}
	}

	for err := range errors {
		t.Logf("concurrent customer error: %v", err)
	}

	// All orders should succeed (plenty of stock)
	if completedCount != numCustomers {
		t.Errorf("expected %d completed orders, got %d", numCustomers, completedCount)
	}

	// Each customer should have exactly 1 order
	for customerID, count := range customerOrders {
		if count != 1 {
			t.Errorf("customer %s has %d orders, expected 1", customerID, count)
		}
	}

	// Verify stock
	updatedProduct := getProduct(t, env, product.Id)
	expectedStock := product.StockAvailable - (int32(completedCount) * 10)
	if updatedProduct.StockAvailable != expectedStock {
		t.Errorf("expected stock %d, got %d", expectedStock, updatedProduct.StockAvailable)
	}

	t.Logf("Concurrent customers: %d orders from %d customers, final stock: %d",
		completedCount, len(customerOrders), updatedProduct.StockAvailable)
}

// TestConcurrentOrders_MultipleProducts tests concurrent orders with multiple products
func TestConcurrentOrders_MultipleProducts(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create multiple products
	product1 := createTestProduct(t, env, "Product A", 1000, 50)
	product2 := createTestProduct(t, env, "Product B", 2000, 50)
	product3 := createTestProduct(t, env, "Product C", 3000, 50)

	// Create 20 concurrent orders, each ordering from all 3 products
	numOrders := 20
	var wg sync.WaitGroup
	results := make(chan *orderv1.CreateOrderResponse, numOrders)
	errors := make(chan error, numOrders)

	for i := 0; i < numOrders; i++ {
		wg.Add(1)
		go func(orderNum int) {
			defer wg.Done()
			resp, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
				CustomerId: "multi-product-customer",
				Items: []*orderv1.CreateOrderItem{
					{ProductId: product1.Id, Quantity: 1},
					{ProductId: product2.Id, Quantity: 1},
					{ProductId: product3.Id, Quantity: 1},
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

	// Count successful orders
	var completedCount int
	for resp := range results {
		if resp.Order.Status == orderv1.OrderStatus_ORDER_STATUS_COMPLETED {
			completedCount++
		}
	}

	for err := range errors {
		t.Logf("multi-product error: %v", err)
	}

	// All 20 orders should succeed (sufficient stock)
	if completedCount != numOrders {
		t.Errorf("expected %d completed orders, got %d", numOrders, completedCount)
	}

	// Verify all products have correct stock
	updated1 := getProduct(t, env, product1.Id)
	updated2 := getProduct(t, env, product2.Id)
	updated3 := getProduct(t, env, product3.Id)

	if updated1.StockAvailable != product1.StockAvailable-int32(completedCount) {
		t.Errorf("product1 stock incorrect: expected %d, got %d",
			product1.StockAvailable-int32(completedCount), updated1.StockAvailable)
	}
	if updated2.StockAvailable != product2.StockAvailable-int32(completedCount) {
		t.Errorf("product2 stock incorrect: expected %d, got %d",
			product2.StockAvailable-int32(completedCount), updated2.StockAvailable)
	}
	if updated3.StockAvailable != product3.StockAvailable-int32(completedCount) {
		t.Errorf("product3 stock incorrect: expected %d, got %d",
			product3.StockAvailable-int32(completedCount), updated3.StockAvailable)
	}

	// No reserved stock should remain
	if updated1.StockReserved != 0 || updated2.StockReserved != 0 || updated3.StockReserved != 0 {
		t.Errorf("expected no reserved stock, got p1=%d, p2=%d, p3=%d",
			updated1.StockReserved, updated2.StockReserved, updated3.StockReserved)
	}

	t.Logf("Multi-product concurrent: %d orders completed successfully", completedCount)
}
