package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	metricspkg "github.com/ahargunyllib/micromart/pkg/metrics"
	"github.com/jmoiron/sqlx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
	metrics         *metricspkg.Metrics
	clickhouse      *ClickHouseClient
}

func NewSagaOrchestrator(
	db *sqlx.DB,
	inventoryClient inventoryv1.InventoryServiceClient,
	log *slog.Logger,
	m *metricspkg.Metrics,
	ch *ClickHouseClient,
) *SagaOrchestrator {
	return &SagaOrchestrator{
		db:              db,
		inventoryClient: inventoryClient,
		log:             log,
		processPayment: func(ctx context.Context, orderID string) error {
			return nil // stub: always succeeds
		},
		metrics:    m,
		clickhouse: ch,
	}
}

type SagaInput struct {
	OrderID    string
	CustomerID string
	TotalCents int64
	ItemCount  int32
	Items      []SagaItem
	CreatedAt  time.Time
}

type SagaItem struct {
	ProductID string
	Quantity  int32
}

func (s *SagaOrchestrator) Execute(ctx context.Context, input SagaInput) error {
	start := time.Now()

	// Start a trace span for the entire saga
	tracer := otel.Tracer("order-service")
	ctx, span := tracer.Start(ctx, "saga.Execute",
		trace.WithAttributes(
			attribute.String("order.id", input.OrderID),
			attribute.Int("order.item_count", int(input.ItemCount)),
		),
	)
	defer span.End()

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
		s.recordResult("error", time.Since(start))
		return fmt.Errorf("check existing saga: %w", err)
	}
	// No existing saga found, proceed with creation

	sagaID, err := s.createSagaState(ctx, input.OrderID)
	if err != nil {
		s.recordResult("error", time.Since(start))
		return fmt.Errorf("create saga state: %w", err)
	}

	s.log.Info("saga started", slog.String("order_id", input.OrderID), slog.String("saga_id", sagaID))

	// Step 1: Reserve Inventory
	s.updateStep(ctx, sagaID, StepReserveInventory)
	s.updateOrderStatus(ctx, input.OrderID, "RESERVING")

	reservationID, err := s.reserveInventory(ctx, input)
	if err != nil {
		span.SetStatus(otelcodes.Error, "reserve failed")
		s.recordResult("failed", time.Since(start))
		return s.fail(ctx, sagaID, input.OrderID, "", "reserve inventory failed", err)
	}
	s.setReservationID(ctx, sagaID, reservationID)
	span.AddEvent("inventory_reserved", trace.WithAttributes(attribute.String("reservation.id", reservationID)))

	// Step 2: Process Payment
	s.updateStep(ctx, sagaID, StepProcessPayment)
	s.updateOrderStatus(ctx, input.OrderID, "PAYING")

	if err := s.processPayment(ctx, input.OrderID); err != nil {
		s.compensateReserve(ctx, reservationID, input.OrderID)
		span.SetStatus(otelcodes.Error, "payment failed")
		s.recordResult("failed", time.Since(start))
		return s.fail(ctx, sagaID, input.OrderID, reservationID, "payment failed", err)
	}
	span.AddEvent("payment_processed", trace.WithAttributes(attribute.String("order.id", input.OrderID)))

	// Step 3: Confirm (decrement inventory)
	s.updateStep(ctx, sagaID, StepConfirmOrder)
	s.updateOrderStatus(ctx, input.OrderID, "CONFIRMING")

	if err := s.confirmOrder(ctx, reservationID); err != nil {
		s.compensateReserve(ctx, reservationID, input.OrderID)
		span.SetStatus(otelcodes.Error, "confirm failed")
		s.recordResult("failed", time.Since(start))
		return s.fail(ctx, sagaID, input.OrderID, reservationID, "confirm failed", err)
	}

	// Success
	s.updateOrderStatus(ctx, input.OrderID, OrderStatusCompleted)
	s.completeSaga(ctx, sagaID)

	span.SetStatus(otelcodes.Ok, "")
	s.recordResult("completed", time.Since(start))

	// Publish analytics event
	if s.clickhouse != nil {
		s.clickhouse.Publish(OrderEvent{
			OrderID:     input.OrderID,
			CustomerID:  input.CustomerID,
			Status:      OrderStatusCompleted,
			TotalCents:  input.TotalCents,
			ItemCount:   input.ItemCount,
			CreatedAt:   input.CreatedAt,
			CompletedAt: time.Now(),
		})
	}

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

func (s *SagaOrchestrator) recordResult(result string, duration time.Duration) {
	if s.metrics == nil {
		return
	}
	s.metrics.SagaDuration.WithLabelValues(result).Observe(duration.Seconds())
	s.metrics.SagaResultTotal.WithLabelValues(result).Inc()
}

// --- Saga Steps ---

func (s *SagaOrchestrator) reserveInventory(ctx context.Context, input SagaInput) (string, error) {
	_, span := otel.Tracer("order-service").Start(ctx, "saga.ReserveInventory")
	defer span.End()

	items := make([]*inventoryv1.StockItem, len(input.Items))
	for i, item := range input.Items {
		items[i] = &inventoryv1.StockItem{ProductId: item.ProductID, Quantity: item.Quantity}
	}
	resp, err := s.inventoryClient.ReserveStock(ctx, &inventoryv1.ReserveStockRequest{
		OrderId: input.OrderID, Items: items,
	})
	if err != nil {
		span.SetStatus(otelcodes.Error, err.Error())
		return "", err
	}
	span.SetAttributes(attribute.String("reservation.id", resp.ReservationId))
	return resp.ReservationId, nil
}

func (s *SagaOrchestrator) confirmOrder(ctx context.Context, reservationID string) error {
	_, span := otel.Tracer("order-service").Start(ctx, "saga.ConfirmOrder")
	defer span.End()

	_, err := s.inventoryClient.DecrementStock(ctx, &inventoryv1.DecrementStockRequest{
		ReservationId: reservationID,
	})
	if err != nil {
		span.SetStatus(otelcodes.Error, err.Error())
	}
	return err
}

// --- Compensation ---

func (s *SagaOrchestrator) compensateReserve(ctx context.Context, reservationID, orderID string) {
	if reservationID == "" {
		return
	}

	_, span := otel.Tracer("order-service").Start(ctx, "saga.CompensateReserve")
	defer span.End()

	_, err := s.inventoryClient.ReleaseStock(ctx, &inventoryv1.ReleaseStockRequest{
		ReservationId: reservationID,
	})
	if err != nil {
		span.SetStatus(otelcodes.Error, err.Error())
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
