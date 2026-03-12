package main

import (
	"context"
	"fmt"
	"testing"

	orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"
	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCreateOrder(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	product := createTestProduct(t, env, "Test Widget", 1999, 100)

	resp, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "customer-1",
		Items: []*orderv1.CreateOrderItem{
			{ProductId: product.Id, Quantity: 3},
		},
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	order := resp.Order
	if order.Id == "" {
		t.Error("expected order ID")
	}
	if order.CustomerId != "customer-1" {
		t.Errorf("customer_id = %q, want %q", order.CustomerId, "customer-1")
	}
	if order.Status != orderv1.OrderStatus_ORDER_STATUS_COMPLETED {
		t.Errorf("status = %v, want COMPLETED", order.Status)
	}
	if order.TotalCents != 1999*3 {
		t.Errorf("total_cents = %d, want %d", order.TotalCents, 1999*3)
	}
	if len(order.Items) != 1 {
		t.Fatalf("items count = %d, want 1", len(order.Items))
	}
	if order.Items[0].ProductName != "Test Widget" {
		t.Errorf("product_name = %q, want %q", order.Items[0].ProductName, "Test Widget")
	}
	if order.Items[0].UnitPriceCents != 1999 {
		t.Errorf("unit_price_cents = %d, want %d", order.Items[0].UnitPriceCents, 1999)
	}
}

func TestCreateOrder_MultipleItems(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	p1 := createTestProduct(t, env, "Shirt", 2999, 50)
	p2 := createTestProduct(t, env, "Pants", 4999, 30)

	resp, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "customer-2",
		Items: []*orderv1.CreateOrderItem{
			{ProductId: p1.Id, Quantity: 2},
			{ProductId: p2.Id, Quantity: 1},
		},
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	expectedTotal := int64(2999*2 + 4999*1)
	if resp.Order.TotalCents != expectedTotal {
		t.Errorf("total_cents = %d, want %d", resp.Order.TotalCents, expectedTotal)
	}
	if len(resp.Order.Items) != 2 {
		t.Errorf("items count = %d, want 2", len(resp.Order.Items))
	}
}

func TestCreateOrder_ProductNotFound(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	_, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "customer-3",
		Items: []*orderv1.CreateOrderItem{
			{ProductId: "00000000-0000-0000-0000-000000000000", Quantity: 1},
		},
	})
	if status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestCreateOrder_Validation(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Empty customer ID
	_, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "",
		Items:      []*orderv1.CreateOrderItem{{ProductId: "abc", Quantity: 1}},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument for empty customer_id, got %v", err)
	}

	// Empty items
	_, err = env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "customer",
		Items:      []*orderv1.CreateOrderItem{},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument for empty items, got %v", err)
	}

	// Zero quantity
	product := createTestProduct(t, env, "Validation Test", 1000, 10)
	_, err = env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "customer",
		Items:      []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: 0}},
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument for zero quantity, got %v", err)
	}
}

func TestCreateOrder_Idempotency(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	product := createTestProduct(t, env, "Idempotent Widget", 999, 50)

	req := &orderv1.CreateOrderRequest{
		CustomerId:     "customer-idem",
		IdempotencyKey: "unique-key-123",
		Items: []*orderv1.CreateOrderItem{
			{ProductId: product.Id, Quantity: 1},
		},
	}

	// First call
	resp1, err := env.orderClient.CreateOrder(ctx, req)
	if err != nil {
		t.Fatalf("CreateOrder first: %v", err)
	}

	// Second call with same key
	resp2, err := env.orderClient.CreateOrder(ctx, req)
	if err != nil {
		t.Fatalf("CreateOrder second: %v", err)
	}

	p := getProduct(t, env, product.Id)
	if p.StockAvailable != 49 {
		t.Errorf("stock_available = %d, want 49 (saga should run only once)", p.StockAvailable)
	}

	// Should return the same order
	if resp1.Order.Id != resp2.Order.Id {
		t.Errorf("idempotency failed: first=%s second=%s", resp1.Order.Id, resp2.Order.Id)
	}
}

func TestGetOrder(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	product := createTestProduct(t, env, "Get Test", 1500, 20)

	created, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "customer-get",
		Items:      []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: 2}},
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	resp, err := env.orderClient.GetOrder(ctx, &orderv1.GetOrderRequest{
		Id: created.Order.Id,
	})
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}

	if resp.Order.Id != created.Order.Id {
		t.Errorf("id = %s, want %s", resp.Order.Id, created.Order.Id)
	}
	if resp.Order.TotalCents != 3000 {
		t.Errorf("total_cents = %d, want 3000", resp.Order.TotalCents)
	}
	if len(resp.Order.Items) != 1 {
		t.Fatalf("items count = %d, want 1", len(resp.Order.Items))
	}
	if resp.Order.Items[0].ProductName != "Get Test" {
		t.Errorf("product_name = %q, want %q", resp.Order.Items[0].ProductName, "Get Test")
	}
}

