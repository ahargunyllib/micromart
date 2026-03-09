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
