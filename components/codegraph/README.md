# Code graph component

This component converts a checked-out source repository into a deterministic,
revision-scoped graph. It is intentionally limited to extraction; PostgreSQL
ownership, Milvus projection, MCP policy, review workflows, and model routing
remain in the MIT-licensed platform.

The graph-domain concepts and incremental analyzer contract adapt the useful
portion of Bevel Software's `code-to-knowledge-graph` project. Files in this
directory are licensed under MPL-2.0. See `NOTICE` and `LICENSE`.

The first provider is headless Go analysis. It uses Go package, syntax, and
type information directly and does not require VS Code, Neo4j, or a network
listener.

## Deliberately excluded

- VS Code extension and HTTP bridge
- Neo4j export and visualization
- ANTLR query language and language-specific grammars
- UI and prompt-generation features
- Description merge/conflict behavior
- Bevel's content-hash-based node identity

The public `Analyzer` interface allows later LSP, SCIP, and tree-sitter
providers to emit the same canonical snapshot.
