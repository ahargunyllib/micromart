package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestHealth(t *testing.T) {
	env := setupTestEnv(t)
	rec := env.doRequest("GET", "/health", nil)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestProductCRUD(t *testing.T) {
	env := setupTestEnv(t)

	rec := env.doRequest("POST", "/api/v1/products", CreateProductRequest{
		Name: "Test Shoe", Description: "A nice shoe", Category: "footwear",
		PriceCents: 7999, InitialStock: 40,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	product := decodeBody[ProductResponse](t, rec)
	if product.Name != "Test Shoe" {
		t.Errorf("name = %q, want %q", product.Name, "Test Shoe")
	}
	if product.PriceCents != 7999 {
		t.Errorf("price = %d, want 7999", product.PriceCents)
	}

	rec = env.doRequest("GET", "/api/v1/products/"+product.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: status = %d", rec.Code)
	}
	got := decodeBody[ProductResponse](t, rec)
	if got.ID != product.ID {
		t.Errorf("id = %s, want %s", got.ID, product.ID)
	}

	newName := "Updated Shoe"
	rec = env.doRequest("PUT", "/api/v1/products/"+product.ID, UpdateProductRequest{
		Name: &newName,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	updated := decodeBody[ProductResponse](t, rec)
	if updated.Name != "Updated Shoe" {
		t.Errorf("name = %q, want %q", updated.Name, "Updated Shoe")
	}
}

func TestProductList(t *testing.T) {
	env := setupTestEnv(t)

	for i := 0; i < 3; i++ {
		env.doRequest("POST", "/api/v1/products", CreateProductRequest{
			Name: fmt.Sprintf("Item %d", i), Category: "test", PriceCents: 1000, InitialStock: 10,
		})
	}

	rec := env.doRequest("GET", "/api/v1/products?page_size=10", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d", rec.Code)
	}

	resp := decodeBody[PaginatedResponse[ProductResponse]](t, rec)
	if len(resp.Data) != 3 {
		t.Errorf("got %d products, want 3", len(resp.Data))
	}
}

func TestProductSearch(t *testing.T) {
	env := setupTestEnv(t)

	env.doRequest("POST", "/api/v1/products", CreateProductRequest{
		Name: "Wireless Headphones", PriceCents: 4999, InitialStock: 20,
	})
	env.doRequest("POST", "/api/v1/products", CreateProductRequest{
		Name: "Cotton Socks", PriceCents: 599, InitialStock: 100,
	})

	rec := env.doRequest("GET", "/api/v1/products/search?q=headphones", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("search: status = %d", rec.Code)
	}

	resp := decodeBody[PaginatedResponse[ProductResponse]](t, rec)
	if len(resp.Data) != 1 {
		t.Errorf("got %d, want 1", len(resp.Data))
	}
}

func TestProductSearch_MissingQuery(t *testing.T) {
	env := setupTestEnv(t)

	rec := env.doRequest("GET", "/api/v1/products/search", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestProductNotFound(t *testing.T) {
	env := setupTestEnv(t)

	rec := env.doRequest("GET", "/api/v1/products/00000000-0000-0000-0000-000000000000", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestCreateOrder(t *testing.T) {
	env := setupTestEnv(t)

	rec := env.doRequest("POST", "/api/v1/products", CreateProductRequest{
		Name: "Order Widget", PriceCents: 1500, InitialStock: 50,
	})
	product := decodeBody[ProductResponse](t, rec)

	rec = env.doRequest("POST", "/api/v1/orders", CreateOrderRequest{
		CustomerID: "customer-1",
		Items: []CreateOrderItem{
			{ProductID: product.ID, Quantity: 3},
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create order: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	order := decodeBody[OrderResponse](t, rec)
	if order.CustomerID != "customer-1" {
		t.Errorf("customer_id = %q, want %q", order.CustomerID, "customer-1")
	}
	if order.TotalCents != 1500*3 {
		t.Errorf("total = %d, want %d", order.TotalCents, 1500*3)
	}
	if len(order.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(order.Items))
	}
	if order.Items[0].ProductName != "Order Widget" {
		t.Errorf("product_name = %q, want %q", order.Items[0].ProductName, "Order Widget")
	}
}

func TestCreateOrder_ProductNotFound(t *testing.T) {
	env := setupTestEnv(t)

	rec := env.doRequest("POST", "/api/v1/orders", CreateOrderRequest{
		CustomerID: "customer-2",
		Items: []CreateOrderItem{
			{ProductID: "00000000-0000-0000-0000-000000000000", Quantity: 1},
		},
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestGetOrder(t *testing.T) {
	env := setupTestEnv(t)

	rec := env.doRequest("POST", "/api/v1/products", CreateProductRequest{
		Name: "Get Order Test", PriceCents: 2000, InitialStock: 10,
	})
	product := decodeBody[ProductResponse](t, rec)

	rec = env.doRequest("POST", "/api/v1/orders", CreateOrderRequest{
		CustomerID: "customer-get",
		Items:      []CreateOrderItem{{ProductID: product.ID, Quantity: 1}},
	})
	created := decodeBody[OrderResponse](t, rec)

	rec = env.doRequest("GET", "/api/v1/orders/"+created.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get order: status = %d", rec.Code)
	}
	got := decodeBody[OrderResponse](t, rec)
	if got.ID != created.ID {
		t.Errorf("id = %s, want %s", got.ID, created.ID)
	}
}

func TestGetOrder_NotFound(t *testing.T) {
	env := setupTestEnv(t)

	rec := env.doRequest("GET", "/api/v1/orders/00000000-0000-0000-0000-000000000000", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestListOrders(t *testing.T) {
	env := setupTestEnv(t)

	rec := env.doRequest("POST", "/api/v1/products", CreateProductRequest{
		Name: "List Test", PriceCents: 1000, InitialStock: 100,
	})
	product := decodeBody[ProductResponse](t, rec)

	for i := 0; i < 3; i++ {
		env.doRequest("POST", "/api/v1/orders", CreateOrderRequest{
			CustomerID: "customer-list",
			Items:      []CreateOrderItem{{ProductID: product.ID, Quantity: 1}},
		})
	}

	rec = env.doRequest("GET", "/api/v1/orders?customer_id=customer-list&page_size=10", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d", rec.Code)
	}

	resp := decodeBody[PaginatedResponse[OrderResponse]](t, rec)
	if len(resp.Data) != 3 {
		t.Errorf("got %d orders, want 3", len(resp.Data))
	}
}

func TestListOrders_MissingCustomerID(t *testing.T) {
	env := setupTestEnv(t)

	rec := env.doRequest("GET", "/api/v1/orders", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestCancelOrder(t *testing.T) {
	env := setupTestEnv(t)

	rec := env.doRequest("POST", "/api/v1/products", CreateProductRequest{
		Name: "Cancel Test", PriceCents: 1000, InitialStock: 10,
	})
	product := decodeBody[ProductResponse](t, rec)

	rec = env.doRequest("POST", "/api/v1/orders", CreateOrderRequest{
		CustomerID: "customer-cancel",
		Items:      []CreateOrderItem{{ProductID: product.ID, Quantity: 1}},
	})
	created := decodeBody[OrderResponse](t, rec)

	rec = env.doRequest("POST", "/api/v1/orders/"+created.ID+"/cancel", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	cancelled := decodeBody[OrderResponse](t, rec)
	if cancelled.Status != "ORDER_STATUS_CANCELLED" {
		t.Errorf("status = %q, want CANCELLED", cancelled.Status)
	}
}

func TestCreateOrder_InvalidBody(t *testing.T) {
	env := setupTestEnv(t)

	req := httptest.NewRequest("POST", "/api/v1/orders", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
