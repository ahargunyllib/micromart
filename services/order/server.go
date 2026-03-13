package main

import (
	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"
	metricspkg "github.com/ahargunyllib/micromart/pkg/metrics"
	redispkg "github.com/ahargunyllib/micromart/pkg/redis"
)

type Server struct {
	orderv1.UnimplementedOrderServiceServer
	repo            *Repository
	inventoryClient inventoryv1.InventoryServiceClient
	saga            *SagaOrchestrator
	redis           *redispkg.Client
	metrics         *metricspkg.Metrics
}

func NewServer(
	repo *Repository,
	inventoryClient inventoryv1.InventoryServiceClient,
	saga *SagaOrchestrator,
	redis *redispkg.Client,
	metrics *metricspkg.Metrics,
) *Server {
	return &Server{
		repo:            repo,
		inventoryClient: inventoryClient,
		saga:            saga,
		redis:           redis,
		metrics:         metrics,
	}
}
