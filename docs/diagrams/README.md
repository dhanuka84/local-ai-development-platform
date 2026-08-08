# Hybrid AI Platform Diagrams

This directory contains the editable Mermaid sources and rendered images for the local and enterprise deployment profiles.

## Deliverables

| View | Mermaid source | PNG | SVG |
|---|---|---|---|
| Local GBX100 architecture | `hybrid-ai-local-architecture.mmd` | `hybrid-ai-local-architecture.png` | `hybrid-ai-local-architecture.svg` |
| Enterprise distributed architecture | `hybrid-ai-enterprise-architecture.mmd` | `hybrid-ai-enterprise-architecture.png` | `hybrid-ai-enterprise-architecture.svg` |
| Local-to-enterprise evolution | `hybrid-ai-local-to-enterprise-evolution.mmd` | `hybrid-ai-local-to-enterprise-evolution.png` | `hybrid-ai-local-to-enterprise-evolution.svg` |

The SVG files are recommended for documentation and presentations because they remain readable at any zoom level. The PNG files are high-resolution raster exports rendered at approximately 11K–13K pixels wide.

## Architecture conventions

- PostgreSQL is the authoritative runtime system of record for workflow, graph relationships, provenance, audit, and indexing state.
- Git repository nodes and typed, evidence-backed relationships are authoritative in PostgreSQL and semantically projected into Milvus.
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
  -s 2
```

For a diagram whose natural Mermaid layout is narrower, increase `-s` to `4` to produce a comparable 12K-class export.

If Chrome is installed elsewhere, update `executablePath` in `puppeteer-config.json` before rendering.
