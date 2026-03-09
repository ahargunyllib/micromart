package main

import (
	"context"
	"fmt"
	"testing"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCreateProduct(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	resp, err := env.client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
		Name:         "Test Shirt",
		Description:  "A comfortable cotton shirt",
		Category:     "clothing",
		PriceCents:   1999,
		InitialStock: 100,
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	p := resp.Product
	if p.Id == "" {
		t.Error("expected product ID to be set")
	}
	if p.Name != "Test Shirt" {
		t.Errorf("name = %q, want %q", p.Name, "Test Shirt")
	}
	if p.PriceCents != 1999 {
		t.Errorf("price_cents = %d, want %d", p.PriceCents, 1999)
	}
	if p.StockAvailable != 100 {
		t.Errorf("stock_available = %d, want %d", p.StockAvailable, 100)
	}
	if !p.Active {
		t.Error("expected product to be active")
	}
}

func TestCreateProduct_Validation(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	_, err := env.client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
		Name: "",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", err)
	}
}

func TestGetProduct(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	created, err := env.client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
		Name:         "Get Test",
		PriceCents:   500,
		InitialStock: 10,
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	resp, err := env.client.GetProduct(ctx, &inventoryv1.GetProductRequest{
		Id: created.Product.Id,
	})
	if err != nil {
		t.Fatalf("GetProduct: %v", err)
	}

	if resp.Product.Name != "Get Test" {
		t.Errorf("name = %q, want %q", resp.Product.Name, "Get Test")
	}
}

func TestGetProduct_NotFound(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	_, err := env.client.GetProduct(ctx, &inventoryv1.GetProductRequest{
		Id: "00000000-0000-0000-0000-000000000000",
	})
	if status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestUpdateProduct(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	created, err := env.client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
		Name:         "Before Update",
		PriceCents:   1000,
		InitialStock: 50,
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	newName := "After Update"
	newPrice := int64(2000)
	resp, err := env.client.UpdateProduct(ctx, &inventoryv1.UpdateProductRequest{
		Id:         created.Product.Id,
		Name:       &newName,
		PriceCents: &newPrice,
	})
	if err != nil {
		t.Fatalf("UpdateProduct: %v", err)
	}

	if resp.Product.Name != "After Update" {
		t.Errorf("name = %q, want %q", resp.Product.Name, "After Update")
	}
	if resp.Product.PriceCents != 2000 {
		t.Errorf("price_cents = %d, want %d", resp.Product.PriceCents, 2000)
	}
	// Stock should be unchanged
	if resp.Product.StockAvailable != 50 {
		t.Errorf("stock_available = %d, want %d", resp.Product.StockAvailable, 50)
	}
}

func TestListProducts(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	// Create products in two categories
	for i := 0; i < 3; i++ {
		_, err := env.client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
			Name:         fmt.Sprintf("Shirt %d", i),
			Category:     "clothing",
			PriceCents:   1000,
			InitialStock: 10,
		})
		if err != nil {
			t.Fatalf("CreateProduct: %v", err)
		}
	}
	_, err := env.client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
		Name:         "Laptop",
		Category:     "electronics",
		PriceCents:   99900,
		InitialStock: 5,
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	// List all
	resp, err := env.client.ListProducts(ctx, &inventoryv1.ListProductsRequest{
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	if len(resp.Products) != 4 {
		t.Errorf("got %d products, want 4", len(resp.Products))
	}

	// List by category
	resp, err = env.client.ListProducts(ctx, &inventoryv1.ListProductsRequest{
		Category: "clothing",
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("ListProducts by category: %v", err)
	}
	if len(resp.Products) != 3 {
		t.Errorf("got %d products, want 3", len(resp.Products))
	}
}

func TestListProducts_Pagination(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, err := env.client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
			Name:         fmt.Sprintf("Product %d", i),
			PriceCents:   1000,
			InitialStock: 10,
		})
		if err != nil {
			t.Fatalf("CreateProduct: %v", err)
		}
	}

	// First page
	resp, err := env.client.ListProducts(ctx, &inventoryv1.ListProductsRequest{
		PageSize: 2,
	})
	if err != nil {
		t.Fatalf("ListProducts page 1: %v", err)
	}
	if len(resp.Products) != 2 {
		t.Errorf("page 1: got %d products, want 2", len(resp.Products))
	}
	if resp.NextPageToken == "" {
		t.Error("expected next_page_token to be set")
	}

	// Second page
	resp2, err := env.client.ListProducts(ctx, &inventoryv1.ListProductsRequest{
		PageSize:  2,
		PageToken: resp.NextPageToken,
	})
	if err != nil {
		t.Fatalf("ListProducts page 2: %v", err)
	}
	if len(resp2.Products) != 2 {
		t.Errorf("page 2: got %d products, want 2", len(resp2.Products))
	}

	// Third page (last)
	resp3, err := env.client.ListProducts(ctx, &inventoryv1.ListProductsRequest{
		PageSize:  2,
		PageToken: resp2.NextPageToken,
	})
	if err != nil {
		t.Fatalf("ListProducts page 3: %v", err)
	}
	if len(resp3.Products) != 1 {
		t.Errorf("page 3: got %d products, want 1", len(resp3.Products))
	}
	if resp3.NextPageToken != "" {
		t.Error("expected no next_page_token on last page")
	}
}

func TestSearchProducts(t *testing.T) {
	env := setupTestEnv(t)
	ctx := context.Background()

	_, err := env.client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
		Name:         "Wireless Bluetooth Headphones",
		PriceCents:   4999,
		InitialStock: 25,
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	_, err = env.client.CreateProduct(ctx, &inventoryv1.CreateProductRequest{
		Name:         "Cotton T-Shirt",
		PriceCents:   1999,
		InitialStock: 50,
	})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	resp, err := env.client.SearchProducts(ctx, &inventoryv1.SearchProductsRequest{
		Query:    "headphones",
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("SearchProducts: %v", err)
	}
	if len(resp.Products) != 1 {
		t.Errorf("got %d products, want 1", len(resp.Products))
	}
	if len(resp.Products) > 0 && resp.Products[0].Name != "Wireless Bluetooth Headphones" {
		t.Errorf("name = %q, want %q", resp.Products[0].Name, "Wireless Bluetooth Headphones")
	}
}
