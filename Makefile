.PHONY: up down logs proto proto-lint lint test build build-gateway build-order build-inventory build-migrations build-migrations-order build-migrations-inventory run-gateway run-order run-inventory migrate-up migrate-down load-test k8s-apply k8s-delete k8s-migrate-up k8s-migrate-status k8s-migrate-logs-order k8s-migrate-logs-inventory k8s-migrate-clean k8s-port-forward-grafana k8s-port-forward-prometheus k8s-port-forward-jaeger k8s-port-forward-gateway k8s-port-forward-postgres-order k8s-port-forward-postgres-inventory k8s-port-forward-redis k8s-port-forward-clickhouse k8s-status k8s-logs-gateway k8s-logs-order k8s-logs-inventory

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

build-migrations-order:
	docker build -t micromart/migrations-order:latest migrations/order

build-migrations-inventory:
	docker build -t micromart/migrations-inventory:latest migrations/inventory

build-migrations: build-migrations-order build-migrations-inventory

build: build-gateway build-order build-inventory build-migrations

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

# Migrations
k8s-migrate-up:
	kubectl apply -f deploy/k8s/migrations-order.yaml
	kubectl apply -f deploy/k8s/migrations-inventory.yaml

k8s-migrate-status:
	@echo "Order migration status:"
	@kubectl get job migrations-order -n micromart -o jsonpath='{.status}' 2>/dev/null || echo "Job not found"
	@echo "\n\nInventory migration status:"
	@kubectl get job migrations-inventory -n micromart -o jsonpath='{.status}' 2>/dev/null || echo "Job not found"

k8s-migrate-logs-order:
	kubectl logs -n micromart -l app=migrations-order --tail=100 -f

k8s-migrate-logs-inventory:
	kubectl logs -n micromart -l app=migrations-inventory --tail=100 -f

k8s-migrate-clean:
	kubectl delete job migrations-order migrations-inventory -n micromart --ignore-not-found=true

# Port forwarding for local access to K8s services
k8s-port-forward-grafana:
	kubectl port-forward -n micromart svc/grafana 3000:3000

k8s-port-forward-prometheus:
	kubectl port-forward -n micromart svc/prometheus 9090:9090

k8s-port-forward-jaeger:
	kubectl port-forward -n micromart svc/jaeger 16686:16686

k8s-port-forward-gateway:
	kubectl port-forward -n micromart svc/gateway 8080:8080

k8s-port-forward-postgres-order:
	kubectl port-forward -n micromart svc/postgres-order 5432:5432

k8s-port-forward-postgres-inventory:
	kubectl port-forward -n micromart svc/postgres-inventory 5433:5432

k8s-port-forward-redis:
	kubectl port-forward -n micromart svc/redis 6379:6379

k8s-port-forward-clickhouse:
	kubectl port-forward -n micromart svc/clickhouse 8123:8123 9000:9000

# Get status of all K8s resources
k8s-status:
	kubectl get all -n micromart

# Get logs from services
k8s-logs-gateway:
	kubectl logs -n micromart -l app=gateway -f

k8s-logs-order:
	kubectl logs -n micromart -l app=order-service -f

k8s-logs-inventory:
	kubectl logs -n micromart -l app=inventory-service -f
