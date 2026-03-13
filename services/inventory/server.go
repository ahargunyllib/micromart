package main

import (
	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	metricspkg "github.com/ahargunyllib/micromart/pkg/metrics"
)

type Server struct {
	inventoryv1.UnimplementedInventoryServiceServer
	repo    *Repository
	metrics *metricspkg.Metrics
}

func NewServer(repo *Repository, metrics *metricspkg.Metrics) *Server {
	return &Server{repo: repo, metrics: metrics}
}
