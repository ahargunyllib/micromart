.PHONY: up down logs proto proto-lint lint test build run-gateway run-order run-inventory migrate-up migrate-down load-test

# --- Docker ---
up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f

# --- Proto ---
proto:
	buf generate

proto-lint:
	buf lint

# --- Lint & Test ---
lint:
	golangci-lint-v2 run ./services/gateway/...
	golangci-lint-v2 run ./services/order/...
	golangci-lint-v2 run ./services/inventory/...
	golangci-lint-v2 run ./pkg/config/...
	golangci-lint-v2 run ./pkg/logger/...
	golangci-lint-v2 run ./pkg/grpcutil/...
	golangci-lint-v2 run ./pkg/metrics/...
	golangci-lint-v2 run ./pkg/otel/...
	golangci-lint-v2 run ./pkg/redis/...

fmt:
	golangci-lint-v2 fmt ./...

test:
	go test -v -race ./services/inventory/...
	go test -v -race ./services/order/...
	go test -v -race ./services/gateway/...

test-cover:
	go test -race -coverprofile=coverage.out ./services/inventory/... ./services/order/... ./services/gateway/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# --- Run (local dev) ---
run-gateway:
	set -a && . services/gateway/.env && set +a && go run ./services/gateway

run-order:
	set -a && . services/order/.env && set +a && go run ./services/order

run-inventory:
	set -a && . services/inventory/.env && set +a && go run ./services/inventory

# --- Migrations ---
migrate-up-order:
	./scripts/migrate.sh order up

migrate-up-inventory:
	./scripts/migrate.sh inventory up

migrate-up: migrate-up-order migrate-up-inventory

migrate-down-order:
	./scripts/migrate.sh order down

migrate-down-inventory:
	./scripts/migrate.sh inventory down

migrate-create-order:
	./scripts/migrate.sh order create $(name)

migrate-create-inventory:
	./scripts/migrate.sh inventory create $(name)

# --- Build (Docker) ---
build-gateway:
	docker build -f services/gateway/Dockerfile -t micromart/gateway:latest .

build-order:
	docker build -f services/order/Dockerfile -t micromart/order:latest .

build-inventory:
	docker build -f services/inventory/Dockerfile -t micromart/inventory:latest .

build: build-gateway build-order build-inventory

# --- Load Testing ---
load-test:
	k6 run tests/load/k6.js

load-test-quick:
	k6 run --duration=30s --vus=5 tests/load/k6.js

# --- K8s ---
k8s-apply:
	kubectl apply -f deploy/k8s/

k8s-delete:
	kubectl delete -f deploy/k8s/
