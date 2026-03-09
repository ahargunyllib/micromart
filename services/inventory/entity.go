package main

import "time"

type Product struct {
	ID             string    `db:"id"`
	Name           string    `db:"name"`
	Description    string    `db:"description"`
	Category       string    `db:"category"`
	PriceCents     int64     `db:"price_cents"`
	StockAvailable int32     `db:"stock_available"`
	StockReserved  int32     `db:"stock_reserved"`
	Active         bool      `db:"active"`
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
}

type StockReservation struct {
	ID        string    `db:"id"`
	OrderID   string    `db:"order_id"`
	ProductID string    `db:"product_id"`
	Quantity  int32     `db:"quantity"`
	Status    string    `db:"status"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

const (
	ReservationStatusReserved    = "RESERVED"
	ReservationStatusReleased    = "RELEASED"
	ReservationStatusDecremented = "DECREMENTED"
)
