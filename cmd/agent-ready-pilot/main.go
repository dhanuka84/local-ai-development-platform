// agent-ready-pilot runs only against an explicitly isolated local deployment.
// It never approves knowledge or impersonates a human product owner.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dhanuka84/hybrid-ai-platform/components/workpacket"
	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/config"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	"github.com/dhanuka84/hybrid-ai-platform/internal/platform"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
	"github.com/dhanuka84/hybrid-ai-platform/internal/telemetry"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type spec struct {
	ProjectID   string            `json:"project_id"`
	RunKey      string            `json:"run_key"`
	Model       string            `json:"model"`
	Branch      string            `json:"branch"`
	WorkflowID  string            `json:"workflow_id,omitempty"`
	TaskAID     string            `json:"task_a_id,omitempty"`
	TaskA       workpacket.Packet `json:"task_a"`
	TaskB       workpacket.Packet `json:"task_b"`
	RepairTaskA *patchRepair      `json:"repair_task_a,omitempty"`
}
type report struct {
	Status             string                `json:"status"`
	WorkflowID         string                `json:"workflow_id"`
	TaskAID            string                `json:"task_a_id"`
	TaskBID            string                `json:"task_b_id,omitempty"`
	KnowledgeID        string                `json:"knowledge_id,omitempty"`
	Version            int                   `json:"version,omitempty"`
	ValidationID       string                `json:"validation_id,omitempty"`
	Provider           string                `json:"provider"`
	Model              string                `json:"model"`
	RepositoryRevision string                `json:"repository_revision"`
	Trace              *domain.WorkflowTrace `json:"trace,omitempty"`
	Next               string                `json:"next,omitempty"`
}
type runner struct {
	app   *platform.Platform
	cfg   config.Config
	spec  spec
	store *artifacts.LocalStore
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string) error {
	if len(args) != 1 || os.Getenv("AGENT_READY_PILOT_ISOLATED") != "true" {
		return errors.New("usage: AGENT_READY_PILOT_ISOLATED=true agent-ready-pilot <spec.json>; requires an initialized disposable local deployment")
	}
	file, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer file.Close()
	var input spec
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&input); err != nil {
		return err
	}
	if !strings.HasPrefix(input.ProjectID, "pilot-") || input.RunKey == "" || input.Model == "" || input.Branch == "" {
		return errors.New("pilot project prefix, run_key, branch and explicit local model required")
	}
	if input.RepairTaskA != nil && (input.WorkflowID == "" || input.TaskAID == "") {
		return errors.New("explicit patch repair requires existing workflow_id and task_a_id")
	}
	for _, packet := range []workpacket.Packet{input.TaskA, input.TaskB} {
		if !packet.LocalOnly || packet.CloudReview || packet.Mode != workpacket.ModePatch || len(packet.Checks) == 0 || len(packet.AllowedFiles) == 0 {
			return errors.New("both packets require local-only scoped patch mode and executed checks")
		}
	}
	cfg, err := config.LoadCLI()
	if err != nil {
		return err
	}
	if cfg.AuthorizationMode != "cerbos" || !strings.HasPrefix(cfg.MilvusCollection, "pilot_") {
		return errors.New("pilot requires Cerbos and an isolated pilot_ Milvus collection")
	}
	if err = localEndpoint(ctx, cfg.OllamaURL); err != nil {
		return err
	}
	app, err := platform.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = app.Close(context.Background()) }()
	// Do not bootstrap identities or approve migrations implicitly. An operator
	// initializes the isolated deployment and supplies an existing workload token.
	hash := sha256.Sum256([]byte(cfg.AuthToken))
	principal, err := app.Repository.AuthenticatePrincipal(ctx, hash[:])
	if err != nil {
		return errors.New("authenticate the existing pilot workload")
	}
	if principal.Human || !principal.HasRole(input.ProjectID, "controller") || !principal.HasRole(input.ProjectID, "development") || !principal.HasRole(input.ProjectID, "validation_executor") || principal.HasRole(input.ProjectID, "product_owner") || principal.HasRole(input.ProjectID, "qa") {
		return errors.New("pilot must use a non-human development/controller/validation_executor identity without approval or attestation roles")
	}
	ctx = identity.WithPrincipal(ctx, principal)
	if err = app.Vectors.EnsureCollection(ctx); err != nil {
		return err
	}
	r := runner{app: app, cfg: cfg, spec: input, store: artifacts.NewLocalStore(cfg.ArtifactsPath)}
	out, err := r.execute(ctx)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return err
	}
	artifact, err := r.store.Put(ctx, raw, "application/json")
	if err != nil {
		return err
	}
	scope := domain.OperationScope{ProjectID: input.ProjectID, WorkflowID: out.WorkflowID}
	if err = app.Service.RecordOperationEvidence(domain.WithOperationScope(ctx, scope), "pilot.report", artifact); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func (r *runner) execute(ctx context.Context) (out report, err error) {
	out = report{Provider: "ollama", Model: r.spec.Model, RepositoryRevision: r.spec.TaskA.BaseRevision, WorkflowID: r.spec.WorkflowID, TaskAID: r.spec.TaskAID}
	if out.WorkflowID == "" {
		run, err := r.app.Service.CreateWorkflow(ctx, service.CreateWorkflowInput{ProjectID: r.spec.ProjectID, Kind: "software-development", Risk: "low", DataClassification: "restricted", Request: "Isolated local agent-ready pilot", IdempotencyKey: r.spec.RunKey})
		if err != nil {
			return out, err
		}
		out.WorkflowID = run.ID
	}
	if out.TaskAID == "" {
		task, _, err := r.app.Service.BeginWorkflowTask(ctx, service.BeginWorkflowTaskInput{WorkflowID: out.WorkflowID, TaskKey: r.spec.RunKey + ":a", Title: "Pilot A", TaskType: "maintenance", RAGQuery: r.spec.TaskA.Goal, IdempotencyKey: r.spec.RunKey + ":a"})
		if err != nil {
			return out, err
		}
		out.TaskAID = task.ID
		if task.State != domain.TaskStateLocalExecution {
			return out, fmt.Errorf("existing Task A is %s; resume using workflow_id and task_a_id", task.State)
		}
		task, item, validation, err := r.develop(ctx, task, r.spec.TaskA, "")
		if err != nil {
			return out, err
		}
		out.KnowledgeID = item.ID
		out.Version = item.Version
		out.ValidationID = validation.ID
		if _, err = r.transition(ctx, task, "VALIDATION_PASSED", validation.ID); err != nil {
			return out, err
		}
		out.Status = "awaiting_human_approval"
		out.Next = "A real product owner must inspect the exact evidence, approve this version via knowledge_candidate_decide, then record LEARNING_PROMOTED. Resume this same spec with the returned workflow_id and task_a_id. Keep model-created knowledge pending until then."
		return out, nil
	}
	a, err := r.app.Service.GetWorkflowTask(ctx, out.TaskAID)
	if err != nil {
		return out, err
	}
	if a.WorkflowID != out.WorkflowID || a.ProjectID != r.spec.ProjectID {
		return out, domain.ErrVersionConflict
	}
	out.KnowledgeID = a.CandidateID
	if a.State == domain.TaskStateValidationRequired && r.spec.RepairTaskA != nil {
		validation, err := r.repairTaskAPatch(ctx, a)
		if err != nil {
			return out, err
		}
		if a, err = r.transition(ctx, a, "VALIDATION_PASSED", validation.ID); err != nil {
			return out, err
		}
		out.Version, out.ValidationID = validation.CandidateVersion, validation.ID
	}
	if a.State == domain.TaskStatePromotionRequired {
		out.Status = "awaiting_human_approval"
		out.Next = "Human approval and LEARNING_PROMOTED are required; the runner cannot perform them."
		return out, nil
	}
	if a.State == domain.TaskStateRAGReadbackRequired {
		a, err = r.transition(ctx, a, "RAG_READBACK_VERIFIED", "")
		if err != nil {
			return out, fmt.Errorf("awaiting verified worker indexing: %w", err)
		}
	}
	if a.State != domain.TaskStateCompleted {
		return out, fmt.Errorf("Task A is %s; inspect workflow_trace_get before recovery", a.State)
	}
	lesson, err := r.app.Repository.GetKnowledge(ctx, a.CandidateID, false)
	if err != nil {
		return out, err
	}
	b, _, err := r.app.Service.BeginWorkflowTask(ctx, service.BeginWorkflowTaskInput{WorkflowID: out.WorkflowID, TaskKey: r.spec.RunKey + ":b", Title: "Pilot B reuse", TaskType: "maintenance", RAGQuery: r.spec.TaskB.Goal, IdempotencyKey: r.spec.RunKey + ":b"})
	if err != nil {
		return out, err
	}
	out.TaskBID = b.ID
	if b.State == domain.TaskStateLocalExecution {
		found := false
		for _, id := range b.RAGHitIDs {
			if id == lesson.ID {
				found = true
			}
		}
		if b.RAGBackend != "milvus" || !found {
			return out, errors.New("Task B must actually retrieve Task A through Milvus")
		}
		if err = r.app.Service.RecordTaskContext(ctx, service.RecordTaskContextInput{TaskID: b.ID, ExpectedVersion: b.Version, Contexts: []domain.UsedContext{{KnowledgeID: lesson.ID, Version: lesson.Version, TargetRepository: r.spec.TaskB.Workspace, TargetBranch: r.spec.Branch, TargetRevision: r.spec.TaskB.BaseRevision}}, IdempotencyKey: r.spec.RunKey + ":use"}); err != nil {
			return out, err
		}
		task, _, validation, err := r.develop(ctx, b, r.spec.TaskB, lesson.RetrievalText())
		if err != nil {
			return out, err
		}
		b, err = r.transition(ctx, task, "VALIDATED_REUSE_COMPLETED", validation.ID)
		if err != nil {
			return out, err
		}
	}
	if b.State != domain.TaskStateCompleted {
		return out, fmt.Errorf("Task B is %s; inspect durable evidence and resume checkpoints explicitly", b.State)
	}
	evidence, err := r.app.Service.WorkflowTrace(ctx, out.WorkflowID)
	if err != nil {
		return out, err
	}
	out.Trace = &evidence
	if !evidence.Complete {
		out.Status = "incomplete_evidence"
		return out, nil
	}
	out.Status = "completed"
	out.Next = "Task B's newly generated lesson remains pending. Query approved semantic metrics separately; this report does not approve definitions."
	return out, nil
}

