package mcpserver

import (
	"context"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type productGetInput struct {
	ProjectID   string `json:"project_id"`
	RecordID    string `json:"record_id"`
	CurrentOnly bool   `json:"current_only"`
}

func (a *API) registerProduct(server *mcp.Server) {
	addTool(a, server, readTool("product_intent_context", "Resolve accepted intent", "Check exact accepted requirements and protected criteria. Unresolved assumptions, missing context and superseded requirements prevent readiness; this does not execute or certify a task."), func(ctx context.Context, _ *mcp.CallToolRequest, in service.IntentContextInput) (*mcp.CallToolResult, domain.IntentContext, error) {
		out, err := a.service.IntentContext(ctx, in)
		return nil, out, err
	})
	addTool(a, server, writeTool("product_observations_compare", "Evaluate observed business behavior", "Execute an accepted versioned reconciliation criterion over authorized retained source observations. Record satisfaction, violation or uncertainty with immutable evidence; never infer a root cause or approve knowledge."), func(ctx context.Context, _ *mcp.CallToolRequest, in service.CompareObservationsInput) (*mcp.CallToolResult, domain.ObservationEvaluation, error) {
		out, err := a.service.CompareProductObservations(ctx, in)
		return nil, out, err
	})
	addTool(a, server, readTool("product_evaluation_get", "Read current observation evaluation", "Hydrate an immutable evaluation only while its intent, dependencies and source observations remain current and authorized."), func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ProjectID    string `json:"project_id"`
		EvaluationID string `json:"evaluation_id"`
	}) (*mcp.CallToolResult, domain.ObservationEvaluation, error) {
		out, err := a.service.GetProductEvaluation(ctx, in.ProjectID, in.EvaluationID)
		return nil, out, err
	})
	addTool(a, server, readTool("product_source_list", "Discover permitted product sources", "List configured source contracts and query limits without endpoints or credentials."), func(ctx context.Context, _ *mcp.CallToolRequest, in registryInput) (*mcp.CallToolResult, struct {
		Sources []domain.SourceDescriptor `json:"sources"`
	}, error) {
		out, err := a.service.ProductSources(ctx, in.ProjectID)
		return nil, struct {
			Sources []domain.SourceDescriptor `json:"sources"`
		}{out}, err
	})
	addTool(a, server, writeTool("product_source_query", "Collect evaluated product evidence", "Execute a bounded read through an operator-configured source adapter. Retain validated observations and immutable receipts; never execute arbitrary SQL or change consumer offsets."), func(ctx context.Context, _ *mcp.CallToolRequest, in domain.SourceQuery) (*mcp.CallToolResult, domain.SourceReceipt, error) {
		out, err := a.service.QueryProductSource(ctx, in)
		return nil, out, err
	})
	addTool(a, server, writeTool("product_record_put", "Propose product knowledge", "Create an immutable pending product/BRS/feature/code/test/release/incident record. It does not replace accepted context or approve generated knowledge."), func(ctx context.Context, _ *mcp.CallToolRequest, in domain.ProductRecordInput) (*mcp.CallToolResult, domain.ProductRecord, error) {
		out, err := a.service.PutProductRecord(ctx, in)
		return nil, out, err
	})
	addTool(a, server, readTool("product_record_get", "Get product record", "Hydrate a project-authorized exact product record. current_only checks publication, head and freshness eligibility."), func(ctx context.Context, _ *mcp.CallToolRequest, in productGetInput) (*mcp.CallToolResult, domain.ProductRecord, error) {
		out, err := a.service.GetProductRecord(ctx, in.ProjectID, in.RecordID, in.CurrentOnly)
		return nil, out, err
	})
	addTool(a, server, writeTool("product_record_validate", "Attest product record validation", "Human QA records exact-digest validation evidence. This is an attributed attestation, not a claim that the gateway executed tests."), func(ctx context.Context, _ *mcp.CallToolRequest, in service.ValidateProductInput) (*mcp.CallToolResult, domain.ProductValidation, error) {
		out, err := a.service.ValidateProductRecord(ctx, in)
		return nil, out, err
	})
	addTool(a, server, writeTool("product_record_decide", "Decide product record", "Explicit accountable human Product Owner decision on one exact validated record. General task authority and model review do not authorize generated-knowledge publication."), func(ctx context.Context, _ *mcp.CallToolRequest, in domain.ProductDecision) (*mcp.CallToolResult, domain.ProductRecord, error) {
		out, err := a.service.DecideProductRecord(ctx, in)
		return nil, out, err
	})
	addTool(a, server, writeTool("product_relation_put", "Link product evidence", "Link exact eligible records in one product. Supports and contradicts edges are evidence associations, not proof of causation."), func(ctx context.Context, _ *mcp.CallToolRequest, in domain.ProductRelation) (*mcp.CallToolResult, domain.ProductRelation, error) {
		out, err := a.service.PutProductRelation(ctx, in)
		return nil, out, err
	})
	addTool(a, server, readTool("product_context_search", "Build shared product context", "Combine local semantic discovery with bounded structural expansion; hydrate current authorized PostgreSQL versions, provenance and observation coverage."), func(ctx context.Context, _ *mcp.CallToolRequest, in domain.ProductContextRequest) (*mcp.CallToolResult, domain.ProductContext, error) {
		out, err := a.service.ProductContext(ctx, in)
		return nil, out, err
	})
}
