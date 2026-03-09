package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type CreateProductParams struct {
	Name         string
	Description  string
	Category     string
	PriceCents   int64
	InitialStock int32
}

func (r *Repository) CreateProduct(ctx context.Context, p CreateProductParams) (*Product, error) {
	var product Product
	err := r.db.QueryRowxContext(ctx, `
		INSERT INTO products (name, description, category, price_cents, stock_available)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING *`,
		p.Name, p.Description, p.Category, p.PriceCents, p.InitialStock,
	).StructScan(&product)

	if err != nil {
		return nil, fmt.Errorf("create product: %w", err)
	}

	return &product, nil
}

func (r *Repository) GetProduct(ctx context.Context, id string) (*Product, error) {
	var product Product
	err := r.db.GetContext(ctx, &product, `SELECT * FROM products WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get product: %w", err)
	}

	return &product, nil
}

type UpdateProductParams struct {
	Name        *string
	Description *string
	Category    *string
	PriceCents  *int64
	Active      *bool
}

func (r *Repository) UpdateProduct(ctx context.Context, id string, p UpdateProductParams) (*Product, error) {
	// Build dynamic SET clause for partial updates
	setClauses := []string{}
	args := []any{}
	argIdx := 1

	if p.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *p.Name)
		argIdx++
	}
	if p.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIdx))
		args = append(args, *p.Description)
		argIdx++
	}
	if p.Category != nil {
		setClauses = append(setClauses, fmt.Sprintf("category = $%d", argIdx))
		args = append(args, *p.Category)
		argIdx++
	}
	if p.PriceCents != nil {
		setClauses = append(setClauses, fmt.Sprintf("price_cents = $%d", argIdx))
		args = append(args, *p.PriceCents)
		argIdx++
	}
	if p.Active != nil {
		setClauses = append(setClauses, fmt.Sprintf("active = $%d", argIdx))
		args = append(args, *p.Active)
		argIdx++
	}

	if len(setClauses) == 0 {
		return r.GetProduct(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf(`UPDATE products SET %s WHERE id = $%d RETURNING *`,
		joinStrings(setClauses, ", "), argIdx)

	var product Product
	err := r.db.QueryRowxContext(ctx, query, args...).StructScan(&product)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("update product: %w", err)
	}

	return &product, nil
}

func (r *Repository) ListProducts(ctx context.Context, category string, pageSize int32, pageToken string) ([]Product, string, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	var products []Product
	var err error

	cursor, _ := decodeCursor(pageToken)

	if category != "" && cursor != "" {
		err = r.db.SelectContext(ctx, &products, `
			SELECT * FROM products
			WHERE category = $1 AND active = TRUE AND id > $2
			ORDER BY id
			LIMIT $3`,
			category, cursor, pageSize+1)
	} else if category != "" {
		err = r.db.SelectContext(ctx, &products, `
			SELECT * FROM products
			WHERE category = $1 AND active = TRUE
			ORDER BY id
			LIMIT $2`,
			category, pageSize+1)
	} else if cursor != "" {
		err = r.db.SelectContext(ctx, &products, `
			SELECT * FROM products
			WHERE active = TRUE AND id > $1
			ORDER BY id
			LIMIT $2`,
			cursor, pageSize+1)
	} else {
		err = r.db.SelectContext(ctx, &products, `
			SELECT * FROM products
			WHERE active = TRUE
			ORDER BY id
			LIMIT $1`,
			pageSize+1)
	}

	if err != nil {
		return nil, "", fmt.Errorf("list products: %w", err)
	}

	var nextToken string
	if int32(len(products)) > pageSize {
		products = products[:pageSize]
		nextToken = encodeCursor(products[len(products)-1].ID)
	}

	return products, nextToken, nil
}

func (r *Repository) SearchProducts(ctx context.Context, query string, pageSize int32, pageToken string) ([]Product, string, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	cursor, _ := decodeCursor(pageToken)

	var products []Product
	var err error

	if cursor != "" {
		err = r.db.SelectContext(ctx, &products, `
			SELECT * FROM products
			WHERE to_tsvector('english', name) @@ plainto_tsquery('english', $1)
				AND active = TRUE AND id > $2
			ORDER BY id
			LIMIT $3`,
			query, cursor, pageSize+1)
	} else {
		err = r.db.SelectContext(ctx, &products, `
			SELECT * FROM products
			WHERE to_tsvector('english', name) @@ plainto_tsquery('english', $1)
				AND active = TRUE
			ORDER BY id
			LIMIT $2`,
			query, pageSize+1)
	}

	if err != nil {
		return nil, "", fmt.Errorf("search products: %w", err)
	}

	var nextToken string
	if int32(len(products)) > pageSize {
		products = products[:pageSize]
		nextToken = encodeCursor(products[len(products)-1].ID)
	}

	return products, nextToken, nil
}
