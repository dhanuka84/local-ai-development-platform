package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const Version = "0.2.0"

type API struct {
	service          *service.Service
	defaultPrincipal domain.Principal
}

func New(svc *service.Service, defaultPrincipals ...domain.Principal) *mcp.Server {
	api := &API{service: svc}
	if len(defaultPrincipals) > 0 {
		api.defaultPrincipal = defaultPrincipals[0]
	}
	server := mcp.NewServer(&mcp.Implementation{
		Name: "hybrid-ai-knowledge", Title: "Hybrid AI Knowledge Gateway", Version: Version,
		Description: "Captures reviewed software-development knowledge and retrieves approved guidance for local or cloud agents.",
	}, &mcp.ServerOptions{
		Instructions: "Search approved knowledge and relevant code symbols before solving a task. Use code_graph_get for exact topology after semantic discovery. Capture useful final outputs with generation_capture. Never treat pending candidates or vector similarity as authoritative facts.",
	})
	api.register(server)
	return server
}

func (a *API) register(server *mcp.Server) {
	mcp.AddTool(server, readTool("platform_status", "Platform status", "Check PostgreSQL, Ollama, and Milvus connectivity."), a.status)
	mcp.AddTool(server, readTool("knowledge_search", "Search approved knowledge", "Search only approved, project-scoped software-development knowledge."), a.search)
	mcp.AddTool(server, readTool("knowledge_get", "Get knowledge", "Fetch one approved knowledge item by ID."), a.get)
	mcp.AddTool(server, readTool("knowledge_candidates_list", "List review candidates", "List pending knowledge candidates for review."), a.listCandidates)
	mcp.AddTool(server, readTool("repository_graph_get", "Get repository graph", "Traverse typed, evidence-backed relationships around a Git repository."), a.repositoryGraph)
	mcp.AddTool(server, readTool("repository_relation_search", "Search repository relationships", "Semantically search approved Git repository relationships in Milvus."), a.repositoryRelationSearch)
	mcp.AddTool(server, readTool("code_symbol_search", "Search code symbols", "Semantically search active, revision-specific source symbols with PostgreSQL lexical fallback."), a.codeSymbolSearch)
	mcp.AddTool(server, readTool("code_graph_get", "Get code graph", "Traverse calls, references, implementations, imports, tests, and containment around a source symbol."), a.codeGraph)
	mcp.AddTool(server, writeTool("generation_capture", "Capture a generation", "Persist a prompt, generated response, provenance, and a review candidate. This is additive and does not approve the candidate."), a.capture)
	mcp.AddTool(server, writeTool("repository_relation_upsert", "Record repository relationship", "Upsert two Git repositories and an approved, evidence-backed relationship; vector indexing is queued transactionally."), a.repositoryRelationUpsert)
	if a.service.CodeGraphAnalysisEnabled() {
		mcp.AddTool(server, writeTool("code_repository_index", "Index a code repository", "Analyze an allowlisted local Go, Java, Kotlin, TypeScript, JavaScript, or Python repository, persist a repository-, branch-, and revision-mapped graph in PostgreSQL, and queue symbol embeddings."), a.codeRepositoryIndex)
	}
	mcp.AddTool(server, writeTool("review_record", "Record review feedback", "Attach Codex, ChatGPT, Kimi, or human review feedback to a candidate without approving it."), a.review)
	mcp.AddTool(server, writeTool("knowledge_candidate_decide", "Approve or reject candidate", "Approve or reject a pending candidate. Approval schedules indexing into Milvus."), a.decide)
	mcp.AddTool(server, writeTool("workflow_run_create", "Create workflow run", "Create an idempotent, project-scoped agentic workflow under the authenticated principal."), a.workflowCreate)
	mcp.AddTool(server, readTool("workflow_run_get", "Get workflow run", "Read authoritative workflow state and governance policy."), a.workflowGet)
	mcp.AddTool(server, writeTool("workflow_run_transition", "Transition workflow run", "Request an optimistic, idempotent state transition with authorization and evidence."), a.workflowTransition)
}

