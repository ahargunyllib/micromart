package main

import (
	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"
	redispkg "github.com/ahargunyllib/micromart/pkg/redis"
)

type Server struct {
	orderv1.UnimplementedOrderServiceServer
	repo            *Repository
	inventoryClient inventoryv1.InventoryServiceClient
	saga            *SagaOrchestrator
	redis           *redispkg.Client
}

func NewServer(repo *Repository, inventoryClient inventoryv1.InventoryServiceClient, saga *SagaOrchestrator, redis *redispkg.Client) *Server {
	return &Server{repo: repo, inventoryClient: inventoryClient, saga: saga, redis: redis}
}