func (r *runner) transition(ctx context.Context, task domain.WorkflowTaskCheckpoint, event, validation string) (domain.WorkflowTaskCheckpoint, error) {
	v, _, err := r.app.Service.TransitionWorkflowTask(ctx, service.TransitionWorkflowTaskInput{TaskID: task.ID, ExpectedVersion: task.Version, EventType: event, IdempotencyKey: task.ID + ":" + event, Provider: "ollama", Model: r.spec.Model, Evidence: "Local pilot checkpoint; exact command receipts are linked by validation_id", Payload: map[string]any{"validation_id": validation}})
	return v, err
}
func (r *runner) develop(ctx context.Context, task domain.WorkflowTaskCheckpoint, packet workpacket.Packet, knowledge string) (domain.WorkflowTaskCheckpoint, domain.KnowledgeItem, domain.KnowledgeValidation, error) {
	var item domain.KnowledgeItem
	var validation domain.KnowledgeValidation
	ctx = domain.WithOperationScope(ctx, domain.OperationScope{ProjectID: task.ProjectID, WorkflowID: task.WorkflowID, TaskID: task.ID})
	prompt := fmt.Sprintf("Create a minimal patch for an isolated synthetic repository. Goal: %s\nAllowed files: %v\nApproved guidance: %s\nReturn JSON only with string fields patch (a git unified diff), summary, and lesson (generalized reusable guidance without raw patch content). Do not include secrets, unrelated files, or claims that tests ran. The lesson is only a pending proposal until locally verified and explicitly approved by a human.", packet.Goal, packet.AllowedFiles, knowledge)
	raw, err := r.generate(ctx, prompt)
	if err != nil {
		return task, item, validation, err
	}
	var generation struct {
		Patch   string `json:"patch"`
		Summary string `json:"summary"`
		Lesson  string `json:"lesson"`
	}
	if err = json.Unmarshal(raw, &generation); err != nil {
		return task, item, validation, errors.New("local model did not return the required patch JSON; exact output retained in CAS")
	}
	if generation.Patch == "" || generation.Summary == "" || generation.Lesson == "" {
		return task, item, validation, errors.New("local patch, summary and generalized lesson required")
	}
	item, err = r.app.Service.Capture(ctx, service.CaptureInput{ProjectID: task.ProjectID, WorkflowID: task.WorkflowID, Prompt: prompt, Response: generation.Lesson, Summary: generation.Summary, TaskType: "maintenance", Provider: "ollama", Model: r.spec.Model, RepositoryRevision: packet.BaseRevision, Procedure: []string{"Apply only the scoped work-packet patch", "Execute every local check", "Review exact evidence before publication"}})
	if err != nil {
		return task, item, validation, err
	}
	task, _, err = r.app.Service.TransitionWorkflowTask(ctx, service.TransitionWorkflowTaskInput{TaskID: task.ID, ExpectedVersion: task.Version, EventType: "LOCAL_RESULT_RECORDED", IdempotencyKey: task.ID + ":generated", Provider: "ollama", Model: r.spec.Model, CandidateID: item.ID, Evidence: "Exact local model output and disclosed-context manifest retained in CAS"})
	if err != nil {
		return task, item, validation, err
	}
	validation, err = r.app.Service.VerifyKnowledgePatch(ctx, service.ValidationInput{KnowledgeID: item.ID, ExpectedVersion: item.Version, SourceManifest: domain.SourceManifest{SchemaVersion: "hybrid-ai/knowledge-source/v1", Sources: []domain.KnowledgeSource{{Kind: "repository", Reference: packet.Workspace, Branch: r.spec.Branch, Revision: packet.BaseRevision}}}, Criteria: []domain.ValidationCriterion{{Name: "bounded local verification", Passed: true, Observation: "Verifier executes packet scope and commands; this criterion alone cannot produce a passing report"}}}, packet, []byte(generation.Patch))
	return task, item, validation, err
}