func (a *API) context(ctx context.Context) context.Context {
	if _, ok := identity.PrincipalFromContext(ctx); !ok && a.defaultPrincipal.ID != "" {
		return identity.WithPrincipal(ctx, a.defaultPrincipal)
	}
	return ctx
}

func readTool(name, title, description string) *mcp.Tool {
	closed := false
	return &mcp.Tool{Name: name, Title: title, Description: description, Annotations: &mcp.ToolAnnotations{
		Title: title, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed,
	}}
}

func writeTool(name, title, description string) *mcp.Tool {
	destructive, closed := false, false
	return &mcp.Tool{Name: name, Title: title, Description: description, Annotations: &mcp.ToolAnnotations{
		Title: title, ReadOnlyHint: false, DestructiveHint: &destructive, OpenWorldHint: &closed,
	}}
}

type statusInput struct{}
type statusOutput struct {
	Status       string            `json:"status"`
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies"`
}

func (a *API) status(ctx context.Context, _ *mcp.CallToolRequest, _ statusInput) (*mcp.CallToolResult, statusOutput, error) {
	dependencies := a.service.Dependencies(ctx)
	status := "ok"
	for _, value := range dependencies {
		if value != "ok" {
			status = "degraded"
			break
		}
	}
	return nil, statusOutput{Status: status, Version: Version, Dependencies: dependencies}, nil
}

type searchInput struct {
	ProjectID string `json:"project_id" jsonschema:"project namespace; required"`
	Query     string `json:"query" jsonschema:"software-development question or task; required"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum results from 1 to 25"`
}
type searchOutput struct {
	Backend string             `json:"backend"`
	Count   int                `json:"count"`
	Results []domain.SearchHit `json:"results"`
}

func (a *API) search(ctx context.Context, _ *mcp.CallToolRequest, input searchInput) (*mcp.CallToolResult, searchOutput, error) {
	ctx = a.context(ctx)
	if _, err := a.service.AuthorizeProjectAction(ctx, input.ProjectID, "knowledge_candidate", "search", "read", nil); err != nil {
		return nil, searchOutput{}, err
	}
	results, backend, err := a.service.Search(ctx, input.ProjectID, input.Query, input.Limit)
	return nil, searchOutput{Backend: backend, Count: len(results), Results: results}, err
}

type getInput struct {
	ID string `json:"id" jsonschema:"knowledge item UUID; required"`
}
type getOutput struct {
	Item domain.KnowledgeItem `json:"item"`
}

func (a *API) get(ctx context.Context, _ *mcp.CallToolRequest, input getInput) (*mcp.CallToolResult, getOutput, error) {
	item, err := a.service.Get(ctx, input.ID, false)
	if err == nil {
		_, err = a.service.AuthorizeProjectAction(a.context(ctx), item.ProjectID, "knowledge_candidate", item.ID, "read", map[string]any{"status": item.Status})
	}
	return nil, getOutput{Item: item}, err
}

type captureInput struct {
	ProjectID          string   `json:"project_id" jsonschema:"project namespace; required"`
	WorkflowID         string   `json:"workflow_id,omitempty" jsonschema:"authoritative workflow UUID"`
	WorkflowStepID     string   `json:"workflow_step_id,omitempty" jsonschema:"authoritative workflow step UUID"`
	SessionID          string   `json:"session_id,omitempty" jsonschema:"originating agent session"`
	TaskType           string   `json:"task_type,omitempty" jsonschema:"task category such as implementation, debugging, maintenance, or review"`
	Prompt             string   `json:"prompt" jsonschema:"original user or agent input; required"`
	Response           string   `json:"response" jsonschema:"generated final response or implementation guidance; required"`
	Summary            string   `json:"summary,omitempty" jsonschema:"short reusable description"`
	Language           string   `json:"language,omitempty" jsonschema:"primary programming language"`
	Tags               []string `json:"tags,omitempty" jsonschema:"retrieval tags"`
	Provider           string   `json:"provider,omitempty" jsonschema:"generator provider such as openai, kimi, or local"`
	Model              string   `json:"model,omitempty" jsonschema:"generator model identifier"`
	RepositoryRevision string   `json:"repository_revision,omitempty" jsonschema:"source repository commit or revision"`
	Outcome            string   `json:"outcome,omitempty" jsonschema:"success, partial, or failure"`
	Procedure          []string `json:"procedure,omitempty" jsonschema:"ordered reusable steps and significant tool actions"`
	ValidationEvidence []string `json:"validation_evidence,omitempty" jsonschema:"tests, builds, checks, and observed outcomes"`
}
type captureOutput struct {
	CandidateID string `json:"candidate_id"`
	Status      string `json:"status"`
	Message     string `json:"message"`
}

