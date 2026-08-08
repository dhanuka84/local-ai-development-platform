# ADR-0001: Go for the MCP and data plane

- Status: Accepted
- Date: 2026-08-08

## Decision

Implement the MCP gateway, workflow adapters, index worker, and administration CLI in Go. Use the official Tier-1 Go MCP SDK. Permit Python sidecars for evaluation/ML experiments and consider Rust only for a measured performance or safety requirement.

## Rationale

Go produces small deployable services, has strong concurrency and operational tooling, supports PostgreSQL and Milvus directly, and is easier for a typical platform team to maintain than a mixed-language core. Python would shorten ML experimentation but adds more runtime/dependency variability to the production boundary. Rust provides excellent control and safety but increases delivery and onboarding cost without a demonstrated hot path.

## Consequences

- Go 1.25.8 is required by the selected Milvus client.
- Binaries and containers are easy to deploy on ARM64 and AMD64.
- ML-specific evaluation may live outside the core service and communicate through versioned contracts.
