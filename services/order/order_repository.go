package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type CreateOrderParams struct {
	CustomerID     string
	IdempotencyKey string
	Items          []CreateOrderItemParams
}

type CreateOrderItemParams struct {
	ProductID      string
	Quantity       int32
	UnitPriceCents int64
	ProductName    string
}

func (r *Repository) CreateOrder(ctx context.Context, p CreateOrderParams) (*Order, []OrderItem, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if p.IdempotencyKey != "" {
		var existing Order
		err := tx.GetContext(ctx, &existing, `
			SELECT * FROM orders WHERE idempotency_key = $1`,
			p.IdempotencyKey)
		if err == nil {
			var items []OrderItem
			err = tx.SelectContext(ctx, &items, `
				SELECT * FROM order_items WHERE order_id = $1 ORDER BY created_at`,
				existing.ID)
			if err != nil {
				return nil, nil, fmt.Errorf("get existing items: %w", err)
			}

			return &existing, items, ErrDuplicateOrder
		}

		if !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, fmt.Errorf("check idempotency: %w", err)
		}
	}

	var totalCents int64
	for _, item := range p.Items {
		totalCents += item.UnitPriceCents * int64(item.Quantity)
	}

	var order Order
	idempotencyKey := sql.NullString{}
	if p.IdempotencyKey != "" {
		idempotencyKey = sql.NullString{String: p.IdempotencyKey, Valid: true}
	}

	err = tx.QueryRowxContext(ctx, `
		INSERT INTO orders (customer_id, status, total_cents, idempotency_key)
		VALUES ($1, $2, $3, $4)
		RETURNING *`,
		p.CustomerID, OrderStatusPending, totalCents, idempotencyKey,
	).StructScan(&order)
	if err != nil {
		return nil, nil, fmt.Errorf("insert order: %w", err)
	}

	items := make([]OrderItem, 0, len(p.Items))
	for _, item := range p.Items {
		var orderItem OrderItem
		err = tx.QueryRowxContext(ctx, `
			INSERT INTO order_items (order_id, product_id, quantity, unit_price_cents)
			VALUES ($1, $2, $3, $4)
			RETURNING *`,
			order.ID, item.ProductID, item.Quantity, item.UnitPriceCents,
		).StructScan(&orderItem)
		if err != nil {
			return nil, nil, fmt.Errorf("insert order item: %w", err)
		}
		items = append(items, orderItem)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit: %w", err)
	}

	return &order, items, nil
}

func (r *Repository) GetOrder(ctx context.Context, id string) (*Order, []OrderItem, error) {
	var order Order
	err := r.db.GetContext(ctx, &order, `SELECT * FROM orders WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get order: %w", err)
	}

	var items []OrderItem
	err = r.db.SelectContext(ctx, &items, `
		SELECT * FROM order_items WHERE order_id = $1 ORDER BY created_at`, id)
	if err != nil {
		return nil, nil, fmt.Errorf("get order items: %w", err)
	}

	return &order, items, nil
}

func (r *Repository) ListOrders(ctx context.Context, customerID string, pageSize int32, pageToken string) ([]Order, string, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	cursor, _ := decodeCursor(pageToken)

	var orders []Order
	var err error

	if cursor != "" {
		err = r.db.SelectContext(ctx, &orders, `
			SELECT * FROM orders
			WHERE customer_id = $1 AND id > $2
			ORDER BY id
			LIMIT $3`,
			customerID, cursor, pageSize+1)
	} else {
		err = r.db.SelectContext(ctx, &orders, `
			SELECT * FROM orders
			WHERE customer_id = $1
			ORDER BY id
			LIMIT $2`,
			customerID, pageSize+1)
	}

	if err != nil {
		return nil, "", fmt.Errorf("list orders: %w", err)
	}

	var nextToken string
	if int32(len(orders)) > pageSize {
		orders = orders[:pageSize]
		nextToken = encodeCursor(orders[len(orders)-1].ID)
	}

	return orders, nextToken, nil
}

func (r *Repository) UpdateOrderStatus(ctx context.Context, id string, status string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`,
		status, id)
	if err != nil {
		return fmt.Errorf("update order status: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) CancelOrder(ctx context.Context, id string) (*Order, []OrderItem, error) {
	var order Order
	err := r.db.GetContext(ctx, &order, `SELECT * FROM orders WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get order: %w", err)
	}

	if order.Status != OrderStatusPending && order.Status != OrderStatusFailed {
		return nil, nil, fmt.Errorf("cannot cancel order with status %s", order.Status)
	}

	err = r.db.GetContext(ctx, &order, `
		UPDATE orders SET status = $1, updated_at = NOW()
		WHERE id = $2
		RETURNING *`,
		OrderStatusCancelled, id)
	if err != nil {
		return nil, nil, fmt.Errorf("cancel order: %w", err)
	}

	var items []OrderItem
	err = r.db.SelectContext(ctx, &items, `
		SELECT * FROM order_items WHERE order_id = $1 ORDER BY created_at`, id)
	if err != nil {
		return nil, nil, fmt.Errorf("get order items: %w", err)
	}

	return &order, items, nil
}
