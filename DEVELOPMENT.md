# Development Guide

This guide covers everything you need to know for developing micromart locally.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Getting Started](#getting-started)
- [Code Organization](#code-organization)
- [Working with Proto Files](#working-with-proto-files)
- [Code Quality](#code-quality)
- [Adding New Services](#adding-new-services)
- [Database Migrations](#database-migrations)
- [Debugging](#debugging)
- [Environment Variables](#environment-variables)
- [Performance Profiling](#performance-profiling)

## Prerequisites

### Required

- **Go 1.25+** - [Install](https://go.dev/doc/install)
- **Docker & Docker Compose** - [Install](https://docs.docker.com/get-docker/)
- **buf** - [Install](https://buf.build/docs/installation) (for proto codegen)
- **golang-migrate** - [Install](https://github.com/golang-migrate/migrate) or use Docker

### Optional

- **k6** - [Install](https://k6.io/docs/getting-started/installation/) (for load testing)
- **delve** - [Install](https://github.com/go-delve/delve/tree/master/Documentation/installation) (for debugging)
- **kubectl** - [Install](https://kubernetes.io/docs/tasks/tools/) (for K8s deployments)

## Getting Started

### 1. Clone and Setup

```bash
# Clone the repository
git clone https://github.com/ahargunyllib/micromart.git
cd micromart

# Start infrastructure (PostgreSQL, Redis, ClickHouse, observability stack)
make up

# Run database migrations
make migrate-up

# Generate proto code (if gen/ is not present)
make proto
```

### 2. Run Services Locally

Open three terminals and run:

```bash
# Terminal 1: Inventory Service
make run-inventory    # gRPC :50052, metrics :9092

# Terminal 2: Order Service
make run-order        # gRPC :50051, metrics :9091

# Terminal 3: Gateway
make run-gateway      # HTTP :8080
```

### 3. Verify Everything Works

```bash
# Health check
curl http://localhost:8080/health

# Create a product
curl -X POST http://localhost:8080/api/v1/products \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Wireless Mouse",
    "category": "electronics",
    "price_cents": 2999,
    "initial_stock": 50
  }'

# Save the product ID from response, then create an order
curl -X POST http://localhost:8080/api/v1/orders \
  -H "Content-Type: application/json" \
  -d '{
    "customer_id": "billy",
    "items": [
      {
        "product_id": "<PRODUCT_ID_HERE>",
        "quantity": 2
      }
    ]
  }'
```

### 4. Access Observability Tools

| Tool | URL | Credentials |
|------|-----|-------------|
| Grafana | http://localhost:3000 | admin/admin |
| Jaeger | http://localhost:16686 | - |
| Prometheus | http://localhost:9090 | - |

## Code Organization

This project uses **Go workspaces** (`go.work`) for monorepo management. Each service and shared package is a separate Go module:

```
micromart/
├── services/
│   ├── gateway/          → REST API gateway module
│   ├── order/            → Order service module (saga orchestrator)
│   └── inventory/        → Inventory & catalog service module
├── pkg/
│   ├── config/           → Shared config utilities
│   ├── logger/           → Structured logging (slog)
│   ├── grpcutil/         → gRPC server/client, interceptors, circuit breaker
│   ├── metrics/          → Prometheus metrics definitions
│   ├── otel/             → OpenTelemetry initialization
│   └── redis/            → Redis client (idempotency, distributed locks)
├── gen/                  → Generated protobuf code (shared via go.work)
├── proto/                → Protocol Buffer definitions
├── migrations/           → Database migration SQL files
│   ├── order/
│   └── inventory/
├── deploy/               → Deployment configurations
│   ├── k8s/              → Kubernetes manifests
│   ├── prometheus.yaml
│   └── otelcol.yaml
├── tests/load/           → k6 load test scripts
├── docs/adr/             → Architecture Decision Records
└── scripts/              → Helper scripts
```

### Module Structure

Each service follows a similar structure:

```
services/order/
├── main.go              # Entry point, wiring
├── server.go            # gRPC server implementation
├── repository.go        # Database queries
├── model.go             # Database models
├── saga.go              # Saga orchestrator logic
├── clickhouse.go        # Analytics event consumer
├── server_test.go       # Integration tests
├── Dockerfile           # Container image
├── .env                 # Environment variables
├── go.mod               # Module dependencies
└── go.sum
```

## Working with Proto Files

### Directory Structure

```
proto/
├── order/v1/
│   └── order.proto       # Order service API
└── inventory/v1/
    └── inventory.proto   # Inventory service API
```

### Generating Code

```bash
# Lint proto files
make proto-lint

# Generate Go code from .proto files
make proto

# Generated code is written to:
# gen/order/v1/          → order.pb.go, order_grpc.pb.go
# gen/inventory/v1/      → inventory.pb.go, inventory_grpc.pb.go
```

### Adding New Proto Files

1. Create `proto/newservice/v1/newservice.proto`
2. Update `buf.gen.yaml` if needed (usually automatic)
3. Run `make proto` to generate code
4. Import generated code: `import orderv1 "github.com/ahargunyllib/micromart/gen/order/v1"`

### Proto Best Practices

- Use semantic versioning in package paths (`v1`, `v2`)
- Add comments for all messages and fields
- Use `google.protobuf.Timestamp` for timestamps
- Mark required fields clearly
- Run `make proto-lint` to catch issues

## Code Quality

### Linting

```bash
# Run linter on all modules
make lint

# Auto-format code
make fmt
```

**Enabled linters** (see `.golangci.yaml`):
- `errcheck` - Check for unchecked errors
- `govet` - Vet examines Go source code
- `staticcheck` - Static analysis
- `unused` - Check for unused code
- `ineffassign` - Detect ineffectual assignments
- `gocritic` - Comprehensive Go linter
- `misspell` - Spell checker

Generated code in `gen/` is automatically excluded.

### Testing

```bash
# Run all tests with race detection
make test

# Run tests for a specific service
go test -v -race ./services/order/...

# Generate coverage report
make test-cover
# Opens coverage.html in browser
```

### Code Coverage

We aim for:
- **>80% coverage** on business logic (saga, repositories)
- **>60% coverage** on API handlers
- **100% coverage** on critical paths (payment flow, inventory reservation)

Check coverage:
```bash
make test-cover
# Review coverage.html to find uncovered lines
```

## Adding New Services

### Step-by-Step Guide

1. **Create service directory**
   ```bash
   mkdir -p services/newservice
   cd services/newservice
   ```

2. **Initialize Go module**
   ```bash
   go mod init github.com/ahargunyllib/micromart/services/newservice
   ```

3. **Add to workspace**
   ```bash
   # In project root
   go work use ./services/newservice
   ```

4. **Define proto interface**
   ```bash
   mkdir -p proto/newservice/v1
   # Create proto/newservice/v1/newservice.proto
   make proto
   ```

5. **Implement the server**
   Create `services/newservice/server.go`:
   ```go
   package main

   import (
       "context"
       newservicev1 "github.com/ahargunyllib/micromart/gen/newservice/v1"
   )

   type Server struct {
       newservicev1.UnimplementedNewServiceServer
   }

   func (s *Server) SomeMethod(ctx context.Context, req *newservicev1.Request) (*newservicev1.Response, error) {
       // Implementation
       return &newservicev1.Response{}, nil
   }
   ```

6. **Create Dockerfile**
   ```dockerfile
   FROM golang:1.25-alpine AS builder
   WORKDIR /app
   COPY . .
   RUN go build -o newservice ./services/newservice

   FROM alpine:latest
   RUN apk --no-cache add ca-certificates
   COPY --from=builder /app/newservice /newservice
   ENTRYPOINT ["/newservice"]
   ```

7. **Update Makefile**
   Add build and run targets:
   ```makefile
   run-newservice:
       set -a && . services/newservice/.env && set +a && go run ./services/newservice

   build-newservice:
       docker build -f services/newservice/Dockerfile -t micromart/newservice:latest .
   ```

8. **Create K8s manifests**
   ```bash
   # Create deploy/k8s/newservice.yaml
   # Include Deployment, Service, and optionally HPA
   ```

9. **Update CI/CD**
   Add to `.github/workflows/ci.yml`:
   ```yaml
   strategy:
     matrix:
       service: [gateway, order, inventory, newservice]  # Add here
   ```

## Database Migrations

### Creating Migrations

```bash
# Create migration for order service
./scripts/migrate.sh order create add_payment_table

# This creates two files in migrations/order/:
# - 000002_add_payment_table.up.sql    # Forward migration
# - 000002_add_payment_table.down.sql  # Rollback migration
```

Edit the SQL files:

**000002_add_payment_table.up.sql:**
```sql
CREATE TABLE payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id),
    amount_cents BIGINT NOT NULL,
    status VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_payments_order_id ON payments(order_id);
```

**000002_add_payment_table.down.sql:**
```sql
DROP TABLE IF EXISTS payments;
```

### Running Migrations

**Docker Compose (local development):**
```bash
# Apply all pending migrations
make migrate-up-order
make migrate-up-inventory

# Or both at once
make migrate-up

# Rollback last migration
./scripts/migrate.sh order down
./scripts/migrate.sh inventory down
```

**Kubernetes (production):**
```bash
# Rebuild migration image
make build-migrations-order

# Deploy and run
make k8s-migrate-clean    # Delete old job
make k8s-migrate-up       # Run migrations

# Check status
make k8s-migrate-status
make k8s-migrate-logs-order
```

### Migration Best Practices

- **Always test rollback**: Ensure `.down.sql` works
- **Make migrations atomic**: One logical change per migration
- **Be careful with data migrations**: Test with production-like data volume
- **Don't edit existing migrations**: Create new ones to fix issues
- **Use transactions**: Wrap multiple statements in `BEGIN`/`COMMIT`

## Debugging

### Local Development with Delve

```bash
# Start infrastructure
make up

# Run service with debugger
dlv debug ./services/order

# Set breakpoints
(dlv) break services/order/saga.go:45
(dlv) continue

# Or use remote debugging
dlv debug ./services/order --headless --listen=:2345 --api-version=2
```

### VS Code Launch Configuration

Create `.vscode/launch.json`:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Debug Gateway",
      "type": "go",
      "request": "launch",
      "mode": "debug",
      "program": "${workspaceFolder}/services/gateway",
      "envFile": "${workspaceFolder}/services/gateway/.env"
    },
    {
      "name": "Debug Order Service",
      "type": "go",
      "request": "launch",
      "mode": "debug",
      "program": "${workspaceFolder}/services/order",
      "envFile": "${workspaceFolder}/services/order/.env"
    }
  ]
}
```

### Kubernetes Debugging

```bash
# Get logs
make k8s-logs-order
make k8s-logs-gateway

# Follow logs in real-time
kubectl logs -n micromart -l app=order-service -f --tail=100

# Shell into a pod
kubectl exec -it <pod-name> -n micromart -- /bin/sh

# Check events for issues
kubectl get events -n micromart --sort-by='.lastTimestamp'

# Describe pod for detailed status
kubectl describe pod <pod-name> -n micromart
```

### Common Issues

**Issue: "connection refused" errors**
```bash
# Check if services are running
docker compose ps

# Check service logs
make logs

# Verify database is accessible
docker exec -it postgres-order psql -U micromart -d order_db
```

**Issue: Migrations failing**
```bash
# Check migration logs
./scripts/migrate.sh order up

# Manually check migration state
docker exec -it postgres-order psql -U micromart -d order_db -c "SELECT * FROM schema_migrations"
```

## Environment Variables

Each service has its own `.env` file:

### services/gateway/.env
```bash
PORT=8080
ORDER_SERVICE_ADDR=localhost:50051
INVENTORY_SERVICE_ADDR=localhost:50052
OTLP_ENDPOINT=localhost:4317
```

### services/order/.env
```bash
DATABASE_URL=postgres://micromart:micromart@localhost:5432/order_db?sslmode=disable
INVENTORY_SERVICE_ADDR=localhost:50052
REDIS_ADDR=localhost:6379
CLICKHOUSE_ADDR=localhost:9000
CLICKHOUSE_DATABASE=default
CLICKHOUSE_USER=micromart
CLICKHOUSE_PASSWORD=micromart
GRPC_PORT=50051
METRICS_PORT=9091
OTLP_ENDPOINT=localhost:4317
```

### services/inventory/.env
```bash
DATABASE_URL=postgres://micromart:micromart@localhost:5433/inventory_db?sslmode=disable
REDIS_ADDR=localhost:6379
GRPC_PORT=50052
METRICS_PORT=9092
OTLP_ENDPOINT=localhost:4317
```

### Creating .env Files

If `.env` files don't exist, create them based on the examples above. The `make run-*` targets automatically load these files.

## Performance Profiling

### CPU Profiling

```bash
# Profile during test
go test -cpuprofile=cpu.prof -bench=. ./services/order/...

# Analyze profile
go tool pprof cpu.prof

# Common pprof commands:
# (pprof) top        # Show top functions by CPU
# (pprof) list saga  # Show line-by-line profile for saga functions
# (pprof) web        # Open browser visualization (requires graphviz)
```

### Memory Profiling

```bash
# Profile memory allocations
go test -memprofile=mem.prof -bench=. ./services/order/...

# Analyze
go tool pprof mem.prof

# (pprof) top        # Show top allocations
# (pprof) list       # Line-by-line allocation
```

### Production Profiling

If pprof is enabled in production (usually on a separate port):

```bash
# Heap profile
go tool pprof http://localhost:6060/debug/pprof/heap

# Goroutine profile
go tool pprof http://localhost:6060/debug/pprof/goroutine

# CPU profile (30 seconds)
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30
```

### Benchmarking

Write benchmarks in `*_test.go`:

```go
func BenchmarkCalculateTotal(b *testing.B) {
    items := []OrderItem{
        {ProductID: "123", Quantity: 5, UnitPriceCents: 1000},
        {ProductID: "456", Quantity: 2, UnitPriceCents: 2500},
    }

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = CalculateOrderTotal(items)
    }
}
```

Run benchmarks:
```bash
go test -bench=. -benchmem ./services/order/...
```

## Load Testing

### Run k6 Tests

```bash
# Full load test (5 minutes, ramping to 20 VUs)
make load-test

# Quick smoke test (30 seconds, 5 VUs)
make load-test-quick

# Custom test
k6 run --vus=10 --duration=1m tests/load/k6.js
```

### Analyzing Results

k6 provides metrics like:
- `http_req_duration` - Request latency (p95, p99)
- `http_req_failed` - Error rate
- `http_reqs` - Throughput (req/s)

Monitor these while load testing:
- Grafana: http://localhost:3000 (metrics)
- Jaeger: http://localhost:16686 (traces)
- Prometheus: http://localhost:9090 (raw metrics)

## Useful Commands

### Docker Compose

```bash
make up        # Start infrastructure
make down      # Stop and remove containers
make logs      # Tail all logs
```

### Building

```bash
make build                    # Build all Docker images
make build-gateway            # Build gateway only
make build-migrations         # Build migration images
```

### Testing

```bash
make test                     # Run all tests
make test-cover               # Generate coverage report
make lint                     # Run linter
make fmt                      # Format code
```

### Proto

```bash
make proto                    # Generate code
make proto-lint               # Lint proto files
```

## Next Steps

- Read [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines
- Check [Architecture Decision Records](docs/adr/) for design rationale
- Review [deploy/k8s/README.md](deploy/k8s/README.md) for Kubernetes deployment

## Getting Help

- **Questions**: Open a [GitHub Discussion](https://github.com/ahargunyllib/micromart/discussions)
- **Bugs**: File an [Issue](https://github.com/ahargunyllib/micromart/issues)
- **Design decisions**: Check [ADRs](docs/adr/)
