package main

import (
	"encoding/json"
	"net/http"
	"strconv"

	orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"
	"github.com/go-chi/chi/v5"
)

type OrderHandler struct {
	client orderv1.OrderServiceClient
}

func NewOrderHandler(client orderv1.OrderServiceClient) *OrderHandler {
	return &OrderHandler{client: client}
}

func (h *OrderHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	items := make([]*orderv1.CreateOrderItem, len(req.Items))
	for i, item := range req.Items {
		items[i] = &orderv1.CreateOrderItem{
			ProductId: item.ProductID,
			Quantity:  item.Quantity,
		}
	}

	resp, err := h.client.CreateOrder(r.Context(), &orderv1.CreateOrderRequest{
		CustomerId:     req.CustomerID,
		Items:          items,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		grpcErrorToHTTP(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, orderToResponse(resp.Order))
}

func (h *OrderHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	resp, err := h.client.GetOrder(r.Context(), &orderv1.GetOrderRequest{
		Id: id,
	})
	if err != nil {
		grpcErrorToHTTP(w, err)
		return
	}

	writeJSON(w, http.StatusOK, orderToResponse(resp.Order))
}

func (h *OrderHandler) List(w http.ResponseWriter, r *http.Request) {
	customerID := r.URL.Query().Get("customer_id")
	if customerID == "" {
		writeError(w, http.StatusBadRequest, "query parameter 'customer_id' is required")
		return
	}

	pageToken := r.URL.Query().Get("page_token")
	pageSize := int32(20)
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if n, err := strconv.Atoi(ps); err == nil {
			pageSize = int32(n)
		}
	}

	resp, err := h.client.ListOrders(r.Context(), &orderv1.ListOrdersRequest{
		CustomerId: customerID,
		PageSize:   pageSize,
		PageToken:  pageToken,
	})
	if err != nil {
		grpcErrorToHTTP(w, err)
		return
	}

	orders := make([]OrderResponse, len(resp.Orders))
	for i, o := range resp.Orders {
		orders[i] = orderToResponse(o)
	}

	writeJSON(w, http.StatusOK, PaginatedResponse[OrderResponse]{
		Data:          orders,
		NextPageToken: resp.NextPageToken,
	})
}

func (h *OrderHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	resp, err := h.client.CancelOrder(r.Context(), &orderv1.CancelOrderRequest{
		Id: id,
	})
	if err != nil {
		grpcErrorToHTTP(w, err)
		return
	}

	writeJSON(w, http.StatusOK, orderToResponse(resp.Order))
}
