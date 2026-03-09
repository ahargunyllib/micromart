package main

import (
	"encoding/base64"
	"strings"

	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func encodeCursor(id string) string {
	return base64.StdEncoding.EncodeToString([]byte(id))
}

func decodeCursor(token string) (string, error) {
	if token == "" {
		return "", nil
	}

	b, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

func joinStrings(strs []string, sep string) string {
	var result strings.Builder
	for i, s := range strs {
		if i > 0 {
			result.WriteString(sep)
		}
		result.WriteString(s)
	}

	return result.String()
}

func productToProto(p *Product) *inventoryv1.Product {
	return &inventoryv1.Product{
		Id:             p.ID,
		Name:           p.Name,
		Description:    p.Description,
		Category:       p.Category,
		PriceCents:     p.PriceCents,
		StockAvailable: p.StockAvailable,
		StockReserved:  p.StockReserved,
		Active:         p.Active,
		CreatedAt:      timestamppb.New(p.CreatedAt),
		UpdatedAt:      timestamppb.New(p.UpdatedAt),
	}
}
