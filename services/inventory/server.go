package main

import (
	inventoryv1 "github.com/ahargunyllib/micromart/gen/inventory/v1"
)

type Server struct {
	inventoryv1.UnimplementedInventoryServiceServer
	repo *Repository
}

func NewServer(repo *Repository) *Server {
	return &Server{repo: repo}
}
