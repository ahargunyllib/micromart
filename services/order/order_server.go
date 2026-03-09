package main

import (
	"context"
	"errors"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) CreateOrder(ctx context.Context, req *orderv1.CreateOrderRequest) (*orderv1.CreateOrderResponse, error) {
	if req.CustomerId == "" {
		return nil, status.Error(codes.InvalidArgument, "customer_id is required")
	}
	if len(req.Items) == 0 {
		return nil, status.Error(codes.InvalidArgument, "items cannot be empty")
	}

	orderItems := make([]CreateOrderItemParams, 0, len(req.Items))
	for _, item := range req.Items {
		if item.ProductId == "" {
			return nil, status.Error(codes.InvalidArgument, "product_id is required")
		}
		if item.Quantity <= 0 {
			return nil, status.Error(codes.InvalidArgument, "quantity must be positive")
		}

		productResp, err := s.inventoryClient.GetProduct(ctx, &inventoryv1.GetProductRequest{
			Id: item.ProductId,
		})
		if err != nil {
			st := status.Convert(err)
			if st.Code() == codes.NotFound {
				return nil, status.Errorf(codes.NotFound, "product %s not found", item.ProductId)
			}
			return nil, status.Errorf(codes.Internal, "get product %s: %v", item.ProductId, err)
		}

		product := productResp.Product
		if !product.Active {
			return nil, status.Errorf(codes.FailedPrecondition, "product %s is not active", item.ProductId)
		}

		orderItems = append(orderItems, CreateOrderItemParams{
			ProductID:      item.ProductId,
			Quantity:       item.Quantity,
			UnitPriceCents: product.PriceCents,
			ProductName:    product.Name,
		})
	}

	order, items, err := s.repo.CreateOrder(ctx, CreateOrderParams{
		CustomerID:     req.CustomerId,
		IdempotencyKey: req.IdempotencyKey,
		Items:          orderItems,
	})
	if errors.Is(err, ErrDuplicateOrder) {
		return &orderv1.CreateOrderResponse{
			Order: orderToProto(order, items, orderItems),
		}, nil
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create order: %v", err)
	}

	return &orderv1.CreateOrderResponse{
		Order: orderToProto(order, items, orderItems),
	}, nil
}

func (s *Server) GetOrder(ctx context.Context, req *orderv1.GetOrderRequest) (*orderv1.GetOrderResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	order, items, err := s.repo.GetOrder(ctx, req.Id)
	if errors.Is(err, ErrNotFound) {
		return nil, status.Errorf(codes.NotFound, "order %s not found", req.Id)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get order: %v", err)
	}

	protoItems := make([]*orderv1.OrderItem, len(items))
	for i, item := range items {
		productName := ""
		productResp, err := s.inventoryClient.GetProduct(ctx, &inventoryv1.GetProductRequest{
			Id: item.ProductID,
		})
		if err == nil {
			productName = productResp.Product.Name
		}

		protoItems[i] = &orderv1.OrderItem{
			ProductId:      item.ProductID,
			ProductName:    productName,
			Quantity:       item.Quantity,
			UnitPriceCents: item.UnitPriceCents,
		}
	}

	return &orderv1.GetOrderResponse{
		Order: &orderv1.Order{
			Id:         order.ID,
			CustomerId: order.CustomerID,
			Status:     statusToProto(order.Status),
			Items:      protoItems,
			TotalCents: order.TotalCents,
			CreatedAt:  timestamppb.New(order.CreatedAt),
			UpdatedAt:  timestamppb.New(order.UpdatedAt),
		},
	}, nil
}

func (s *Server) ListOrders(ctx context.Context, req *orderv1.ListOrdersRequest) (*orderv1.ListOrdersResponse, error) {
	if req.CustomerId == "" {
		return nil, status.Error(codes.InvalidArgument, "customer_id is required")
	}

	orders, nextToken, err := s.repo.ListOrders(ctx, req.CustomerId, req.PageSize, req.PageToken)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list orders: %v", err)
	}

	protoOrders := make([]*orderv1.Order, len(orders))
	for i, o := range orders {
		protoOrders[i] = &orderv1.Order{
			Id:         o.ID,
			CustomerId: o.CustomerID,
			Status:     statusToProto(o.Status),
			TotalCents: o.TotalCents,
			CreatedAt:  timestamppb.New(o.CreatedAt),
			UpdatedAt:  timestamppb.New(o.UpdatedAt),
		}
	}

	return &orderv1.ListOrdersResponse{
		Orders:        protoOrders,
		NextPageToken: nextToken,
	}, nil
}

func (s *Server) CancelOrder(ctx context.Context, req *orderv1.CancelOrderRequest) (*orderv1.CancelOrderResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	order, items, err := s.repo.CancelOrder(ctx, req.Id)
	if errors.Is(err, ErrNotFound) {
		return nil, status.Errorf(codes.NotFound, "order %s not found", req.Id)
	}
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "cancel order: %v", err)
	}

	protoItems := make([]*orderv1.OrderItem, len(items))
	for i, item := range items {
		protoItems[i] = &orderv1.OrderItem{
			ProductId:      item.ProductID,
			Quantity:       item.Quantity,
			UnitPriceCents: item.UnitPriceCents,
		}
	}

	return &orderv1.CancelOrderResponse{
		Order: &orderv1.Order{
			Id:         order.ID,
			CustomerId: order.CustomerID,
			Status:     statusToProto(order.Status),
			Items:      protoItems,
			TotalCents: order.TotalCents,
			CreatedAt:  timestamppb.New(order.CreatedAt),
			UpdatedAt:  timestamppb.New(order.UpdatedAt),
		},
	}, nil
}
