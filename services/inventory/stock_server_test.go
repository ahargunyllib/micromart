package main

import (
	"context"
	"testing"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestReserveStock(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	created, err := env.client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
		Name:         "Reserve Test",
		PriceCents:   1000,
		InitialStock: 50,
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	resp, err := env.client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
		OrderId: "00000000-0000-0000-0000-000000000123",
		Items: []*inventoryv1.StockItem{
			{ProductId: created.Product.Id, Quantity: 10},
		},
	})
	if err != nil {
		t.Fatalf("ReserveStock: %v", err)
	}
	if resp.ReservationId == "" {
		t.Error("expected reservation_id to be set")
	}

	// Verify stock updated
	product, err := env.client.GetProduct(ctx, &inventoryv1.GetProductRequest{
		Id: created.Product.Id,
	})
	if err != nil {
		t.Fatalf("GetProduct: %v", err)
	}
	if product.Product.StockAvailable != 50 {
		t.Errorf("stock_available = %d, want 50", product.Product.StockAvailable)
	}
	if product.Product.StockReserved != 10 {
		t.Errorf("stock_reserved = %d, want 10", product.Product.StockReserved)
	}
}

func TestReserveStock_InsufficientStock(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	created, err := env.client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
		Name:         "Low Stock",
		PriceCents:   1000,
		InitialStock: 5,
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	_, err = env.client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
		OrderId: "00000000-0000-0000-0000-000000000456",
		Items: []*inventoryv1.StockItem{
			{ProductId: created.Product.Id, Quantity: 10},
		},
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Errorf("expected FailedPrecondition, got %v", err)
	}
}

func TestReleaseStock(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	created, err := env.client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
		Name:         "Release Test",
		PriceCents:   1000,
		InitialStock: 50,
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	// Reserve
	reserved, err := env.client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
		OrderId: "00000000-0000-0000-0000-000000000789",
		Items: []*inventoryv1.StockItem{
			{ProductId: created.Product.Id, Quantity: 20},
		},
	})
	if err != nil {
		t.Fatalf("ReserveStock: %v", err)
	}

	// Release
	_, err = env.client.ReleaseStock(ctx, &inventoryv1.ReleaseStockRequest{
		ReservationId: reserved.ReservationId,
	})
	if err != nil {
		t.Fatalf("ReleaseStock: %v", err)
	}

	// Verify stock restored
	product, err := env.client.GetProduct(ctx, &inventoryv1.GetProductRequest{
		Id: created.Product.Id,
	})
	if err != nil {
		t.Fatalf("GetProduct: %v", err)
	}
	if product.Product.StockAvailable != 50 {
		t.Errorf("stock_available = %d, want 50", product.Product.StockAvailable)
	}
	if product.Product.StockReserved != 0 {
		t.Errorf("stock_reserved = %d, want 0", product.Product.StockReserved)
	}
}

func TestDecrementStock(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	created, err := env.client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
		Name:         "Decrement Test",
		PriceCents:   1000,
		InitialStock: 50,
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	// Reserve
	reserved, err := env.client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
		OrderId: "00000000-0000-0000-0000-00000000adec",
		Items: []*inventoryv1.StockItem{
			{ProductId: created.Product.Id, Quantity: 15},
		},
	})
	if err != nil {
		t.Fatalf("ReserveStock: %v", err)
	}

	// Decrement (confirm the sale)
	_, err = env.client.DecrementStock(ctx, &inventoryv1.DecrementStockRequest{
		ReservationId: reserved.ReservationId,
	})
	if err != nil {
		t.Fatalf("DecrementStock: %v", err)
	}

	// Verify: available reduced, reserved back to 0
	product, err := env.client.GetProduct(ctx, &inventoryv1.GetProductRequest{
		Id: created.Product.Id,
	})
	if err != nil {
		t.Fatalf("GetProduct: %v", err)
	}
	if product.Product.StockAvailable != 35 {
		t.Errorf("stock_available = %d, want 35", product.Product.StockAvailable)
	}
	if product.Product.StockReserved != 0 {
		t.Errorf("stock_reserved = %d, want 0", product.Product.StockReserved)
	}
}

func TestReleaseStock_AlreadyReleased(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	created, err := env.client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
		Name:         "Double Release",
		PriceCents:   1000,
		InitialStock: 50,
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	reserved, err := env.client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
		OrderId: "00000000-0000-0000-0000-0000dab1e000",
		Items: []*inventoryv1.StockItem{
			{ProductId: created.Product.Id, Quantity: 10},
		},
	})
	if err != nil {
		t.Fatalf("ReserveStock: %v", err)
	}

	// First release — should succeed
	_, err = env.client.ReleaseStock(ctx, &inventoryv1.ReleaseStockRequest{
		ReservationId: reserved.ReservationId,
	})
	if err != nil {
		t.Fatalf("ReleaseStock: %v", err)
	}

	// Second release — should fail (idempotent safety)
	_, err = env.client.ReleaseStock(ctx, &inventoryv1.ReleaseStockRequest{
		ReservationId: reserved.ReservationId,
	})
	if status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound on double release, got %v", err)
	}
}

func TestReserveStock_MultipleProducts(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	p1, _ := env.client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
		Name: "Multi A", PriceCents: 1000, InitialStock: 30,
	})
	p2, _ := env.client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
		Name: "Multi B", PriceCents: 2000, InitialStock: 20,
	})

	resp, err := env.client.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
		OrderId: "00000000-0000-0000-0000-000000aaa111",
		Items: []*inventoryv1.StockItem{
			{ProductId: p1.Product.Id, Quantity: 5},
			{ProductId: p2.Product.Id, Quantity: 3},
		},
	})
	if err != nil {
		t.Fatalf("ReserveStock: %v", err)
	}
	if resp.ReservationId == "" {
		t.Error("expected reservation_id")
	}

	// Verify both products updated
	got1, _ := env.client.GetProduct(ctx, &inventoryv1.GetProductRequest{Id: p1.Product.Id})
	got2, _ := env.client.GetProduct(ctx, &inventoryv1.GetProductRequest{Id: p2.Product.Id})

	if got1.Product.StockReserved != 5 {
		t.Errorf("product A: stock_reserved = %d, want 5", got1.Product.StockReserved)
	}
	if got2.Product.StockReserved != 3 {
		t.Errorf("product B: stock_reserved = %d, want 3", got2.Product.StockReserved)
	}
}
