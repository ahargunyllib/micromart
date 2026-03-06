#!/usr/bin/env bash
set -euo pipefail

USAGE="Usage: $0 <service> <command> [arg]
  service: order | inventory
  command: create <name>  — create a new migration pair
           up             — apply all pending migrations
           down           — roll back the last migration
           force <version> — force set the migration version

Examples:
  $0 order create add_orders_table
  $0 inventory up
  $0 order down
  $0 order force 1"

if [[ $# -lt 2 ]]; then
  echo "$USAGE"
  exit 1
fi

SERVICE="$1"
COMMAND="$2"
ARG="${3:-}"

# --- Map service to database URL ---
case "$SERVICE" in
  order)
    DB_URL="postgres://micromart:micromart@postgres-order:5432/order_db?sslmode=disable"
    ;;
  inventory)
    DB_URL="postgres://micromart:micromart@postgres-inventory:5432/inventory_db?sslmode=disable"
    ;;
  *)
    echo "Error: unknown service '$SERVICE'. Must be 'order' or 'inventory'."
    exit 1
    ;;
esac

MIGRATIONS_DIR="migrations/${SERVICE}"
MIGRATE_IMAGE="migrate/migrate:latest"

# --- Route command ---
case "$COMMAND" in
  create)
    if [[ -z "$ARG" ]]; then
      echo "Error: 'create' requires a migration name."
      echo "  $0 $SERVICE create <name>"
      exit 1
    fi
    mkdir -p "$MIGRATIONS_DIR"
    docker run --rm \
      -v "$(pwd)/${MIGRATIONS_DIR}:/migrations" \
      "$MIGRATE_IMAGE" \
      create -ext sql -dir /migrations -seq "$ARG"
    echo "Created migration in ${MIGRATIONS_DIR}/"
    ;;

  up)
    docker run --rm \
      --network micromart_default \
      -v "$(pwd)/${MIGRATIONS_DIR}:/migrations" \
      "$MIGRATE_IMAGE" \
      -path /migrations -database "$DB_URL" up
    ;;

  down)
    docker run --rm \
      --network micromart_default \
      -v "$(pwd)/${MIGRATIONS_DIR}:/migrations" \
      "$MIGRATE_IMAGE" \
      -path /migrations -database "$DB_URL" down 1
    ;;

  force)
    if [[ -z "$ARG" ]]; then
      echo "Error: 'force' requires a version number."
      echo "  $0 $SERVICE force <version>"
      exit 1
    fi
    docker run --rm \
      --network micromart_default \
      -v "$(pwd)/${MIGRATIONS_DIR}:/migrations" \
      "$MIGRATE_IMAGE" \
      -path /migrations -database "$DB_URL" force "$ARG"
    ;;

  *)
    echo "Error: unknown command '$COMMAND'. Must be one of: create, up, down, force."
    exit 1
    ;;
esac
