<!-- Generated from .github/instructions/ddd-architecture.instructions.md — DO NOT EDIT DIRECTLY -->

# DDD Layered Architecture — Strict Enforcement

Our project follows Domain-Driven Design (DDD) principles with **strict enforcement** to maintain clean, understandable core logic. We prioritize **simplicity, pragmatism, and maintainability** while avoiding unnecessary complexity.

## Layer Dependencies (NEVER VIOLATE)

```
Interfaces → Application → Domain ← Infrastructure
```

## Layer Definitions & Responsibilities

- **`internal/domain`**: The heart of the application. Contains entities, value objects, domain services, and repository **interfaces**.
  - ✅ ALLOWED: Pure business logic, entities, value objects, domain services
  - ❌ FORBIDDEN: External dependencies, I/O operations, frameworks (except Go standard library)

- **`internal/application`**: Orchestrates domain objects to fulfill business use cases.
  - ✅ ALLOWED: Use case orchestration, domain service coordination
  - ❌ FORBIDDEN: Direct infrastructure implementations, business rule implementations

- **`internal/infrastructure`**: Concrete implementations of domain interfaces.
  - ✅ ALLOWED: External API calls, database operations, **parallel processing**, resource management
  - ❌ FORBIDDEN: Business logic, domain rules

- **`internal/interfaces`**: Adapters to the outside world (CLI, HTTP handlers).
  - ✅ ALLOWED: API boundary definitions, request/response handling
  - ❌ FORBIDDEN: Business logic, **parallel processing implementations**, direct infrastructure logic

## Responsibility Matrix

| Layer | Responsibilities | ❌ FORBIDDEN |
|-------|-----------------|-------------|
| **Domain** | Business logic, entities, value objects | External dependencies, I/O, frameworks |
| **Application** | Use cases, orchestration | Direct infrastructure calls, business rules |
| **Infrastructure** | Database, external APIs, **parallel processing** | Business logic, domain rules |
| **Interfaces** | API boundaries, CLI, HTTP handlers | Business logic, parallel processing implementation |

## Core Domain Concepts

- **Entities vs. Value Objects**:
  - **Entity**: Object with distinct identity, can be mutable, encapsulates business logic
  - **Value Object**: Immutable object defined by attributes, always validate on creation
- **Repositories**: Define interfaces in domain layer, implement in infrastructure layer
- **Pragmatic Interface Usage**: Only create interfaces for dependency inversion, testability, or genuine polymorphism

## Critical Violations to Avoid

```go
// ❌ WRONG: Interface layer doing parallel processing
func ProcessBatchPURLs() {
    maxWorkers := 10
    purlChan := make(chan string)
    // This belongs in Infrastructure layer!
}
```

```go
// ✅ Interface layer: Orchestration only
func ProcessBatchPURLs(ctx context.Context, cfg *config.Config, purls []string) {
    // Delegate to Infrastructure layer
    repositoryService := repository.NewRepositoryService(...)
    return repositoryService.FetchAnalysesBatch(ctx, purls)
}
```

---

## Search Before Implementing

**Before writing ANY new code, you MUST:**

1. **Search for existing implementations** using semantic search or grep
2. **Identify similar patterns** in the codebase
3. **Reuse existing code** instead of duplicating functionality
4. **Verify no existing solution exists** before creating new code

**Example workflow:**
```bash
grep -r "Batch\|batch" internal/
grep -r "parallel\|concurrent" internal/
```

❌ FORBIDDEN: Writing duplicate code without searching
✅ REQUIRED: Always search → analyze → reuse → then implement if needed

## Pre-Implementation Checklist

**Before writing ANY function, you MUST verify:**

- [ ] **Search completed**: No existing implementation found
- [ ] **Layer check**: Implementation belongs in correct DDD layer
- [ ] **Dependency check**: No layer violations introduced
- [ ] **Reuse analysis**: Existing utilities/patterns identified
- [ ] **Interface design**: Follows existing patterns in the layer

## Code Reuse Enforcement

**Required Search Patterns:**
- Batch processing: `grep -r "Batch\|batch" internal/`
- Parallel execution: `grep -r "goroutine\|channel\|parallel" internal/`
- Domain conversions: `grep -r "ToDomain\|FromDomain" internal/`
- Service patterns: `grep -r "Service\|Repository" internal/`

## Parallel Processing Placement

- ✅ **Infrastructure layer**: Implementation details
- ✅ **Application layer**: High-level coordination
- ❌ **Interface layer**: Never implement goroutines/channels
- ❌ **Domain layer**: Pure business logic only

## Implementation Validation

**Before submitting code:**

1. **Layer validation**: `go vet ./...` passes
2. **Dependency check**: No circular dependencies
3. **Reuse verification**: No duplicate implementations
4. **DDD compliance**: Each layer respects boundaries

## Documentation Requirements

**Every function MUST document:**
```go
// ProcessBatchPURLs processes multiple PURLs using Infrastructure layer services
//
// DDD Layer: Interface (delegates to Infrastructure)
// Dependencies: repository.RepositoryService (Infrastructure)
// Reuses: Existing batch processing implementation
//
// Args: ctx - context, cfg - configuration, purls - PURL list
// Returns: domain.Analysis map, error
func ProcessBatchPURLs(ctx context.Context, cfg *config.Config, purls []string) (map[string]*domain.Analysis, error)
```

## Project Summary

1. **SEARCH FIRST** — Always look for existing implementations
2. **RESPECT DDD LAYERS** — Place code in correct architectural layer
3. **REUSE CODE** — Never duplicate existing functionality
4. **FOLLOW CHECKLIST** — Validate before implementing
5. **CORRECT CONCURRENCY** — Parallel processing in Infrastructure layer only

**VIOLATION CONSEQUENCE**: Immediate refactoring required to comply with DDD principles and eliminate code duplication.

## Learned from Copilot Reviews

- **Inject Infrastructure Into Interfaces via Function Types**: The Interfaces layer (CLI) must never import Infrastructure packages directly. When the CLI needs to call Infrastructure logic (e.g., file format detection, workflow parsing), define a function type in the Interfaces layer and inject the concrete implementation from the composition root (`cmd/`). This preserves the `Interfaces → Application → Domain ← Infrastructure` dependency direction.
