package main

import (
	"time"

	"database/sql"
)

type Order struct {
	ID             string         `db:"id"`
	CustomerID     string         `db:"customer_id"`
	Status         string         `db:"status"`
	TotalCents     int64          `db:"total_cents"`
	IdempotencyKey sql.NullString `db:"idempotency_key"`
	CreatedAt      time.Time      `db:"created_at"`
	UpdatedAt      time.Time      `db:"updated_at"`
}

type OrderItem struct {
	ID             string    `db:"id"`
	OrderID        string    `db:"order_id"`
	ProductID      string    `db:"product_id"`
	Quantity       int32     `db:"quantity"`
	UnitPriceCents int64     `db:"unit_price_cents"`
	CreatedAt      time.Time `db:"created_at"`
}

type SagaState struct {
	ID            string         `db:"id"`
	OrderID       string         `db:"order_id"`
	CurrentStep   string         `db:"current_step"`
	Status        string         `db:"status"`
	ReservationID sql.NullString `db:"reservation_id"`
	FailureReason sql.NullString `db:"failure_reason"`
	CreatedAt     time.Time      `db:"created_at"`
	UpdatedAt     time.Time      `db:"updated_at"`
}

const (
	OrderStatusPending   = "PENDING"
	OrderStatusCompleted = "COMPLETED"
	OrderStatusFailed    = "FAILED"
	OrderStatusCancelled = "CANCELLED"

	SagaStatusInProgress = "IN_PROGRESS"
	SagaStatusCompleted  = "COMPLETED"
	SagaStatusFailed     = "FAILED"
)
