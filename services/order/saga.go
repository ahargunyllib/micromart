package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	"github.com/jmoiron/sqlx"
)

type SagaStep string

const (
	StepReserveInventory SagaStep = "RESERVE_INVENTORY"
	StepProcessPayment   SagaStep = "PROCESS_PAYMENT"
	StepConfirmOrder     SagaStep = "CONFIRM_ORDER"
)

// PaymentFunc is the payment processor. Swappable for testing.
type PaymentFunc func(ctx context.Context, orderID string) error

type SagaOrchestrator struct {
	db              *sqlx.DB
	inventoryClient inventoryv1.InventoryServiceClient
	log             *slog.Logger
	processPayment  PaymentFunc
}

func NewSagaOrchestrator(db *sqlx.DB, inventoryClient inventoryv1.InventoryServiceClient, log *slog.Logger) *SagaOrchestrator {
	return &SagaOrchestrator{
		db:              db,
		inventoryClient: inventoryClient,
		log:             log,
		processPayment: func(ctx context.Context, orderID string) error {
			return nil // stub: always succeeds
		},
	}
}

type SagaInput struct {
	OrderID string
	Items   []SagaItem
}

type SagaItem struct {
	ProductID string
	Quantity  int32
}

func (s *SagaOrchestrator) Execute(ctx context.Context, input SagaInput) error {
	// Check for existing saga (idempotency)
	existingSaga, err := s.getExistingSaga(ctx, input.OrderID)
	if err == nil {
		// Found an existing saga
		if existingSaga.Status == SagaStatusCompleted {
			s.log.Info("saga already completed, skipping", slog.String("order_id", input.OrderID), slog.String("saga_id", existingSaga.ID))
			return nil
		}
		if existingSaga.Status == SagaStatusInProgress {
			s.log.Warn("saga already in progress, skipping duplicate execution", slog.String("order_id", input.OrderID), slog.String("saga_id", existingSaga.ID))
			return nil
		}
		// If failed, we could retry, but for now we skip to prevent double-execution
		s.log.Warn("saga already exists with status, skipping", slog.String("order_id", input.OrderID), slog.String("status", existingSaga.Status))
		return nil
	}
	// If error is not "no rows", return the error
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check existing saga: %w", err)
	}
	// No existing saga found, proceed with creation

	sagaID, err := s.createSagaState(ctx, input.OrderID)
	if err != nil {
		return fmt.Errorf("create saga state: %w", err)
	}

	s.log.Info("saga started", slog.String("order_id", input.OrderID), slog.String("saga_id", sagaID))

	// Step 1: Reserve Inventory
	s.updateStep(ctx, sagaID, StepReserveInventory)
	s.updateOrderStatus(ctx, input.OrderID, "RESERVING")

	reservationID, err := s.reserveInventory(ctx, input)
	if err != nil {
		return s.fail(ctx, sagaID, input.OrderID, "", "reserve inventory failed", err)
	}
	s.setReservationID(ctx, sagaID, reservationID)
	s.log.Info("inventory reserved", slog.String("order_id", input.OrderID), slog.String("reservation_id", reservationID))

	// Step 2: Process Payment
	s.updateStep(ctx, sagaID, StepProcessPayment)
	s.updateOrderStatus(ctx, input.OrderID, "PAYING")

	if err := s.processPayment(ctx, input.OrderID); err != nil {
		s.compensateReserve(ctx, reservationID, input.OrderID)
		return s.fail(ctx, sagaID, input.OrderID, reservationID, "payment failed", err)
	}
	s.log.Info("payment processed", slog.String("order_id", input.OrderID))

	// Step 3: Confirm (decrement inventory)
	s.updateStep(ctx, sagaID, StepConfirmOrder)
	s.updateOrderStatus(ctx, input.OrderID, "CONFIRMING")

	if err := s.confirmOrder(ctx, reservationID); err != nil {
		s.compensateReserve(ctx, reservationID, input.OrderID)
		return s.fail(ctx, sagaID, input.OrderID, reservationID, "confirm failed", err)
	}

	// Success
	s.updateOrderStatus(ctx, input.OrderID, OrderStatusCompleted)
	s.completeSaga(ctx, sagaID)
	s.log.Info("saga completed", slog.String("order_id", input.OrderID), slog.String("saga_id", sagaID))

	return nil
}

