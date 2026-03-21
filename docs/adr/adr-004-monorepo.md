# ADR-004: Monorepo with Go Workspaces

## Status
Accepted

## Context
The project contains 3 services and shared packages. We need a repository structure that allows code sharing, independent builds, and manageable CI/CD.

## Decision
We use a **monorepo** with Go workspaces (`go.work`). Each service and shared package is its own Go module. The workspace coordinates local module resolution.

```
micromart/
├── go.work
├── gen/              # Generated proto code
├── pkg/              # Shared packages (config, logger, grpcutil, metrics, otel, redis)
├── services/
│   ├── gateway/
│   ├── order/
│   └── inventory/
├── proto/
├── migrations/
└── deploy/
```

## Alternatives Considered

**Single go.mod monolith:** One module for everything. Simpler, but every service compiles all dependencies even if unused. Cannot independently version or deploy services.

**Multi-repo (one repo per service):** Maximum independence, but proto sharing becomes painful (need a shared proto repo or artifact registry). Dependency updates require coordinated PRs across repos. Overkill for a solo project.

**Single go.mod with build tags:** Use build tags to separate service code. Hacky and error-prone. Not idiomatic Go.

## Consequences

**Positive:**
- Shared packages are imported directly without publishing
- Single PR can update proto + all services atomically
- CI/CD runs against the entire codebase, catching cross-service breaks
- Easy to refactor shared code

**Negative:**
- Docker build context includes the entire repo (each Dockerfile needs access to go.work and pkg/)
- CI runs all tests even for changes to a single service (can be optimized with path filters later)
- go.work is committed, which is unusual (typically .gitignored when used for local overrides)
