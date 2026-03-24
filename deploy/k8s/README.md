# Kubernetes Deployment Guide

This directory contains Kubernetes manifests for deploying the micromart microservices platform.

## Architecture Overview

The deployment includes:

### Infrastructure Services
- **PostgreSQL** (2 instances): Separate databases for order and inventory services
- **Redis**: Caching and distributed locking
- **ClickHouse**: Analytics database
- **Prometheus**: Metrics collection
- **Grafana**: Metrics visualization
- **Jaeger**: Distributed tracing UI
- **OpenTelemetry Collector**: Trace aggregation and forwarding

### Application Services
- **Gateway**: REST API gateway (port 8080)
- **Order Service**: Order management and saga orchestration (gRPC port 50051)
- **Inventory Service**: Product catalog and stock management (gRPC port 50052)

### Database Migrations
- **Migration Jobs**: Kubernetes Jobs that apply database schema migrations

## Prerequisites

- Kubernetes cluster (1.28+)
  - Local: minikube, kind, Docker Desktop
  - Cloud: GKE, EKS, AKS
- kubectl configured to access your cluster
- Docker (for building images)
- Sufficient cluster resources:
  - CPU: ~4 cores
  - Memory: ~8GB
  - Storage: ~20GB

## Quick Start

### 1. Build Images

For local development (using local images):
```bash
# From project root
make build              # Build service images
make build-migrations   # Build migration images
```

For production (using GitHub Container Registry):
```bash
# Images are built automatically by CI/CD when you push to main
# They are available at ghcr.io/ahargunyllib/micromart/*
```

### 2. Deploy to Kubernetes

```bash
# Deploy all infrastructure and application services
make k8s-apply

# Wait for infrastructure to be ready (30-60 seconds)
kubectl get pods -n micromart --watch

# Run database migrations
make k8s-migrate-up

# Check migration status
make k8s-migrate-status
```

### 3. Access Services

Port-forward to access services from your local machine:

```bash
# API Gateway
make k8s-port-forward-gateway
# Now accessible at http://localhost:8080

# Grafana (admin/admin)
make k8s-port-forward-grafana
# http://localhost:3000

# Jaeger UI
make k8s-port-forward-jaeger
# http://localhost:16686

# Prometheus
make k8s-port-forward-prometheus
# http://localhost:9090
```

### 4. Test the Deployment

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

# Create an order
curl -X POST http://localhost:8080/api/v1/orders \
  -H "Content-Type: application/json" \
  -d '{
    "customer_id": "billy",
    "items": [{"product_id": "<PRODUCT_ID>", "quantity": 2}]
  }'
```

## Manifest Files

| File | Description |
|------|-------------|
| `namespace.yaml` | micromart namespace |
| `configmap.yaml` | Shared configuration (service addresses, endpoints) |
| `secrets.yaml` | Database credentials and sensitive config |
| `postgres.yaml` | PostgreSQL StatefulSets for order and inventory databases |
| `redis.yaml` | Redis Deployment with persistent storage |
| `clickhouse.yaml` | ClickHouse StatefulSet for analytics |
| `prometheus.yaml` | Prometheus Deployment with service discovery |
| `grafana.yaml` | Grafana Deployment with Prometheus datasource |
| `jaeger.yaml` | Jaeger all-in-one Deployment |
| `otel-collector.yaml` | OpenTelemetry Collector Deployment |
| `gateway.yaml` | API Gateway Deployment, Service, and Ingress |
| `order-service.yaml` | Order Service Deployment, Service, and HPA |
| `inventory-service.yaml` | Inventory Service Deployment and Service |
| `migrations-order.yaml` | Order database migration Job |
| `migrations-inventory.yaml` | Inventory database migration Job |

## Database Migrations

### How It Works

Database migrations use Kubernetes Jobs that:
1. Wait for PostgreSQL to be ready (init container)
2. Run `golang-migrate` to apply pending migrations
3. Complete successfully (idempotent - safe to re-run)

Migration SQL files are packaged into Docker images:
- `ghcr.io/ahargunyllib/micromart/migrations-order:latest`
- `ghcr.io/ahargunyllib/micromart/migrations-inventory:latest`

### Adding New Migrations

```bash
# Create new migration files locally
./scripts/migrate.sh order create add_payment_table

# Edit the generated SQL files in migrations/order/

# Rebuild the migration image
make build-migrations-order

# For local K8s, you're done - just re-run migrations
# For production, commit and push - CI/CD will build the image

# Clean up old migration Job
make k8s-migrate-clean

# Run migrations
make k8s-migrate-up

