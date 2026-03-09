.PHONY: up down logs proto proto-lint migrate-up migrate-down

up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f

proto:
	buf generate

proto-lint:
	buf lint

migrate-up-order:
	./scripts/migrate.sh order up

migrate-up-inventory:
	./scripts/migrate.sh inventory up

migrate-up: migrate-up-order migrate-up-inventory

migrate-down-order:
	./scripts/migrate.sh order down

migrate-down-inventory:
	./scripts/migrate.sh inventory down

run-gateway:
	set -a && . services/gateway/.env && set +a && go run ./services/gateway

run-order:
	set -a && . services/order/.env && set +a && go run ./services/order

run-inventory:
	set -a && . services/inventory/.env && set +a && go run ./services/inventory

test-order:
	TESTCONTAINERS_RYUK_DISABLED=true go test -v ./services/order/

test-inventory:
	TESTCONTAINERS_RYUK_DISABLED=true go test -v ./services/inventory/
