package main

import (
	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
	orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"
)

type Server struct {
	orderv1.UnimplementedOrderServiceServer
	repo            *Repository
	inventoryClient inventoryv1.InventoryServiceClient
}

func NewServer(repo *Repository, inventoryClient inventoryv1.InventoryServiceClient) *Server {
	return &Server{
		repo:            repo,
		inventoryClient: inventoryClient,
	}
}
