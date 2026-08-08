# Repository agent guidance

- Run `knowledge_search` before substantial design or debugging work when the MCP server is available.
- Use `repository_graph_get` when a change may affect another repository in the same product.
- Treat Milvus results as candidates and hydrate authoritative content from PostgreSQL through MCP tools.
- Never approve knowledge merely because a model generated or reviewed it. Approval requires an accountable actor and relevant validation evidence.
- After a useful, validated outcome, offer to record it with `generation_capture`. Include ordered procedure, validation evidence, repository revision, provider, and model.
- Maintenance tasks must use local models only. Do not invoke cloud review or introduce a cloud fallback.
- Never send secrets, credentials, personal data, production dumps, or unrestricted repository content to Kimi, OpenAI, or another cloud provider.
- Run `make check` before handing off repository changes.