func (a *API) capture(ctx context.Context, _ *mcp.CallToolRequest, input captureInput) (*mcp.CallToolResult, captureOutput, error) {
	ctx = a.context(ctx)
	if _, err := a.service.AuthorizeProjectAction(ctx, input.ProjectID, "knowledge_candidate", "new", "capture", map[string]any{
		"workflow_id": input.WorkflowID,
	}); err != nil {
		return nil, captureOutput{}, err
	}
	item, err := a.service.Capture(ctx, service.CaptureInput{
		ProjectID: input.ProjectID, WorkflowID: input.WorkflowID, WorkflowStepID: input.WorkflowStepID,
		SessionID: input.SessionID, TaskType: input.TaskType,
		Prompt: input.Prompt, Response: input.Response, Summary: input.Summary, Language: input.Language,
		Tags: input.Tags, Provider: input.Provider, Model: input.Model,
		RepositoryRevision: input.RepositoryRevision, Outcome: input.Outcome,
		Procedure: input.Procedure, ValidationEvidence: input.ValidationEvidence,
	})
	return nil, captureOutput{CandidateID: item.ID, Status: item.Status, Message: "Captured with immutable artifacts and provenance."}, err
}

type listCandidatesInput struct {
	ProjectID string `json:"project_id,omitempty" jsonschema:"optional project filter"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum results from 1 to 100"`
}
type listCandidatesOutput struct {
	Count int                    `json:"count"`
	Items []domain.KnowledgeItem `json:"items"`
}

func (a *API) listCandidates(ctx context.Context, _ *mcp.CallToolRequest, input listCandidatesInput) (*mcp.CallToolResult, listCandidatesOutput, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		projectID = "*"
	}
	if _, err := a.service.AuthorizeProjectAction(a.context(ctx), projectID, "knowledge_candidate", "pending", "read", map[string]any{"status": "pending"}); err != nil {
		return nil, listCandidatesOutput{}, err
	}
	items, err := a.service.ListCandidates(ctx, input.ProjectID, input.Limit)
	return nil, listCandidatesOutput{Count: len(items), Items: items}, err
}

type reviewInput struct {
	KnowledgeID        string   `json:"knowledge_id" jsonschema:"candidate UUID; required"`
	WorkflowID         string   `json:"workflow_id,omitempty" jsonschema:"authoritative workflow UUID"`
	WorkflowStepID     string   `json:"workflow_step_id,omitempty" jsonschema:"authoritative workflow step UUID"`
	Provider           string   `json:"provider,omitempty" jsonschema:"review provider"`
	Model              string   `json:"model,omitempty" jsonschema:"review model identifier"`
	Verdict            string   `json:"verdict" jsonschema:"one of approve, reject, revise, comment; required"`
	Comments           string   `json:"comments,omitempty" jsonschema:"review rationale and findings"`
	ImprovedContent    string   `json:"improved_content,omitempty" jsonschema:"suggested improved response or code"`
	ValidationEvidence []string `json:"validation_evidence,omitempty" jsonschema:"fresh local checks for improved_content; required for revise"`
	RawOutput          string   `json:"raw_output,omitempty" jsonschema:"exact unmodified reviewer response; comments are used when omitted"`
	ContextManifest    string   `json:"context_manifest,omitempty" jsonschema:"sanitized JSON manifest of context disclosed to the reviewer"`
}
type reviewOutput struct {
	Recorded                bool             `json:"recorded"`
	ReviewArtifact          *domain.Artifact `json:"review_artifact,omitempty"`
	ContextManifestArtifact *domain.Artifact `json:"context_manifest_artifact,omitempty"`
	Message                 string           `json:"message"`
}