func (r *runner) generate(ctx context.Context, prompt string) (output []byte, err error) {
	ctx = telemetry.WithModel(ctx, "ollama", r.spec.Model)
	ctx, finish, err := r.app.Service.BeginOperation(ctx, "model.generate", domain.ScopeFromContext(ctx))
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, finish(err)) }()
	format := map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []string{"patch", "summary", "lesson"},
		"properties": map[string]any{
			"patch": map[string]string{"type": "string"}, "summary": map[string]string{"type": "string"},
			"lesson": map[string]string{"type": "string"},
		},
	}
	options := map[string]any{"temperature": 0.2, "num_predict": 2048}
	manifest, _ := json.Marshal(map[string]any{"schema": "hybrid-ai/disclosed-context/v1", "provider": "ollama", "model": r.spec.Model, "scope": "synthetic goal and file allowlist, or explicitly bound synthetic patch-repair evidence; only Task B receives approved generalized guidance; no unrestricted repository content", "prompt_sha256": domain.Digest([]byte(prompt)), "think": false, "format": format, "options": options})
	for _, data := range [][]byte{[]byte(prompt), manifest} {
		artifact, err := r.store.Put(ctx, data, "text/plain")
		if err != nil {
			return nil, err
		}
		if err = r.app.Service.RecordOperationEvidence(ctx, "model.generate.input", artifact); err != nil {
			return nil, err
		}
	}
	// Request the final structured answer explicitly. Thinking-capable models
	// can otherwise finish with an empty response and separate thinking text.
	// That text is evidence, never a substitute for the required patch answer.
	payload, _ := json.Marshal(map[string]any{"model": r.spec.Model, "prompt": prompt, "stream": false, "format": format, "think": false, "options": options})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.cfg.OllamaURL, "/")+"/api/generate", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("local Ollama generation unavailable")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 8<<20 {
		return nil, errors.New("local model output exceeds evidence limit")
	}
	artifact, err := r.store.Put(ctx, body, "application/json")
	if err != nil {
		return nil, err
	}
	if err = r.app.Service.RecordOperationEvidence(ctx, "model.generate.output", artifact); err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("local Ollama generation failed; exact response retained in CAS")
	}
	var result struct {
		Response string `json:"response"`
		Done     bool   `json:"done"`
		Model    string `json:"model"`
	}
	if err = json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if !result.Done || result.Response == "" || result.Model != r.spec.Model {
		return nil, errors.New("incomplete or unexpected local model response")
	}
	return []byte(result.Response), nil
}
func localEndpoint(ctx context.Context, endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("pilot Ollama endpoint must be local HTTP without credentials")
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil || len(ips) == 0 {
		return errors.New("local Ollama address unavailable")
	}
	for _, ip := range ips {
		if !ip.IP.IsLoopback() && !ip.IP.IsPrivate() {
			return errors.New("cloud/public model endpoints are forbidden in the pilot")
		}
	}
	return nil
}
