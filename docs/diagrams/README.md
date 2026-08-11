# Hybrid AI Platform Diagrams

This directory contains the editable Mermaid sources and rendered images for
the local, review-learning, and enterprise deployment profiles.

## Deliverables

| View | Mermaid source | PNG | SVG |
|---|---|---|---|
| Quick review-learning explainer | [generation prompt](hybrid-ai-review-learning-explainer.prompt.md) | `hybrid-ai-review-learning-explainer.png` | Not applicable |
| Local GBX100 architecture | `hybrid-ai-local-architecture.mmd` | `hybrid-ai-local-architecture.png` | `hybrid-ai-local-architecture.svg` |
| Remote review and local learning loop | `hybrid-ai-review-learning-loop.mmd` | `hybrid-ai-review-learning-loop.png` | `hybrid-ai-review-learning-loop.svg` |
| Enterprise distributed architecture | `hybrid-ai-enterprise-architecture.mmd` | `hybrid-ai-enterprise-architecture.png` | `hybrid-ai-enterprise-architecture.svg` |
| Local-to-enterprise evolution | `hybrid-ai-local-to-enterprise-evolution.mmd` | `hybrid-ai-local-to-enterprise-evolution.png` | `hybrid-ai-local-to-enterprise-evolution.svg` |
| OpenClaw agentic automation and Cerbos governance | [Mermaid](openclaw-agentic-automation-workflow.mmd) | [PNG](openclaw-agentic-automation-workflow.png) | [SVG](openclaw-agentic-automation-workflow.svg) |

SVG is the main format for documentation and presentations because it remains
sharp at any zoom level. PNG is an optional compatibility export for tools,
chat systems, and fixed-format documents that cannot display SVG. Mermaid PNGs
are rendered at high resolution. The editable `.mmd` file remains the source
for each Mermaid diagram.

## Flow numbering and colors

The arrows use color and short step numbers to make each path easier to follow.

- Runtime architecture: blue `1` request, purple `2` orchestration, orange `3`
  local execution, magenta `3C` optional cloud review, amber `4` evidence,
  emerald `5` approval and commit, green `6` indexing, and cyan `7` retrieval
  and reuse.
- Review-learning loop: green `1` local work, magenta `2` remote review,
  indigo `3` validation and learning, and cyan `4` local reuse.
- Local-to-enterprise evolution: green `1` local profile, purple `2` stable
  contracts, blue `3` enterprise scale-out, and dashed red `3M` Milvus
  migration.
- OpenClaw automation: blue `1` request, purple `2` orchestration, orange `3`
  execution, red `4` authorization, emerald persistence, and green `5`
  approved learning.
- Gray arrows are supporting operational paths. Dashed red arrows are hard
  controls, migration warnings, or disaster-recovery paths.

The enterprise and local overviews use curved group-to-group arrows for
cross-plane flows. Components remain visible inside each parallel plane, while
detailed endpoint behavior is defined in the implementation documents and
contracts. The review-learning loop uses the same pattern for its numbered
lifecycle stages. The evolution diagram keeps its detailed one-to-one contract
mappings because those relationships are the purpose of that view.

## Architecture conventions

- PostgreSQL is the authoritative runtime system of record for workflow, graph relationships, provenance, audit, and indexing state.
- Git repository nodes and typed, evidence-backed relationships are authoritative in PostgreSQL and semantically projected into Milvus.
- Revisioned code entities and every exact code edge are authoritative in PostgreSQL. Only selected first-party entity summaries are projected into Milvus, using the same stable PostgreSQL UUID.
- Apache AGE contains only rebuildable active repository/code/approved-knowledge topology; every result is re-hydrated from PostgreSQL.
- OpenClaw orchestrates analysis; deterministic compiler-aware analyzers produce graph evidence. LLM interpretations follow the candidate review workflow.
- Git is the human-reviewable source for approved patterns, ADRs, policies, and runbooks.
- Object or content-addressed storage holds large immutable artifacts and evidence.
- Milvus is a derived vector and hybrid-search index that can be rebuilt from authoritative sources.
- Cloud review is policy-gated. Maintenance is local-only and fails closed without local inference.
- The PostgreSQL outbox and idempotent workers prevent unsafe dual writes between PostgreSQL, Git, object storage, and Milvus.

## Rendering

The diagrams were validated with Mermaid CLI 11.16.0. A local Chrome executable is configured in `puppeteer-config.json`.

Render a diagram as SVG:

```bash
npx -y @mermaid-js/mermaid-cli \
  -p docs/diagrams/puppeteer-config.json \
  -i docs/diagrams/hybrid-ai-local-architecture.mmd \
  -o docs/diagrams/hybrid-ai-local-architecture.svg \
  -b '#ffffff'
```

Render a high-resolution PNG:

```bash
npx -y @mermaid-js/mermaid-cli \
  -p docs/diagrams/puppeteer-config.json \
  -i docs/diagrams/hybrid-ai-local-architecture.mmd \
  -o docs/diagrams/hybrid-ai-local-architecture.png \
  -b '#ffffff' \
  -w 6400 \
  -s 3
```

For a diagram whose natural Mermaid layout is narrower, increase `-s` to `4` to produce a comparable 12K-class export.

The review-learning diagram has a dedicated reproducible target:

```bash
make diagram-review-loop
```

The OpenClaw/Cerbos automation diagram has a dedicated target:

```bash
make diagram-agentic-workflow
```

This diagram uses aligned rows, compact spacing, and thicker colored arrows so
each flow is easy to follow without large empty areas. The SVG stays sharp at
any zoom level. The optional PNG is 7,953 × 2,916 pixels; use the SVG when
presenting on a large screen or printing.

If Chrome is installed elsewhere, update `executablePath` in `puppeteer-config.json` before rendering.