func (a *API) review(ctx context.Context, _ *mcp.CallToolRequest, input reviewInput) (*mcp.CallToolResult, reviewOutput, error) {
	verdict := strings.ToLower(strings.TrimSpace(input.Verdict))
	if verdict != "approve" && verdict != "reject" && verdict != "revise" && verdict != "comment" {
		return nil, reviewOutput{}, fmt.Errorf("verdict must be approve, reject, revise, or comment")
	}
	ctx = a.context(ctx)
	item, err := a.service.Get(ctx, input.KnowledgeID, true)
	if err != nil {
		return nil, reviewOutput{}, err
	}
	principal, err := a.service.AuthorizeProjectAction(ctx, item.ProjectID, "knowledge_candidate", item.ID, "review", map[string]any{
		"status": item.Status, "workflow_id": input.WorkflowID,
	})
	if err != nil {
		return nil, reviewOutput{}, err
	}
	review, err := a.service.RecordReview(ctx, domain.ReviewRecord{
		KnowledgeID: input.KnowledgeID, WorkflowID: input.WorkflowID, WorkflowStepID: input.WorkflowStepID,
		Reviewer: principal.ID, Provider: input.Provider,
		Model: input.Model, Verdict: verdict, Comments: input.Comments, ImprovedContent: input.ImprovedContent,
		ValidationEvidence: input.ValidationEvidence, RawOutput: input.RawOutput, ContextManifest: input.ContextManifest,
	})
	output := reviewOutput{Recorded: err == nil, Message: "Review and immutable evidence recorded; use knowledge_candidate_decide for the approval gate."}
	if review.ReviewArtifact.SHA256 != "" {
		output.ReviewArtifact = &review.ReviewArtifact
	}
	if review.ContextManifestArtifact.SHA256 != "" {
		output.ContextManifestArtifact = &review.ContextManifestArtifact
	}
	return nil, output, err
}

type decideInput struct {
	KnowledgeID string `json:"knowledge_id" jsonschema:"pending candidate UUID; required"`
	Decision    string `json:"decision" jsonschema:"approve or reject; required"`
}
type decideOutput struct {
	KnowledgeID string `json:"knowledge_id"`
	Status      string `json:"status"`
}

func (a *API) decide(ctx context.Context, _ *mcp.CallToolRequest, input decideInput) (*mcp.CallToolResult, decideOutput, error) {
	decision := strings.ToLower(strings.TrimSpace(input.Decision))
	if decision != "approve" && decision != "reject" {
		return nil, decideOutput{}, fmt.Errorf("decision must be approve or reject")
	}
	ctx = a.context(ctx)
	candidate, err := a.service.Get(ctx, input.KnowledgeID, true)
	if err != nil {
		return nil, decideOutput{}, err
	}
	workflowLinked, qaValidated := candidate.WorkflowID != "", false
	if workflowLinked {
		workflow, workflowErr := a.service.GetWorkflow(ctx, candidate.WorkflowID)
		if workflowErr != nil {
			return nil, decideOutput{}, workflowErr
		}
		qaValidated = workflow.QAValidatedBy != "" && workflow.State == "promotion_pending"
	}
	principal, err := a.service.AuthorizeProjectAction(ctx, candidate.ProjectID, "knowledge_candidate", candidate.ID, decision, map[string]any{
		"status": candidate.Status, "workflow_id": candidate.WorkflowID,
		"workflow_linked": workflowLinked, "qa_validated": qaValidated,
	})
	if err != nil {
		return nil, decideOutput{}, err
	}
	var item domain.KnowledgeItem
	switch decision {
	case "approve":
		item, err = a.service.Approve(ctx, input.KnowledgeID, principal.ID)
	case "reject":
		item, err = a.service.Reject(ctx, input.KnowledgeID, principal.ID)
	default:
		err = fmt.Errorf("decision must be approve or reject")
	}
	return nil, decideOutput{KnowledgeID: item.ID, Status: item.Status}, err
}

type repositoryInput struct {
	Name          string `json:"name" jsonschema:"repository display name; required"`
	CanonicalURL  string `json:"canonical_url" jsonschema:"canonical HTTPS, SSH, or local Git remote; required"`
	DefaultBranch string `json:"default_branch,omitempty" jsonschema:"default branch name"`
	Revision      string `json:"revision,omitempty" jsonschema:"observed commit SHA or version"`
}

