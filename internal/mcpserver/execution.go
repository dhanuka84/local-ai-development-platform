package mcpserver

import (
	"context"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (a *API) registerExecution(server *mcp.Server) {
	addTool(a, server, readTool("sdlc_trace_get", "Read execution audit", "Read bounded pages of correlated immutable operation evidence under an execution's access rules. A fixed through timestamp makes pagination stable."), func(ctx context.Context, _ *mcp.CallToolRequest, in domain.ExecutionTraceInput) (*mcp.CallToolResult, domain.ExecutionTracePage, error) {
		out, err := a.service.ExecutionTrace(ctx, in)
		return nil, out, err
	})
	addTool(a, server, readTool("sdlc_package_get", "Read package activation", "Read the configured package and exact active regression decision for a target role."), func(ctx context.Context, _ *mcp.CallToolRequest, in service.PackageGetInput) (*mcp.CallToolResult, service.PackageStatus, error) {
		out, err := a.service.GetPackageStatus(ctx, in)
		return nil, out, err
	})
	addTool(a, server, writeTool("sdlc_package_evaluate", "Evaluate an agent package", "Derive a regression campaign from exact qualified execution packages, independent outcomes and rejected cases. Fixture campaigns remain distinguishable from local model trials."), func(ctx context.Context, _ *mcp.CallToolRequest, in service.PackageEvaluateInput) (*mcp.CallToolResult, domain.PackageEvaluation, error) {
		out, err := a.service.EvaluatePackage(ctx, in)
		return nil, out, err
	})
	addTool(a, server, writeTool("sdlc_package_activate", "Activate or roll back a qualified package", "An accountable human with operations and QA authority can select a passed exact package campaign with a concurrency precondition. Prior decisions remain immutable."), func(ctx context.Context, _ *mcp.CallToolRequest, in service.PackageActivateInput) (*mcp.CallToolResult, domain.PackageActivation, error) {
		out, err := a.service.ActivatePackage(ctx, in)
		return nil, out, err
	})
	addTool(a, server, writeTool("sdlc_improvements_propose", "Propose outcome improvements", "Create pending requirement, test, runbook, package and KB proposals from a completed attributed execution. No publication or active package changes occur."), func(ctx context.Context, _ *mcp.CallToolRequest, in service.ExecutionIDInput) (*mcp.CallToolResult, service.ExecutionImprovements, error) {
		out, err := a.service.ProposeExecutionImprovements(ctx, in)
		return nil, out, err
	})
	addTool(a, server, readTool("sdlc_record_get", "Read scoped execution observation", "Hydrate an exact current product record under a live execution lease, including source field and purpose checks."), func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		domain.ExecutionLease
		RecordID string `json:"record_id"`
	}) (*mcp.CallToolResult, domain.ProductRecord, error) {
		out, err := a.service.GetExecutionRecord(ctx, in.ExecutionLease, in.RecordID)
		return nil, out, err
	})
	addTool(a, server, writeTool("sdlc_run_create", "Submit accepted SDLC intent", "Create an idempotent run with fixed criteria, operator-scoped workers and total budgets. Only an accountable human operator can grant execution."), func(ctx context.Context, _ *mcp.CallToolRequest, in service.ExecutionCreateInput) (*mcp.CallToolResult, domain.ExecutionRun, error) {
		out, err := a.service.CreateExecution(ctx, in)
		return nil, out, err
	})
	addTool(a, server, readTool("sdlc_run_get", "Read SDLC execution", "Read attributed progress, blockers, usage, steps and immutable evidence for an owned or assigned execution."), func(ctx context.Context, _ *mcp.CallToolRequest, in service.ExecutionIDInput) (*mcp.CallToolResult, domain.ExecutionView, error) {
		out, err := a.service.GetExecution(ctx, in)
		return nil, out, err
	})
	addTool(a, server, readTool("sdlc_run_list", "List SDLC executions", "List executions owned by or assigned to the authenticated principal."), func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		ProjectID string `json:"project_id"`
		Limit     int    `json:"limit"`
	}) (*mcp.CallToolResult, struct {
		Runs []domain.ExecutionRun `json:"runs"`
	}, error) {
		out, err := a.service.ListExecutions(ctx, in.ProjectID, in.Limit)
		return nil, struct {
			Runs []domain.ExecutionRun `json:"runs"`
		}{out}, err
	})
	addTool(a, server, writeTool("sdlc_step_claim", "Claim assigned execution stage", "Reserve a bounded lease and cumulative resources. Expired leases are fenced; external action identity is stable across recovery."), func(ctx context.Context, _ *mcp.CallToolRequest, in service.ExecutionIDInput) (*mcp.CallToolResult, domain.ExecutionClaim, error) {
		out, err := a.service.ClaimExecution(ctx, in)
		return nil, out, err
	})
	addTool(a, server, writeTool("sdlc_step_context", "Construct governed stage context", "Hydrate current accepted intent, structural and semantic product knowledge and approved lessons. Persist the exact bounded disclosure."), func(ctx context.Context, _ *mcp.CallToolRequest, in domain.ExecutionLease) (*mcp.CallToolResult, service.ExecutionContext, error) {
		out, err := a.service.ExecutionContext(ctx, in)
		return nil, out, err
	})
	addTool(a, server, writeTool("sdlc_artifact_put", "Retain execution evidence", "Retain exact bytes under a live scoped lease. An artifact is evidence, never approval or proof of successful execution by itself."), func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		domain.ExecutionLease
		Content   string `json:"content"`
		MediaType string `json:"media_type"`
	}) (*mcp.CallToolResult, domain.Artifact, error) {
		out, err := a.service.PutExecutionArtifact(ctx, in.ExecutionLease, in.Content, in.MediaType)
		return nil, out, err
	})
	addTool(a, server, readTool("sdlc_artifact_get", "Read execution evidence", "Read an exact artifact only through its authorized execution, never by an unrestricted global digest lookup."), func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		service.ExecutionIDInput
		SHA256 string `json:"sha256"`
	}) (*mcp.CallToolResult, struct {
		Content string `json:"content"`
	}, error) {
		out, err := a.service.GetExecutionArtifact(ctx, in.ExecutionIDInput, in.SHA256)
		return nil, struct {
			Content string `json:"content"`
		}{out}, err
	})
	addTool(a, server, writeTool("sdlc_step_complete", "Submit attributed stage evidence", "Validate stage-specific evidence and derive the next state. Builder submissions cannot certify product tests or complete a run."), func(ctx context.Context, _ *mcp.CallToolRequest, in service.ExecutionCompleteInput) (*mcp.CallToolResult, domain.ExecutionRun, error) {
		out, err := a.service.CompleteExecution(ctx, in)
		return nil, out, err
	})
	addTool(a, server, writeTool("sdlc_run_control", "Control SDLC execution", "Pause, cancel or resume an owned run using its current version. Changed accepted criteria block resume; interrupted writes require reconciliation."), func(ctx context.Context, _ *mcp.CallToolRequest, in service.ExecutionControlInput) (*mcp.CallToolResult, domain.ExecutionRun, error) {
		out, err := a.service.ControlExecution(ctx, in)
		return nil, out, err
	})
	addTool(a, server, writeTool("sdlc_source_query", "Collect scoped execution evidence", "Reserve cumulative query and row budgets before an authorized source read under the assigned diagnostic or recovery lease."), func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		domain.ExecutionLease
		Query domain.SourceQuery `json:"query"`
	}) (*mcp.CallToolResult, domain.SourceReceipt, error) {
		out, err := a.service.QueryExecutionSource(ctx, in.ExecutionLease, in.Query)
		return nil, out, err
	})
	addTool(a, server, writeTool("sdlc_observations_compare", "Evaluate execution business criteria", "Run the exact accepted observation oracle under the assigned execution lease, retaining independent deterministic business evidence."), func(ctx context.Context, _ *mcp.CallToolRequest, in struct {
		domain.ExecutionLease
		Comparison service.CompareObservationsInput `json:"comparison"`
	}) (*mcp.CallToolResult, domain.ObservationEvaluation, error) {
		out, err := a.service.CompareExecutionObservations(ctx, in.ExecutionLease, in.Comparison)
		return nil, out, err
	})
}
