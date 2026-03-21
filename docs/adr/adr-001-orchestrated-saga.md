# ADR-001: Orchestrated Saga Pattern for Order Processing

## Status
Accepted

## Context
In a microservices architecture, each service owns its own database. A single business operation like "create an order" spans multiple services (Order, Inventory, Payment). We need a way to maintain data consistency across these services without distributed transactions (2PC), which are impractical in a microservices context due to tight coupling, reduced availability, and poor performance.

## Decision
We use the **orchestrated saga pattern** with the Order Service as the central coordinator.

The saga follows this sequence:
1. Reserve Inventory (Inventory Service)
2. Process Payment (Payment Service, currently stubbed)
3. Confirm Order (decrement inventory)

Each step has a compensating action that runs on failure:
- Payment fails -> Release reserved inventory
- Confirm fails -> Release reserved inventory

Saga state is persisted in PostgreSQL (`saga_state` table) for crash recovery.

## Alternatives Considered

**Choreography-based saga:** Each service emits events and other services react. Rejected because with only 3 services, the added complexity of an event bus (Kafka/NATS) and the difficulty of tracing event chains is not justified. Choreography becomes valuable at 10+ services where a central orchestrator would become a bottleneck.

**Two-phase commit (2PC):** Distributed transaction across all services. Rejected because it requires all participants to be available simultaneously, creating tight coupling and a single point of failure at the transaction coordinator.

**Best-effort with manual reconciliation:** Process steps independently and reconcile failures asynchronously. Rejected because it provides weaker consistency guarantees and requires operational tooling for manual intervention.

## Consequences

**Positive:**
- Clear, linear flow that is easy to reason about, test, and debug
- Saga state is visible in a single table for monitoring
- Compensating actions are co-located with forward actions
- Crash recovery is straightforward (read saga_state, resume or compensate)

**Negative:**
- Order Service becomes a coordination bottleneck under extreme load
- Adding new saga steps requires modifying the orchestrator
- Synchronous execution adds latency (each step is sequential)
