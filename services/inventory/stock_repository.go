package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type StockItem struct {
	ProductID string
	Quantity  int32
}

func (r *Repository) ReserveStock(ctx context.Context, orderID string, items []StockItem) (string, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	for _, item := range items {
		// Lock the product row and check stock
		var product Product
		err := tx.QueryRowxContext(ctx, `
			SELECT * FROM products WHERE id = $1 FOR UPDATE`,
			item.ProductID,
		).StructScan(&product)
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("product %s: %w", item.ProductID, ErrNotFound)
		}
		if err != nil {
			return "", fmt.Errorf("lock product %s: %w", item.ProductID, err)
		}

		available := product.StockAvailable - product.StockReserved
		if available < item.Quantity {
			return "", fmt.Errorf("product %s: need %d, have %d: %w",
				item.ProductID, item.Quantity, available, ErrInsufficientStock)
		}

		// Update reserved count
		_, err = tx.ExecContext(ctx, `
			UPDATE products
			SET stock_reserved = stock_reserved + $1, updated_at = NOW()
			WHERE id = $2`,
			item.Quantity, item.ProductID)
		if err != nil {
			return "", fmt.Errorf("reserve stock for %s: %w", item.ProductID, err)
		}

		// Insert reservation record - let DB generate unique id, use order_id for grouping
		_, err = tx.ExecContext(ctx, `
			INSERT INTO stock_reservations (order_id, product_id, quantity, status)
			VALUES ($1, $2, $3, $4)`,
			orderID, item.ProductID, item.Quantity, ReservationStatusReserved)
		if err != nil {
			return "", fmt.Errorf("insert reservation for %s: %w", item.ProductID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit reservation: %w", err)
	}

	return orderID, nil
}

func (r *Repository) ReleaseStock(ctx context.Context, reservationID string) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var reservations []StockReservation
	err = tx.SelectContext(ctx, &reservations, `
		SELECT * FROM stock_reservations
		WHERE order_id = $1 AND status = $2
		FOR UPDATE`,
		reservationID, ReservationStatusReserved)
	if err != nil {
		return fmt.Errorf("get reservations: %w", err)
	}

	if len(reservations) == 0 {
		return ErrInvalidReservation
	}

	for _, res := range reservations {
		_, err = tx.ExecContext(ctx, `
			UPDATE products
			SET stock_reserved = stock_reserved - $1, updated_at = NOW()
			WHERE id = $2`,
			res.Quantity, res.ProductID)
		if err != nil {
			return fmt.Errorf("release stock for %s: %w", res.ProductID, err)
		}
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE stock_reservations
		SET status = $1, updated_at = NOW()
		WHERE order_id = $2 AND status = $3`,
		ReservationStatusReleased, reservationID, ReservationStatusReserved)
	if err != nil {
		return fmt.Errorf("update reservation status: %w", err)
	}

	return tx.Commit()
}

func (r *Repository) DecrementStock(ctx context.Context, reservationID string) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var reservations []StockReservation
	err = tx.SelectContext(ctx, &reservations, `
		SELECT * FROM stock_reservations
		WHERE order_id = $1 AND status = $2
		FOR UPDATE`,
		reservationID, ReservationStatusReserved)
	if err != nil {
		return fmt.Errorf("get reservations: %w", err)
	}

	if len(reservations) == 0 {
		return ErrInvalidReservation
	}

	for _, res := range reservations {
		_, err = tx.ExecContext(ctx, `
			UPDATE products
			SET stock_available = stock_available - $1,
				stock_reserved = stock_reserved - $1,
				updated_at = NOW()
			WHERE id = $2`,
			res.Quantity, res.ProductID)
		if err != nil {
			return fmt.Errorf("decrement stock for %s: %w", res.ProductID, err)
		}
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE stock_reservations
		SET status = $1, updated_at = NOW()
		WHERE order_id = $2 AND status = $3`,
		ReservationStatusDecremented, reservationID, ReservationStatusReserved)
	if err != nil {
		return fmt.Errorf("update reservation status: %w", err)
	}

	return tx.Commit()
}