func (s *SagaOrchestrator) Resume(ctx context.Context) error {
	var sagas []SagaState
	err := s.db.SelectContext(ctx, &sagas, `SELECT * FROM saga_state WHERE status = $1`, SagaStatusInProgress)
	if err != nil {
		return fmt.Errorf("query in-progress sagas: %w", err)
	}

	for _, saga := range sagas {
		s.log.Warn("resuming interrupted saga", slog.String("saga_id", saga.ID), slog.String("order_id", saga.OrderID))
		if saga.ReservationID.Valid {
			s.compensateReserve(ctx, saga.ReservationID.String, saga.OrderID)
		}
		s.fail(ctx, saga.ID, saga.OrderID, "", "interrupted by restart", nil)
	}
	return nil
}

// --- Saga Steps ---

func (s *SagaOrchestrator) reserveInventory(ctx context.Context, input SagaInput) (string, error) {
	items := make([]*inventoryv1.StockItem, len(input.Items))
	for i, item := range input.Items {
		items[i] = &inventoryv1.StockItem{ProductId: item.ProductID, Quantity: item.Quantity}
	}
	resp, err := s.inventoryClient.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
		OrderId: input.OrderID, Items: items,
	})
	if err != nil {
		return "", err
	}
	return resp.ReservationId, nil
}

func (s *SagaOrchestrator) confirmOrder(ctx context.Context, reservationID string) error {
	_, err := s.inventoryClient.DecrementStock(ctx, &inventoryv1.DecrementStockRequest{
		ReservationId: reservationID,
	})
	return err
}

// --- Compensation ---

func (s *SagaOrchestrator) compensateReserve(ctx context.Context, reservationID, orderID string) {
	if reservationID == "" {
		return
	}
	_, err := s.inventoryClient.ReleaseStock(ctx, &inventoryv1.ReleaseStockRequest{
		ReservationId: reservationID,
	})
	if err != nil {
		s.log.Error("compensation failed: release stock", slog.String("order_id", orderID), slog.String("error", err.Error()))
	} else {
		s.log.Info("compensation: inventory released", slog.String("order_id", orderID), slog.String("reservation_id", reservationID))
	}
}

// --- State Persistence ---

func (s *SagaOrchestrator) getExistingSaga(ctx context.Context, orderID string) (*SagaState, error) {
	var saga SagaState
	err := s.db.GetContext(ctx, &saga, `SELECT * FROM saga_state WHERE order_id = $1 ORDER BY created_at DESC LIMIT 1`, orderID)
	if err != nil {
		return nil, err
	}
	return &saga, nil
}

func (s *SagaOrchestrator) createSagaState(ctx context.Context, orderID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO saga_state (order_id, current_step, status) VALUES ($1, $2, $3) RETURNING id`,
		orderID, string(StepReserveInventory), SagaStatusInProgress).Scan(&id)
	return id, err
}

func (s *SagaOrchestrator) updateStep(ctx context.Context, sagaID string, step SagaStep) {
	s.db.ExecContext(ctx, `UPDATE saga_state SET current_step = $1, updated_at = NOW() WHERE id = $2`, string(step), sagaID)
}

func (s *SagaOrchestrator) setReservationID(ctx context.Context, sagaID, reservationID string) {
	s.db.ExecContext(ctx, `UPDATE saga_state SET reservation_id = $1, updated_at = NOW() WHERE id = $2`, reservationID, sagaID)
}

func (s *SagaOrchestrator) updateOrderStatus(ctx context.Context, orderID, status string) {
	s.db.ExecContext(ctx, `UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`, status, orderID)
}

func (s *SagaOrchestrator) completeSaga(ctx context.Context, sagaID string) {
	s.db.ExecContext(ctx, `UPDATE saga_state SET status = $1, updated_at = NOW() WHERE id = $2`, SagaStatusCompleted, sagaID)
}

func (s *SagaOrchestrator) fail(ctx context.Context, sagaID, orderID, reservationID, reason string, originalErr error) error {
	msg := reason
	if originalErr != nil {
		msg = fmt.Sprintf("%s: %v", reason, originalErr)
	}
	s.db.ExecContext(ctx, `UPDATE saga_state SET status = $1, failure_reason = $2, updated_at = NOW() WHERE id = $3`, SagaStatusFailed, msg, sagaID)
	s.db.ExecContext(ctx, `UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`, OrderStatusFailed, orderID)
	s.log.Error("saga failed", slog.String("saga_id", sagaID), slog.String("order_id", orderID), slog.String("reservation_id", reservationID), slog.String("reason", msg))
	return fmt.Errorf("saga failed: %s", msg)
}
