# ADR-002: gRPC for Inter-Service Communication

## Status
Accepted

## Context
Services need to communicate synchronously for operations like product lookup and stock reservation. We need a protocol that provides type safety, performance, and good tooling for Go.

## Decision
We use **gRPC with Protocol Buffers (proto3)** for all inter-service communication. The API Gateway translates external REST requests to internal gRPC calls.

## Alternatives Considered

**REST/JSON internally:** Familiar and simple, but lacks strong typing. Schema changes are easy to miss across services, leading to runtime errors. JSON serialization is slower than protobuf binary encoding.

**GraphQL:** Good for flexible client queries, but adds complexity for service-to-service calls where the schema is fixed. Better suited for client-facing APIs than internal communication.

**Message queue (async):** Appropriate for fire-and-forget operations, but the saga pattern requires synchronous request-response to coordinate steps and handle failures inline.

## Consequences

**Positive:**
- Strongly typed contracts via .proto files catch breaking changes at compile time
- Binary serialization is significantly faster and smaller than JSON
- Built-in interceptor chains for cross-cutting concerns (auth, tracing, metrics, circuit breaking)
- Proto files serve as living API documentation
- Code generation eliminates hand-written client/server boilerplate

**Negative:**
- Not human-readable on the wire (need tools like grpcurl for debugging)
- Requires proto tooling (buf) in the build pipeline
- Browser clients cannot call gRPC directly (hence the REST gateway)
