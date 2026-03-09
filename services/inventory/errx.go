package main

import "errors"

var (
	ErrNotFound           = errors.New("not found")
	ErrInsufficientStock  = errors.New("insufficient stock")
	ErrInvalidReservation = errors.New("invalid reservation")
)
