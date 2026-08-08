package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const Version = "0.1.0"

type API struct {
	service *service.Service
}

func New(svc *service.Service) *mcp.Server {
	api := &API{service: svc}
	server := mcp.NewServer(&mcp.Implementation{
		Name: "hybrid-ai-knowledge", Title: "Hybrid AI Knowledge Gateway", Version: Version,
		Description: "Captures reviewed software-development knowledge and retrieves approved guidance for local or cloud agents.",
	}, &mcp.ServerOptions{
		Instructions: "Search approved knowledge before solving a task. Capture useful final outputs with generation_capture. Never treat pending candidates as approved facts.",
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
	mcp.AddTool(server, writeTool("generation_capture", "Capture a generation", "Persist a prompt, generated response, provenance, and a review candidate. This is additive and does not approve the candidate."), a.capture)
	mcp.AddTool(server, writeTool("repository_relation_upsert", "Record repository relationship", "Upsert two Git repositories and an approved, evidence-backed relationship; vector indexing is queued transactionally."), a.repositoryRelationUpsert)
	mcp.AddTool(server, writeTool("review_record", "Record review feedback", "Attach Codex, ChatGPT, Kimi, or human review feedback to a candidate without approving it."), a.review)
	mcp.AddTool(server, writeTool("knowledge_candidate_decide", "Approve or reject candidate", "Approve or reject a pending candidate. Approval schedules indexing into Milvus."), a.decide)
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
	return nil, getOutput{Item: item}, err
}

type captureInput struct {
	ProjectID          string   `json:"project_id" jsonschema:"project namespace; required"`
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
	item, err := a.service.Capture(ctx, service.CaptureInput{
		ProjectID: input.ProjectID, SessionID: input.SessionID, TaskType: input.TaskType,
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
	items, err := a.service.ListCandidates(ctx, input.ProjectID, input.Limit)
	return nil, listCandidatesOutput{Count: len(items), Items: items}, err
}

type reviewInput struct {
	KnowledgeID     string `json:"knowledge_id" jsonschema:"candidate UUID; required"`
	Reviewer        string `json:"reviewer" jsonschema:"reviewing user or agent identity; required"`
	Provider        string `json:"provider,omitempty" jsonschema:"review provider"`
	Model           string `json:"model,omitempty" jsonschema:"review model identifier"`
	Verdict         string `json:"verdict" jsonschema:"one of approve, reject, revise, comment; required"`
	Comments        string `json:"comments,omitempty" jsonschema:"review rationale and findings"`
	ImprovedContent string `json:"improved_content,omitempty" jsonschema:"suggested improved response or code"`
}
type reviewOutput struct {
	Recorded bool   `json:"recorded"`
	Message  string `json:"message"`
}

func (a *API) review(ctx context.Context, _ *mcp.CallToolRequest, input reviewInput) (*mcp.CallToolResult, reviewOutput, error) {
	verdict := strings.ToLower(strings.TrimSpace(input.Verdict))
	if verdict != "approve" && verdict != "reject" && verdict != "revise" && verdict != "comment" {
		return nil, reviewOutput{}, fmt.Errorf("verdict must be approve, reject, revise, or comment")
	}
	err := a.service.RecordReview(ctx, domain.ReviewRecord{
		KnowledgeID: input.KnowledgeID, Reviewer: input.Reviewer, Provider: input.Provider,
		Model: input.Model, Verdict: verdict, Comments: input.Comments, ImprovedContent: input.ImprovedContent,
	})
	return nil, reviewOutput{Recorded: err == nil, Message: "Review recorded; use knowledge_candidate_decide for the approval gate."}, err
}

type decideInput struct {
	KnowledgeID string `json:"knowledge_id" jsonschema:"pending candidate UUID; required"`
	Decision    string `json:"decision" jsonschema:"approve or reject; required"`
	Actor       string `json:"actor" jsonschema:"accountable human or policy identity; required"`
}
type decideOutput struct {
	KnowledgeID string `json:"knowledge_id"`
	Status      string `json:"status"`
}

func (a *API) decide(ctx context.Context, _ *mcp.CallToolRequest, input decideInput) (*mcp.CallToolResult, decideOutput, error) {
	decision := strings.ToLower(strings.TrimSpace(input.Decision))
	var item domain.KnowledgeItem
	var err error
	switch decision {
	case "approve":
		item, err = a.service.Approve(ctx, input.KnowledgeID, input.Actor)
	case "reject":
		item, err = a.service.Reject(ctx, input.KnowledgeID, input.Actor)
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
	ApprovedBy   string          `json:"approved_by" jsonschema:"accountable human or policy identity; required"`
}
type repositoryRelationUpsertOutput struct {
	Relation   domain.RepositoryRelation `json:"relation"`
	IndexState string                    `json:"index_state"`
}

func (a *API) repositoryRelationUpsert(ctx context.Context, _ *mcp.CallToolRequest, input repositoryRelationUpsertInput) (*mcp.CallToolResult, repositoryRelationUpsertOutput, error) {
	relation, err := a.service.UpsertRepositoryRelation(ctx, domain.RepositoryRelation{
		ProjectID:    input.ProjectID,
		From:         domain.SoftwareRepository{Name: input.From.Name, CanonicalURL: input.From.CanonicalURL, DefaultBranch: input.From.DefaultBranch, Revision: input.From.Revision},
		To:           domain.SoftwareRepository{Name: input.To.Name, CanonicalURL: input.To.CanonicalURL, DefaultBranch: input.To.DefaultBranch, Revision: input.To.Revision},
		RelationType: input.RelationType, Evidence: input.Evidence, Confidence: input.Confidence, ApprovedBy: input.ApprovedBy,
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
	relations, err := a.service.RepositoryGraph(ctx, input.ProjectID, input.Root, input.Depth)
	return nil, repositoryRelationsOutput{Count: len(relations), Relations: relations}, err
}

type repositoryRelationSearchInput struct {
	ProjectID string `json:"project_id" jsonschema:"software product or solution namespace; required"`
	Query     string `json:"query" jsonschema:"semantic relationship question; required"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum results from 1 to 25"`
}

func (a *API) repositoryRelationSearch(ctx context.Context, _ *mcp.CallToolRequest, input repositoryRelationSearchInput) (*mcp.CallToolResult, repositoryRelationsOutput, error) {
	relations, err := a.service.SearchRepositoryRelations(ctx, input.ProjectID, input.Query, input.Limit)
	return nil, repositoryRelationsOutput{Count: len(relations), Relations: relations}, err
}
