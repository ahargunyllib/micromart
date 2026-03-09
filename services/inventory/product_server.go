package main

import (
	"context"
	"errors"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) CreateProduct(ctx context.Context, req *inventoryv1.CreateProductRequest) (*inventoryv1.CreateProductResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if req.PriceCents < 0 {
		return nil, status.Error(codes.InvalidArgument, "price_cents must be non-negative")
	}
	if req.InitialStock < 0 {
		return nil, status.Error(codes.InvalidArgument, "initial_stock must be non-negative")
	}

	product, err := s.repo.CreateProduct(ctx, CreateProductParams{
		Name:         req.Name,
		Description:  req.Description,
		Category:     req.Category,
		PriceCents:   req.PriceCents,
		InitialStock: req.InitialStock,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create product: %v", err)
	}

	return &inventoryv1.CreateProductResponse{
		Product: productToProto(product),
	}, nil
}

func (s *Server) GetProduct(ctx context.Context, req *inventoryv1.GetProductRequest) (*inventoryv1.GetProductResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	product, err := s.repo.GetProduct(ctx, req.Id)
	if errors.Is(err, ErrNotFound) {
		return nil, status.Errorf(codes.NotFound, "product %s not found", req.Id)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get product: %v", err)
	}

	return &inventoryv1.GetProductResponse{
		Product: productToProto(product),
	}, nil
}

func (s *Server) UpdateProduct(ctx context.Context, req *inventoryv1.UpdateProductRequest) (*inventoryv1.UpdateProductResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	params := UpdateProductParams{}
	if req.Name != nil {
		params.Name = req.Name
	}
	if req.Description != nil {
		params.Description = req.Description
	}
	if req.Category != nil {
		params.Category = req.Category
	}
	if req.PriceCents != nil {
		params.PriceCents = req.PriceCents
	}
	if req.Active != nil {
		params.Active = req.Active
	}

	product, err := s.repo.UpdateProduct(ctx, req.Id, params)
	if errors.Is(err, ErrNotFound) {
		return nil, status.Errorf(codes.NotFound, "product %s not found", req.Id)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "update product: %v", err)
	}

	return &inventoryv1.UpdateProductResponse{
		Product: productToProto(product),
	}, nil
}

func (s *Server) ListProducts(ctx context.Context, req *inventoryv1.ListProductsRequest) (*inventoryv1.ListProductsResponse, error) {
	products, nextToken, err := s.repo.ListProducts(ctx, req.Category, req.PageSize, req.PageToken)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list products: %v", err)
	}

	protoProducts := make([]*inventoryv1.Product, len(products))
	for i, p := range products {
		protoProducts[i] = productToProto(&p)
	}

	return &inventoryv1.ListProductsResponse{
		Products:      protoProducts,
		NextPageToken: nextToken,
	}, nil
}

func (s *Server) SearchProducts(ctx context.Context, req *inventoryv1.SearchProductsRequest) (*inventoryv1.SearchProductsResponse, error) {
	if req.Query == "" {
		return nil, status.Error(codes.InvalidArgument, "query is required")
	}

	products, nextToken, err := s.repo.SearchProducts(ctx, req.Query, req.PageSize, req.PageToken)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "search products: %v", err)
	}

	protoProducts := make([]*inventoryv1.Product, len(products))
	for i, p := range products {
		protoProducts[i] = productToProto(&p)
	}

	return &inventoryv1.SearchProductsResponse{
		Products:      protoProducts,
		NextPageToken: nextToken,
	}, nil
}
