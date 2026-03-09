package main

import (
	"encoding/json"
	"net/http"
	"strconv"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	"github.com/go-chi/chi/v5"
)

type ProductHandler struct {
	client inventoryv1.InventoryServiceClient
}

func NewProductHandler(client inventoryv1.InventoryServiceClient) *ProductHandler {
	return &ProductHandler{client: client}
}

func (h *ProductHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, err := h.client.CreateProduct(r.Context(), &inventoryv1.CreateProductRequest{
		Name:         req.Name,
		Description:  req.Description,
		Category:     req.Category,
		PriceCents:   req.PriceCents,
		InitialStock: req.InitialStock,
	})
	if err != nil {
		grpcErrorToHTTP(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, productToResponse(resp.Product))
}

func (h *ProductHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	resp, err := h.client.GetProduct(r.Context(), &inventoryv1.GetProductRequest{
		Id: id,
	})
	if err != nil {
		grpcErrorToHTTP(w, err)
		return
	}

	writeJSON(w, http.StatusOK, productToResponse(resp.Product))
}

func (h *ProductHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req UpdateProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	grpcReq := &inventoryv1.UpdateProductRequest{Id: id}
	if req.Name != nil {
		grpcReq.Name = req.Name
	}
	if req.Description != nil {
		grpcReq.Description = req.Description
	}
	if req.Category != nil {
		grpcReq.Category = req.Category
	}
	if req.PriceCents != nil {
		grpcReq.PriceCents = req.PriceCents
	}
	if req.Active != nil {
		grpcReq.Active = req.Active
	}

	resp, err := h.client.UpdateProduct(r.Context(), grpcReq)
	if err != nil {
		grpcErrorToHTTP(w, err)
		return
	}

	writeJSON(w, http.StatusOK, productToResponse(resp.Product))
}

func (h *ProductHandler) List(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	pageToken := r.URL.Query().Get("page_token")
	pageSize := int32(20)
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if n, err := strconv.Atoi(ps); err == nil {
			pageSize = int32(n)
		}
	}

	resp, err := h.client.ListProducts(r.Context(), &inventoryv1.ListProductsRequest{
		Category:  category,
		PageSize:  pageSize,
		PageToken: pageToken,
	})
	if err != nil {
		grpcErrorToHTTP(w, err)
		return
	}

	products := make([]ProductResponse, len(resp.Products))
	for i, p := range resp.Products {
		products[i] = productToResponse(p)
	}

	writeJSON(w, http.StatusOK, PaginatedResponse[ProductResponse]{
		Data:          products,
		NextPageToken: resp.NextPageToken,
	})
}

func (h *ProductHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	pageToken := r.URL.Query().Get("page_token")
	pageSize := int32(20)
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if n, err := strconv.Atoi(ps); err == nil {
			pageSize = int32(n)
		}
	}

	resp, err := h.client.SearchProducts(r.Context(), &inventoryv1.SearchProductsRequest{
		Query:     query,
		PageSize:  pageSize,
		PageToken: pageToken,
	})
	if err != nil {
		grpcErrorToHTTP(w, err)
		return
	}

	products := make([]ProductResponse, len(resp.Products))
	for i, p := range resp.Products {
		products[i] = productToResponse(p)
	}

	writeJSON(w, http.StatusOK, PaginatedResponse[ProductResponse]{
		Data:          products,
		NextPageToken: resp.NextPageToken,
	})
}
