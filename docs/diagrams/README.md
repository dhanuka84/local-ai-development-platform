# Project diagrams

Updated September 12, 2026. These are repository-native Mermaid diagrams with
SVG and PNG exports. Use the [developer guide](../developer-guide.md) for the
full explanation and the [documentation index](../README.md) for current status.

## Views and downloadable exports

| View | Mermaid source | SVG | PNG |
|---|---|---|---|
| Implemented local runtime | [Source](hybrid-ai-local-architecture.mmd) | [SVG](hybrid-ai-local-architecture.svg) | [PNG](hybrid-ai-local-architecture.png) |
| Governed task lifecycle and validated reuse | [Source](hybrid-ai-review-learning-loop.mmd) | [SVG](hybrid-ai-review-learning-loop.svg) | [PNG](hybrid-ai-review-learning-loop.png) |
| KB entries versus governed definitions | [Source](hybrid-ai-review-learning-explainer.mmd) | [SVG](hybrid-ai-review-learning-explainer.svg) | [PNG](hybrid-ai-review-learning-explainer.png) |
| OpenClaw integration and authority | [Source](openclaw-agentic-automation-workflow.mmd) | [SVG](openclaw-agentic-automation-workflow.svg) | [PNG](openclaw-agentic-automation-workflow.png) |
| Enterprise reference target | [Source](hybrid-ai-enterprise-architecture.mmd) | [SVG](hybrid-ai-enterprise-architecture.svg) | [PNG](hybrid-ai-enterprise-architecture.png) |
| Local functionality to deployment acceptance | [Source](hybrid-ai-local-to-enterprise-evolution.mmd) | [SVG](hybrid-ai-local-to-enterprise-evolution.svg) | [PNG](hybrid-ai-local-to-enterprise-evolution.png) |

Use SVG for zooming, presentations and print. Markdown pages embed PNG for
broad renderer compatibility. The publication explainer now has editable
Mermaid source; its previous AI-generated raster is superseded. The
[original generation prompt](hybrid-ai-review-learning-explainer.prompt.md)
remains a historical record and does not describe the current export.

## How to read them

Purple identifies clients or immutable evidence, blue identifies application
services and data, green identifies local execution or successful completion,
amber identifies validation/authority conditions, and peach identifies pending
KB entries. Dashed gray boxes identify optional, conditional or proposed
components; the text states which. The enterprise diagram is a reference target,
not a claim of a deployed cluster.

Arrows show calls, data movement or lifecycle progression as labeled. A dotted
connection to a note supplies context. The local diagram shows logical
components: the application service and analyzer run inside the gateway; AGE
runs inside PostgreSQL. They are not all separately deployed services.

## Architecture rules represented

- PostgreSQL is authoritative for runtime records, versions, decisions, graphs,
  traces and outbox intent. AGE and Milvus are rebuildable projections.
- A search result is an ID/score candidate until current PostgreSQL hydration
  checks approval, source, version, project and projection eligibility.
- Git holds source, policies, contracts and docs. Runtime KB content is in
  PostgreSQL; there is no automatic Git-wiki publication.
- Exact model output, disclosed-context manifests and validation receipts go
  into the SHA-256 artifact store. Raw model review is evidence, not approval.
- Generated KB entries require the user's explicit exact-version decision.
  Validated domain/capability/metric definitions may use standing task authority
  under the existing operator roles and recorded validation.
- Eligible validated reuse completes a task while its new KB entry stays
  pending. New-knowledge publication requires indexing and exact Milvus read-back.
- Maintenance remains local with no cloud fallback. A conditional development
  review is read-only; automated disclosure packaging remains planned.
- Local functional acceptance is separate from live rollout, representative
  adoption cohorts and target identity/storage/HA/recovery acceptance.

## Render and verify

Use Node/npm and local Chrome. The pinned renderer is
`@mermaid-js/mermaid-cli@11.16.0`; the browser path is set in
[puppeteer-config.json](puppeteer-config.json).

```bash
make diagrams
make docs-check
```

The renderer processes every `.mmd` file, writes SVG and PNG on white
backgrounds, and updates [manifest.json](manifest.json). PNG uses a 2,400-pixel
viewport and scale 2; actual dimensions depend on diagram layout and are saved
in the manifest. No image-generation model is used.

The manifest binds each source, SVG, PNG, renderer script and browser config
by SHA-256. `docs-check` verifies those hashes, SVG/PNG structure and dimensions,
local Markdown links/anchors, and reachability of every Markdown document from
the repository README. CI runs this check without installing a browser.

Render one diagram during editing:

```bash
make diagram-local-architecture
make diagram-enterprise-architecture
make diagram-local-to-enterprise
make diagram-review-loop
make diagram-agentic-workflow
make diagram-knowledge-publication
```

A single target updates only its diagram's manifest entry. After changing the
shared renderer or browser configuration, regenerate all diagrams. Review
the rendered images for label wrapping, clipping and misleading relationships;
hash checks cannot judge architectural accuracy.

The [renderer](../../scripts/render_diagrams.py) and
[documentation checker](../../scripts/docs_check.py) contain the executable
build/verification rules. External website availability and production behavior
are outside this documentation check.