# Check status
make k8s-migrate-status
```

### CI/CD Integration

Migration images are automatically built by GitHub Actions when:
- Changes are detected in `migrations/order/**` or `migrations/inventory/**`
- Push is made to the `main` branch

Images are tagged with:
- `latest` - Always points to the most recent build
- `{git-sha}` - Specific commit for reproducibility

## Useful Commands

### Deployment Management

```bash
# Deploy everything
make k8s-apply

# Delete everything
make k8s-delete

# Check status of all resources
make k8s-status

# Describe a specific resource
kubectl describe deployment gateway -n micromart
kubectl describe pod <pod-name> -n micromart
```

### Logs

```bash
# Service logs
make k8s-logs-gateway
make k8s-logs-order
make k8s-logs-inventory

# Migration logs
make k8s-migrate-logs-order
make k8s-migrate-logs-inventory

# Infrastructure logs
kubectl logs -n micromart -l app=postgres-order -f
kubectl logs -n micromart -l app=redis -f
kubectl logs -n micromart -l app=prometheus -f
```

### Port Forwarding

```bash
# Application
make k8s-port-forward-gateway         # :8080

# Observability
make k8s-port-forward-grafana         # :3000
make k8s-port-forward-prometheus      # :9090
make k8s-port-forward-jaeger          # :16686

# Databases (for debugging)
make k8s-port-forward-postgres-order      # :5432
make k8s-port-forward-postgres-inventory  # :5433
make k8s-port-forward-redis              # :6379
make k8s-port-forward-clickhouse         # :8123 (HTTP), :9000 (native)
```

### Debugging

```bash
# Get pod shell
kubectl exec -it <pod-name> -n micromart -- /bin/sh

# Check pod events
kubectl get events -n micromart --sort-by='.lastTimestamp'

# Describe failing pod
kubectl describe pod <pod-name> -n micromart

# Check resource usage
kubectl top pods -n micromart
kubectl top nodes
```

## Persistent Storage

StatefulSets and some Deployments use PersistentVolumeClaims:

| Service | Storage | Size |
|---------|---------|------|
| postgres-order | 1Gi per replica | 1Gi |
| postgres-inventory | 1Gi per replica | 1Gi |
| redis | PVC | 1Gi |
| clickhouse | 5Gi per replica | 5Gi |
| prometheus | PVC | 5Gi |
| grafana | PVC | 1Gi |

**Note**: PVCs are NOT deleted when you run `make k8s-delete`. To fully clean up:
```bash
kubectl delete pvc --all -n micromart
```

## Scaling

### Horizontal Pod Autoscaling

Order service includes an HPA that scales based on CPU:
```yaml
minReplicas: 2
maxReplicas: 10
targetCPUUtilization: 70%
```

To manually scale:
```bash
kubectl scale deployment gateway --replicas=3 -n micromart
kubectl scale deployment inventory-service --replicas=3 -n micromart
```

### Vertical Scaling

Edit resource requests/limits in the manifest files:
```yaml
resources:
  requests:
    cpu: 200m
    memory: 128Mi
  limits:
    cpu: 1000m
    memory: 512Mi
```

## Production Considerations

### 1. Image Registry

Update image references in manifests to use your registry:
```yaml
image: ghcr.io/your-org/your-repo/gateway:v1.0.0
```

Or use local images for development:
```yaml
image: micromart/gateway:latest
imagePullPolicy: IfNotPresent
```

### 2. Secrets Management

Current setup uses plaintext secrets in `secrets.yaml`. For production:
- Use Kubernetes Secrets with encryption at rest
- Consider external secret managers (AWS Secrets Manager, HashiCorp Vault, etc.)
- Use Sealed Secrets or SOPS for GitOps workflows

### 3. Ingress

The gateway includes an Ingress resource configured for nginx-ingress-controller:
```yaml
host: api.micromart.local
ingressClassName: nginx
```

For production:
- Install an ingress controller (nginx, traefik, etc.)
- Configure TLS/SSL certificates
- Update the host to your actual domain
- Add annotations for rate limiting, CORS, etc.

### 4. Monitoring

- Prometheus scrapes metrics from all services
- Grafana includes a pre-configured Prometheus datasource
- Import the dashboard from `deploy/grafana-dashboard.json`
- Consider adding alerting rules and PagerDuty/Slack integration

### 5. High Availability

For production HA:
- Run multiple replicas of stateless services (gateway, services)
- Use StatefulSets with multiple replicas for databases
- Configure pod anti-affinity to spread pods across nodes
- Set pod disruption budgets

### 6. Resource Limits

Current manifests include conservative resource limits suitable for development. For production:
- Profile your services under load
- Adjust CPU/memory requests based on actual usage
- Set appropriate limits to prevent resource exhaustion
- Use VPA (Vertical Pod Autoscaler) for recommendations

## Troubleshooting

### Pods stuck in Pending

```bash
kubectl describe pod <pod-name> -n micromart
# Check Events section for reason (usually insufficient resources or PVC issues)
```

### Services can't reach databases

Check service DNS:
```bash
kubectl exec -it <pod-name> -n micromart -- nslookup postgres-order.micromart.svc.cluster.local
```

### Migrations failing

```bash
# Check migration logs
make k8s-migrate-logs-order

# Verify database is accessible
kubectl exec -it postgres-order-0 -n micromart -- psql -U micromart -d order_db -c "SELECT 1"

# Re-run migrations
make k8s-migrate-clean
make k8s-migrate-up
```

### Images not pulling

For local images (minikube, kind):
```bash
# Minikube: Use minikube's Docker daemon
eval $(minikube docker-env)
make build build-migrations

# Kind: Load images into cluster
kind load docker-image micromart/gateway:latest
kind load docker-image micromart/order:latest
kind load docker-image micromart/inventory:latest
kind load docker-image micromart/migrations-order:latest
kind load docker-image micromart/migrations-inventory:latest
```

For registry images:
```bash
# Check image pull policy and credentials
kubectl describe pod <pod-name> -n micromart
```

## Clean Up

```bash
# Delete all resources
make k8s-delete

# Delete persistent volumes (data will be lost!)
kubectl delete pvc --all -n micromart

# Delete the namespace
kubectl delete namespace micromart
```

## Additional Resources

- [Main README](../../README.md) - Project overview and local development
- [API Documentation](../../README.md#api-reference) - REST API endpoints
- [Architecture Decision Records](../../docs/adr/) - Design decisions
- [Makefile](../../Makefile) - All available commands
