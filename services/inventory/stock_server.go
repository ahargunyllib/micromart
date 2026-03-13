package main

import (
	"context"
	"errors"
	"time"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) ReserveStock(ctx context.Context, req *inventoryv1.ReserveStockRequest) (*inventoryv1.ReserveStockResponse, error) {
	if req.OrderId == "" {
		return nil, status.Error(codes.InvalidArgument, "order_id is required")
	}
	if len(req.Items) == 0 {
		return nil, status.Error(codes.InvalidArgument, "items cannot be empty")
	}

	items := make([]StockItem, len(req.Items))
	for i, item := range req.Items {
		if item.ProductId == "" {
			return nil, status.Error(codes.InvalidArgument, "product_id is required")
		}
		if item.Quantity <= 0 {
			return nil, status.Error(codes.InvalidArgument, "quantity must be positive")
		}
		items[i] = StockItem{
			ProductID: item.ProductId,
			Quantity:  item.Quantity,
		}
	}

	start := time.Now()
	reservationID, err := s.repo.ReserveStock(ctx, req.OrderId, items)

	if s.metrics != nil {
		if err != nil {
			s.metrics.StockReservationsTotal.WithLabelValues("failed").Inc()
		} else {
			s.metrics.StockReservationsTotal.WithLabelValues("success").Inc()
		}
		s.metrics.LockWaitDuration.WithLabelValues("reserve_stock").Observe(time.Since(start).Seconds())
	}

	if errors.Is(err, ErrNotFound) {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	if errors.Is(err, ErrInsufficientStock) {
		return nil, status.Errorf(codes.FailedPrecondition, "%v", err)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "reserve stock: %v", err)
	}

	return &inventoryv1.ReserveStockResponse{
		ReservationId: reservationID,
	}, nil
}

func (s *Server) ReleaseStock(ctx context.Context, req *inventoryv1.ReleaseStockRequest) (*inventoryv1.ReleaseStockResponse, error) {
	if req.ReservationId == "" {
		return nil, status.Error(codes.InvalidArgument, "reservation_id is required")
	}

	err := s.repo.ReleaseStock(ctx, req.ReservationId)
	if errors.Is(err, ErrInvalidReservation) {
		return nil, status.Errorf(codes.NotFound, "reservation %s not found", req.ReservationId)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "release stock: %v", err)
	}

	return &inventoryv1.ReleaseStockResponse{}, nil
}

func (s *Server) DecrementStock(ctx context.Context, req *inventoryv1.DecrementStockRequest) (*inventoryv1.DecrementStockResponse, error) {
	if req.ReservationId == "" {
		return nil, status.Error(codes.InvalidArgument, "reservation_id is required")
	}

	err := s.repo.DecrementStock(ctx, req.ReservationId)
	if errors.Is(err, ErrInvalidReservation) {
		return nil, status.Errorf(codes.NotFound, "reservation %s not found", req.ReservationId)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "decrement stock: %v", err)
	}

	return &inventoryv1.DecrementStockResponse{}, nil
}
