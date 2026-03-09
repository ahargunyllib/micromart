package main

import orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"

type CreateOrderRequest struct {
	CustomerID     string            `json:"customer_id"`
	Items          []CreateOrderItem `json:"items"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
}

type CreateOrderItem struct {
	ProductID string `json:"product_id"`
	Quantity  int32  `json:"quantity"`
}

type OrderItemResponse struct {
	ProductID      string `json:"product_id"`
	ProductName    string `json:"product_name"`
	Quantity       int32  `json:"quantity"`
	UnitPriceCents int64  `json:"unit_price_cents"`
}

type OrderResponse struct {
	ID         string              `json:"id"`
	CustomerID string              `json:"customer_id"`
	Status     string              `json:"status"`
	Items      []OrderItemResponse `json:"items"`
	TotalCents int64               `json:"total_cents"`
	CreatedAt  string              `json:"created_at"`
	UpdatedAt  string              `json:"updated_at"`
}

type OrderListResponse struct {
	Orders        []OrderResponse `json:"orders"`
	NextPageToken string          `json:"next_page_token,omitempty"`
}

func orderToResponse(o *orderv1.Order) OrderResponse {
	items := make([]OrderItemResponse, len(o.Items))
	for i, item := range o.Items {
		items[i] = OrderItemResponse{
			ProductID:      item.ProductId,
			ProductName:    item.ProductName,
			Quantity:       item.Quantity,
			UnitPriceCents: item.UnitPriceCents,
		}
	}

	return OrderResponse{
		ID:         o.Id,
		CustomerID: o.CustomerId,
		Status:     o.Status.String(),
		Items:      items,
		TotalCents: o.TotalCents,
		CreatedAt:  o.CreatedAt.AsTime().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:  o.UpdatedAt.AsTime().Format("2006-01-02T15:04:05Z"),
	}
}