type repositoryRelationUpsertInput struct {
	ProjectID    string          `json:"project_id" jsonschema:"software product or solution namespace; required"`
	From         repositoryInput `json:"from" jsonschema:"source repository; required"`
	To           repositoryInput `json:"to" jsonschema:"target repository; required"`
	RelationType string          `json:"relation_type" jsonschema:"depends_on, provides_api_to, deploys_with, shares_contract, fork_of, upstream_of, successor_of, contains, or related_to; required"`
	Evidence     string          `json:"evidence" jsonschema:"manifest path, API reference, build config, Git evidence, or reviewed rationale; required"`
	Confidence   float32         `json:"confidence,omitempty" jsonschema:"confidence from 0 to 1; zero defaults to 1"`
}
type repositoryRelationUpsertOutput struct {
	Relation   domain.RepositoryRelation `json:"relation"`
	IndexState string                    `json:"index_state"`
}

func (a *API) repositoryRelationUpsert(ctx context.Context, _ *mcp.CallToolRequest, input repositoryRelationUpsertInput) (*mcp.CallToolResult, repositoryRelationUpsertOutput, error) {
	ctx = a.context(ctx)
	principal, err := a.service.AuthorizeProjectAction(ctx, input.ProjectID, "repository_relation", input.From.CanonicalURL+"->"+input.To.CanonicalURL, "upsert", nil)
	if err != nil {
		return nil, repositoryRelationUpsertOutput{}, err
	}
	relation, err := a.service.UpsertRepositoryRelation(ctx, domain.RepositoryRelation{
		ProjectID:    input.ProjectID,
		From:         domain.SoftwareRepository{Name: input.From.Name, CanonicalURL: input.From.CanonicalURL, DefaultBranch: input.From.DefaultBranch, Revision: input.From.Revision},
		To:           domain.SoftwareRepository{Name: input.To.Name, CanonicalURL: input.To.CanonicalURL, DefaultBranch: input.To.DefaultBranch, Revision: input.To.Revision},
		RelationType: input.RelationType, Evidence: input.Evidence, Confidence: input.Confidence, ApprovedBy: principal.ID,
	})
	return nil, repositoryRelationUpsertOutput{Relation: relation, IndexState: "queued"}, err
}

type repositoryGraphInput struct {
	ProjectID string `json:"project_id" jsonschema:"software product or solution namespace; required"`
	Root      string `json:"root" jsonschema:"repository UUID, canonical URL, or exact name; required"`
	Depth     int    `json:"depth,omitempty" jsonschema:"traversal depth from 1 to 5"`
}
type repositoryRelationsOutput struct {
	Count     int                         `json:"count"`
	Relations []domain.RepositoryRelation `json:"relations"`
}

func (a *API) repositoryGraph(ctx context.Context, _ *mcp.CallToolRequest, input repositoryGraphInput) (*mcp.CallToolResult, repositoryRelationsOutput, error) {
	if _, err := a.service.AuthorizeProjectAction(a.context(ctx), input.ProjectID, "repository_relation", input.Root, "read", nil); err != nil {
		return nil, repositoryRelationsOutput{}, err
	}
	relations, err := a.service.RepositoryGraph(ctx, input.ProjectID, input.Root, input.Depth)
	return nil, repositoryRelationsOutput{Count: len(relations), Relations: relations}, err
}

type repositoryRelationSearchInput struct {
	ProjectID string `json:"project_id" jsonschema:"software product or solution namespace; required"`
	Query     string `json:"query" jsonschema:"semantic relationship question; required"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum results from 1 to 25"`
}

func (a *API) repositoryRelationSearch(ctx context.Context, _ *mcp.CallToolRequest, input repositoryRelationSearchInput) (*mcp.CallToolResult, repositoryRelationsOutput, error) {
	if _, err := a.service.AuthorizeProjectAction(a.context(ctx), input.ProjectID, "repository_relation", "search", "read", nil); err != nil {
		return nil, repositoryRelationsOutput{}, err
	}
	relations, err := a.service.SearchRepositoryRelations(ctx, input.ProjectID, input.Query, input.Limit)
	return nil, repositoryRelationsOutput{Count: len(relations), Relations: relations}, err
}

