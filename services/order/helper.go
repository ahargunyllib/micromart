package main

import (
	"encoding/base64"

	orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"
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

func statusToProto(s string) orderv1.OrderStatus {
	switch s {
	case OrderStatusPending:
		return orderv1.OrderStatus_ORDER_STATUS_PENDING
	case "RESERVING":
		return orderv1.OrderStatus_ORDER_STATUS_RESERVING
	case "PAYING":
		return orderv1.OrderStatus_ORDER_STATUS_PAYING
	case "CONFIRMING":
		return orderv1.OrderStatus_ORDER_STATUS_CONFIRMING
	case OrderStatusCompleted:
		return orderv1.OrderStatus_ORDER_STATUS_COMPLETED
	case OrderStatusFailed:
		return orderv1.OrderStatus_ORDER_STATUS_FAILED
	case OrderStatusCancelled:
		return orderv1.OrderStatus_ORDER_STATUS_CANCELLED
	default:
		return orderv1.OrderStatus_ORDER_STATUS_UNSPECIFIED
	}
}

func orderToProto(order *Order, items []OrderItem, enriched []CreateOrderItemParams) *orderv1.Order {
	protoItems := make([]*orderv1.OrderItem, len(items))

	nameMap := make(map[string]string)
	for _, e := range enriched {
		nameMap[e.ProductID] = e.ProductName
	}

	for i, item := range items {
		protoItems[i] = &orderv1.OrderItem{
			ProductId:      item.ProductID,
			ProductName:    nameMap[item.ProductID],
			Quantity:       item.Quantity,
			UnitPriceCents: item.UnitPriceCents,
		}
	}

	return &orderv1.Order{
		Id:         order.ID,
		CustomerId: order.CustomerID,
		Status:     statusToProto(order.Status),
		Items:      protoItems,
		TotalCents: order.TotalCents,
		CreatedAt:  timestamppb.New(order.CreatedAt),
		UpdatedAt:  timestamppb.New(order.UpdatedAt),
	}
}