func TestGetOrder_NotFound(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	_, err := env.orderClient.GetOrder(ctx, &orderv1.GetOrderRequest{
		Id: "00000000-0000-0000-0000-000000000000",
	})
	if status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestListOrders(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	product := createTestProduct(t, env, "List Test", 1000, 100)

	// Create 3 orders for the same customer
	for i := 0; i < 3; i++ {
		_, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
			CustomerId: "customer-list",
			Items:      []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: 1}},
		})
		if err != nil {
			t.Fatalf("CreateOrder %d: %v", i, err)
		}
	}

	// Create 1 order for different customer
	_, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "customer-other",
		Items:      []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("CreateOrder other: %v", err)
	}

	resp, err := env.orderClient.ListOrders(ctx, &orderv1.ListOrdersRequest{
		CustomerId: "customer-list",
		PageSize:   10,
	})
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	if len(resp.Orders) != 3 {
		t.Errorf("got %d orders, want 3", len(resp.Orders))
	}
}

func TestListOrders_Pagination(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	product := createTestProduct(t, env, "Page Test", 1000, 100)

	for i := 0; i < 5; i++ {
		_, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
			CustomerId:     "customer-page",
			IdempotencyKey: fmt.Sprintf("page-key-%d", i),
			Items:          []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: 1}},
		})
		if err != nil {
			t.Fatalf("CreateOrder %d: %v", i, err)
		}
	}

	// Page 1
	resp1, err := env.orderClient.ListOrders(ctx, &orderv1.ListOrdersRequest{
		CustomerId: "customer-page",
		PageSize:   2,
	})
	if err != nil {
		t.Fatalf("ListOrders page 1: %v", err)
	}
	if len(resp1.Orders) != 2 {
		t.Errorf("page 1: got %d, want 2", len(resp1.Orders))
	}
	if resp1.NextPageToken == "" {
		t.Error("expected next_page_token")
	}

	// Page 2
	resp2, err := env.orderClient.ListOrders(ctx, &orderv1.ListOrdersRequest{
		CustomerId: "customer-page",
		PageSize:   2,
		PageToken:  resp1.NextPageToken,
	})
	if err != nil {
		t.Fatalf("ListOrders page 2: %v", err)
	}
	if len(resp2.Orders) != 2 {
		t.Errorf("page 2: got %d, want 2", len(resp2.Orders))
	}

	// Page 3 (last)
	resp3, err := env.orderClient.ListOrders(ctx, &orderv1.ListOrdersRequest{
		CustomerId: "customer-page",
		PageSize:   2,
		PageToken:  resp2.NextPageToken,
	})
	if err != nil {
		t.Fatalf("ListOrders page 3: %v", err)
	}
	if len(resp3.Orders) != 1 {
		t.Errorf("page 3: got %d, want 1", len(resp3.Orders))
	}
	if resp3.NextPageToken != "" {
		t.Error("expected empty next_page_token on last page")
	}
}

func TestCancelOrder(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	product := createTestProduct(t, env, "Cancel Test", 1000, 5)

	// Create an order that will fail due to insufficient stock (requesting more than available)
	created, err := env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "customer-cancel",
		Items:      []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: 10}},
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	// Order should be in FAILED status due to insufficient stock
	if created.Order.Status != orderv1.OrderStatus_ORDER_STATUS_FAILED {
		t.Fatalf("expected FAILED status, got %v", created.Order.Status)
	}

	// Cancel the failed order
	resp, err := env.orderClient.CancelOrder(ctx, &orderv1.CancelOrderRequest{
		Id: created.Order.Id,
	})
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}

	if resp.Order.Status != orderv1.OrderStatus_ORDER_STATUS_CANCELLED {
		t.Errorf("status = %v, want CANCELLED", resp.Order.Status)
	}
}

func TestCancelOrder_NotFound(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	_, err := env.orderClient.CancelOrder(ctx, &orderv1.CancelOrderRequest{
		Id: "00000000-0000-0000-0000-000000000000",
	})
	if status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestCreateOrder_InactiveProduct(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	product := createTestProduct(t, env, "Inactive Product", 1000, 10)

	// Deactivate the product directly in DB
	_, err := env.inventoryDB.Exec(`UPDATE products SET active = FALSE WHERE id = $1`, product.Id)
	if err != nil {
		t.Fatalf("deactivate product: %v", err)
	}

	_, err = env.orderClient.CreateOrder(ctx, &orderv1.CreateOrderRequest{
		CustomerId: "customer-inactive",
		Items:      []*orderv1.CreateOrderItem{{ProductId: product.Id, Quantity: 1}},
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Errorf("expected FailedPrecondition for inactive product, got %v", err)
	}
}