type codeRepositoryIndexInput struct {
	ProjectID      string          `json:"project_id" jsonschema:"software product or solution namespace; required"`
	Repository     repositoryInput `json:"repository" jsonschema:"repository identity; required"`
	RepositoryPath string          `json:"repository_path" jsonschema:"local path below CODEGRAPH_ALLOWED_ROOTS; required"`
	Branch         string          `json:"branch,omitempty" jsonschema:"expected checked-out Git branch; analysis fails if it does not match"`
	Revision       string          `json:"revision,omitempty" jsonschema:"expected Git commit; analysis fails if it does not match"`
	AllowDirty     bool            `json:"allow_dirty,omitempty" jsonschema:"permit uncommitted files and fingerprint the dirty snapshot"`
}

type codeRepositoryIndexOutput struct {
	Analysis   domain.CodeAnalysis `json:"analysis"`
	IndexState string              `json:"index_state"`
}

func (a *API) codeRepositoryIndex(ctx context.Context, _ *mcp.CallToolRequest, input codeRepositoryIndexInput) (*mcp.CallToolResult, codeRepositoryIndexOutput, error) {
	ctx = a.context(ctx)
	principal, err := a.service.AuthorizeProjectAction(ctx, input.ProjectID, "code_repository", input.Repository.CanonicalURL, "index", nil)
	if err != nil {
		return nil, codeRepositoryIndexOutput{}, err
	}
	analysis, err := a.service.IndexCodeRepository(ctx, service.CodeIndexInput{
		ProjectID: input.ProjectID,
		Repository: domain.SoftwareRepository{
			Name: input.Repository.Name, CanonicalURL: input.Repository.CanonicalURL,
			DefaultBranch: input.Repository.DefaultBranch,
		},
		RepositoryPath: input.RepositoryPath, Branch: input.Branch, Revision: input.Revision,
		AllowDirty: input.AllowDirty, RequestedBy: principal.ID,
	})
	return nil, codeRepositoryIndexOutput{Analysis: analysis, IndexState: "queued"}, err
}

type codeSymbolSearchInput struct {
	ProjectID    string `json:"project_id" jsonschema:"software product or solution namespace; required"`
	RepositoryID string `json:"repository_id,omitempty" jsonschema:"optional repository UUID filter"`
	Query        string `json:"query" jsonschema:"symbol name, behavior, or programming concept; required"`
	Limit        int    `json:"limit,omitempty" jsonschema:"maximum results from 1 to 50"`
}

type codeSymbolSearchOutput struct {
	Backend string              `json:"backend"`
	Count   int                 `json:"count"`
	Results []domain.CodeEntity `json:"results"`
}

func (a *API) codeSymbolSearch(ctx context.Context, _ *mcp.CallToolRequest, input codeSymbolSearchInput) (*mcp.CallToolResult, codeSymbolSearchOutput, error) {
	if _, err := a.service.AuthorizeProjectAction(a.context(ctx), input.ProjectID, "code_repository", defaultResourceID(input.RepositoryID, "search"), "read", nil); err != nil {
		return nil, codeSymbolSearchOutput{}, err
	}
	entities, backend, err := a.service.SearchCodeEntities(ctx, input.ProjectID, input.RepositoryID, input.Query, input.Limit)
	return nil, codeSymbolSearchOutput{Backend: backend, Count: len(entities), Results: entities}, err
}

type codeGraphInput struct {
	ProjectID  string `json:"project_id" jsonschema:"software product or solution namespace; required"`
	Repository string `json:"repository" jsonschema:"repository UUID, canonical URL, or exact name; required"`
	Symbol     string `json:"symbol" jsonschema:"entity UUID, stable key, qualified name, or exact name; required"`
	Depth      int    `json:"depth,omitempty" jsonschema:"bidirectional traversal depth from 1 to 5"`
}

type codeGraphOutput struct {
	Graph domain.CodeGraph `json:"graph"`
}

