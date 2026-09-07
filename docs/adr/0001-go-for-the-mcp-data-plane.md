# ADR-0001: Go for the MCP and data plane

- Status: Accepted
- Date: 2026-08-08

## Decision

Implement the MCP gateway, workflow adapters, index worker, and administration CLI in Go. Use the official Tier-1 Go MCP SDK. Permit Python sidecars for evaluation/ML experiments and consider Rust only for a measured performance or safety requirement.

## Rationale

Go produces small deployable services, has strong concurrency and operational tooling, supports PostgreSQL and Milvus directly, and is easier for a typical platform team to maintain than a mixed-language core. Python would shorten ML experimentation but adds more runtime/dependency variability to the production boundary. Rust provides excellent control and safety but increases delivery and onboarding cost without a demonstrated hot path.

## Consequences

- Go 1.25.8 is the module's minimum requirement, originally selected for the
  Milvus client. As of 2026-09-06, development, CI and Docker use Go 1.26.8;
  the `toolchain` directive selects it without raising the module's minimum.
- Binaries and containers are easy to deploy on ARM64 and AMD64.
- ML-specific evaluation may live outside the core service and communicate through versioned contracts.

## Toolchain verification — 2026-09-06

Ordered procedure: inspect project knowledge and repository relationships;
preserve the module minimum and select the newer development toolchain; align
CI, Docker and formatting; exclude private runtime files from Docker context;
run native/container checks, contracts, disposable PostgreSQL integration and
cross-platform builds; remove temporary test services without a live rollout.

Go 1.26.8 passed `make check` natively on Linux ARM64 and through
`make check-container`, `make contracts-check`, `make integration-test-fresh`,
Compose configuration checks, native and Linux AMD64 cross-builds, and
`git diff --check`. The Docker build target produced gateway, worker and admin
binaries whose embedded versions were verified as Go 1.26.8. Its image contains
neither `.local` runtime state nor `.env`. The synthetic integration database
and containers were removed; retained pilot state and live services were not
changed by this upgrade. Remote CI was updated, not executed in this session.

Evidence revision: `e44cbb4b0eeec517d819892fb85e9fdad0ca5e45` plus the existing
uncommitted pilot work and this toolchain update. Optional `generation_capture`
may record the procedure and checks above as pending operational knowledge.
Validation provider: local Go/Docker tooling; generation/review model: none
invoked. This verification is not an approval of reusable knowledge.
