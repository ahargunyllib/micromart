package main

import inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"

type CreateProductRequest struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Category     string `json:"category"`
	PriceCents   int64  `json:"price_cents"`
	InitialStock int32  `json:"initial_stock"`
}

type UpdateProductRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Category    *string `json:"category,omitempty"`
	PriceCents  *int64  `json:"price_cents,omitempty"`
	Active      *bool   `json:"active,omitempty"`
}

type ProductResponse struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Category       string `json:"category"`
	PriceCents     int64  `json:"price_cents"`
	StockAvailable int32  `json:"stock_available"`
	StockReserved  int32  `json:"stock_reserved"`
	Active         bool   `json:"active"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type ProductListResponse struct {
	Products      []ProductResponse `json:"products"`
	NextPageToken string            `json:"next_page_token,omitempty"`
}

func productToResponse(p *inventoryv1.Product) ProductResponse {
	return ProductResponse{
		ID:             p.Id,
		Name:           p.Name,
		Description:    p.Description,
		Category:       p.Category,
		PriceCents:     p.PriceCents,
		StockAvailable: p.StockAvailable,
		StockReserved:  p.StockReserved,
		Active:         p.Active,
		CreatedAt:      p.CreatedAt.AsTime().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:      p.UpdatedAt.AsTime().Format("2006-01-02T15:04:05Z"),
	}
}