func (a *API) codeGraph(ctx context.Context, _ *mcp.CallToolRequest, input codeGraphInput) (*mcp.CallToolResult, codeGraphOutput, error) {
	if _, err := a.service.AuthorizeProjectAction(a.context(ctx), input.ProjectID, "code_repository", input.Repository, "read", nil); err != nil {
		return nil, codeGraphOutput{}, err
	}
	graph, err := a.service.CodeGraph(ctx, input.ProjectID, input.Repository, input.Symbol, input.Depth)
	return nil, codeGraphOutput{Graph: graph}, err
}

func defaultResourceID(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

type workflowCreateInput struct {
	ProjectID          string         `json:"project_id" jsonschema:"project namespace; required"`
	Kind               string         `json:"kind,omitempty" jsonschema:"workflow kind; defaults to software-development"`
	Risk               string         `json:"risk,omitempty" jsonschema:"low, medium, high, or critical"`
	DataClassification string         `json:"data_classification,omitempty" jsonschema:"public, internal, confidential, or restricted"`
	Request            string         `json:"request" jsonschema:"task request stored as an immutable artifact; required"`
	OpenClawFlowID     string         `json:"openclaw_flow_id,omitempty" jsonschema:"managed OpenClaw Task Flow ID"`
	IdempotencyKey     string         `json:"idempotency_key" jsonschema:"stable trigger key; required"`
	Metadata           map[string]any `json:"metadata,omitempty" jsonschema:"bounded non-secret workflow metadata"`
}

type workflowRunOutput struct {
	Workflow domain.WorkflowRun `json:"workflow"`
}

func (a *API) workflowCreate(ctx context.Context, _ *mcp.CallToolRequest, input workflowCreateInput) (*mcp.CallToolResult, workflowRunOutput, error) {
	run, err := a.service.CreateWorkflow(a.context(ctx), service.CreateWorkflowInput{
		ProjectID: input.ProjectID, Kind: input.Kind, Risk: input.Risk,
		DataClassification: input.DataClassification, Request: input.Request,
		OpenClawFlowID: input.OpenClawFlowID, IdempotencyKey: input.IdempotencyKey, Metadata: input.Metadata,
	})
	return nil, workflowRunOutput{Workflow: run}, err
}

type workflowGetInput struct {
	WorkflowID string `json:"workflow_id" jsonschema:"workflow UUID; required"`
}

func (a *API) workflowGet(ctx context.Context, _ *mcp.CallToolRequest, input workflowGetInput) (*mcp.CallToolResult, workflowRunOutput, error) {
	run, err := a.service.GetWorkflow(a.context(ctx), input.WorkflowID)
	return nil, workflowRunOutput{Workflow: run}, err
}

type workflowTransitionInput struct {
	WorkflowID      string         `json:"workflow_id" jsonschema:"workflow UUID; required"`
	ExpectedVersion int            `json:"expected_version" jsonschema:"current optimistic workflow version; required"`
	EventType       string         `json:"event_type" jsonschema:"state-machine event; required"`
	IdempotencyKey  string         `json:"idempotency_key" jsonschema:"stable step attempt key; required"`
	Evidence        string         `json:"evidence,omitempty" jsonschema:"exact JSON/text evidence stored immutably; required by verification and human gates"`
	Payload         map[string]any `json:"payload,omitempty" jsonschema:"bounded transition metadata"`
}

type workflowTransitionOutput struct {
	Workflow domain.WorkflowRun   `json:"workflow"`
	Event    domain.WorkflowEvent `json:"event"`
}

func (a *API) workflowTransition(ctx context.Context, _ *mcp.CallToolRequest, input workflowTransitionInput) (*mcp.CallToolResult, workflowTransitionOutput, error) {
	run, event, err := a.service.TransitionWorkflow(a.context(ctx), service.TransitionWorkflowInput{
		WorkflowID: input.WorkflowID, ExpectedVersion: input.ExpectedVersion, EventType: input.EventType,
		IdempotencyKey: input.IdempotencyKey, Evidence: input.Evidence, Payload: input.Payload,
	})
	return nil, workflowTransitionOutput{Workflow: run, Event: event}, err
}
