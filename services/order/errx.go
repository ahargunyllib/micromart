package main

import "errors"

var (
	ErrNotFound       = errors.New("not found")
	ErrDuplicateOrder = errors.New("duplicate order")
)
