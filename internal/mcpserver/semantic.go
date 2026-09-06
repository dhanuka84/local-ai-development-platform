package mcpserver

import (
	"context"
	"encoding/json"
	"github.com/dhanuka84/hybrid-ai-platform/internal/contextregistry"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type registryInput struct {
	ProjectID string `json:"project_id"`
}
type definitionSearchInput struct {
	ProjectID string `json:"project_id"`
	Query     string `json:"query"`
	Limit     int    `json:"limit"`
}
type definitionSearchOutput struct {
	Definitions []domain.GovernedDefinition `json:"definitions"`
}
type contextUseOutput struct {
	Recorded bool `json:"recorded"`
}

func (a *API) registerSemantic(server *mcp.Server) {
	addTool(a, server, readTool("knowledge_index_failures", "List failed indexes", "Operations-only failed knowledge intents with explicit recovery instructions."), func(ctx context.Context, _ *mcp.CallToolRequest, in registryInput) (*mcp.CallToolResult, struct {
		Failures []domain.FailedIndex `json:"failures"`
	}, error) {
		v, e := a.service.FailedKnowledgeIndexes(ctx, in.ProjectID)
		return nil, struct {
			Failures []domain.FailedIndex `json:"failures"`
		}{v}, e
	})
	addTool(a, server, writeTool("knowledge_index_retry", "Retry failed index", "Human operations decision creates one replacement intent while preserving exhausted attempt history."), func(ctx context.Context, _ *mcp.CallToolRequest, in domain.RetryIndexInput) (*mcp.CallToolResult, struct {
		EventID int64 `json:"event_id"`
	}, error) {
		v, e := a.service.RetryKnowledgeIndex(ctx, in)
		return nil, struct {
			EventID int64 `json:"event_id"`
		}{v}, e
	})
	addTool(a, server, readTool("platform_evidence_health", "Inspect evidence alerts", "Operations-only missing outcome, local export failure, and retention-review alerts. Never deletes evidence."), func(ctx context.Context, _ *mcp.CallToolRequest, in registryInput) (*mcp.CallToolResult, domain.EvidenceHealth, error) {
		v, e := a.service.EvidenceHealth(ctx, in.ProjectID)
		return nil, v, e
	})
	addTool(a, server, writeTool("context_registry_validate", "Validate context registry", "Run local contract fixtures and SQL preparation checks; stage definitions as pending, never approve them."), func(ctx context.Context, _ *mcp.CallToolRequest, in registryInput) (*mcp.CallToolResult, domain.RegistryValidation, error) {
		v, e := a.service.ValidateContextRegistry(ctx, in.ProjectID)
		return nil, v, e
	})
	addTool(a, server, writeTool("context_definition_decide", "Decide context definition", "Accountable human product owner approves or rejects one exact validated registry definition."), func(ctx context.Context, _ *mcp.CallToolRequest, in domain.DefinitionDecision) (*mcp.CallToolResult, domain.GovernedDefinition, error) {
		v, e := a.service.DecideContextDefinition(ctx, in)
		return nil, v, e
	})
	addTool(a, server, readTool("context_definition_search", "Search semantic definitions", "Use local Ollama and Milvus candidates; hydrate only approved current PostgreSQL definitions."), func(ctx context.Context, _ *mcp.CallToolRequest, in definitionSearchInput) (*mcp.CallToolResult, definitionSearchOutput, error) {
		v, e := a.service.SearchContextDefinitions(ctx, in.ProjectID, in.Query, in.Limit)
		return nil, definitionSearchOutput{Definitions: v}, e
	})
	addTool(a, server, readTool("platform_metric_query", "Query governed metric", "Execute a reviewed fixed SQL metric by ID/version, project, half-open UTC window and allowed dimensions. No arbitrary SQL."), func(ctx context.Context, _ *mcp.CallToolRequest, in domain.MetricRequest) (*mcp.CallToolResult, domain.MetricResult, error) {
		v, e := a.service.PlatformMetric(ctx, in)
		return nil, v, e
	})
	addTool(a, server, writeTool("workflow_task_context_record", "Record actual knowledge use", "Record exact eligible knowledge used by the active local task; verify source applicability to the target revision."), func(ctx context.Context, _ *mcp.CallToolRequest, in service.RecordTaskContextInput) (*mcp.CallToolResult, contextUseOutput, error) {
		e := a.service.RecordTaskContext(ctx, in)
		return nil, contextUseOutput{Recorded: e == nil}, e
	})
	server.AddResource(&mcp.Resource{URI: "context://registry/v1", Name: "context-registry-v1", MIMEType: "application/json", Description: "Source-controlled definitions, not approval authority. Query eligibility comes from project-scoped PostgreSQL decisions."}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		defs, sha, e := contextregistry.Load()
		if e != nil {
			return nil, e
		}
		raw, e := json.Marshal(map[string]any{"registry_sha256": sha, "authority": "source_controlled_proposal; project approval required", "definitions": defs})
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "context://registry/v1", MIMEType: "application/json", Text: string(raw)}}}, e
	})
}
