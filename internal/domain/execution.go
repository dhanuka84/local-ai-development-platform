package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"
)

const ExecutionSchema = "hybrid-ai/sdlc-execution/v1"
const AgentPackageSchema = "hybrid-ai/agent-package/v1"

var evaluatorImagePattern = regexp.MustCompile(`^(sha256:[a-f0-9]{64}|[a-zA-Z0-9./:_-]+@sha256:[a-f0-9]{64})$`)

var ErrExecutionBlocked = errors.New("execution is blocked")
var ErrLeaseLost = errors.New("execution lease is absent, expired or superseded")
var ErrBudgetExhausted = errors.New("execution budget exhausted")
var ErrResourceBusy = errors.New("local execution resource is busy")

func ExecutionRole(stage string) string {
	switch stage {
	case "build":
		return "sdlc_builder"
	case "evaluate", "verify_delivery", "verify_recovery":
		return "sdlc_evaluator"
	case "deliver":
		return "sdlc_delivery"
	case "diagnose":
		return "sdlc_diagnosis"
	case "remediate":
		return "sdlc_remediation"
	}
	return ""
}

type ExecutionBudget struct {
	MaxAttempts       int `json:"max_attempts"`
	MaxModelCalls     int `json:"max_model_calls"`
	MaxTokens         int `json:"max_tokens"`
	MaxSourceQueries  int `json:"max_source_queries"`
	MaxSourceRows     int `json:"max_source_rows"`
	MaxSeconds        int `json:"max_seconds"`
	MaxConcurrentRuns int `json:"max_concurrent_runs"`
}

func (b ExecutionBudget) Validate() error {
	if b.MaxAttempts < 1 || b.MaxAttempts > 10 || b.MaxModelCalls < 1 || b.MaxModelCalls > 30 || b.MaxTokens < 128 || b.MaxTokens > 1000000 || b.MaxSourceQueries < 0 || b.MaxSourceQueries > 100 || b.MaxSourceRows < 0 || b.MaxSourceRows > 100000 || b.MaxSeconds < 30 || b.MaxSeconds > 86400 || b.MaxConcurrentRuns < 1 || b.MaxConcurrentRuns > 16 {
		return errors.New("execution requires bounded attempts, tokens, queries, duration and concurrency")
	}
	return nil
}

// Packages and targets are operator-owned configuration. A model cannot supply
// its own identity, tools, evaluator image, limits, or publication authority.
type AgentPackage struct {
	Schema          string   `json:"schema"`
	ID              string   `json:"id"`
	Version         int      `json:"version"`
	Role            string   `json:"role"`
	Model           string   `json:"model,omitempty"`
	ModelSHA256     string   `json:"model_sha256,omitempty"`
	Instructions    string   `json:"instructions"`
	Tools           []string `json:"tools"`
	MaxInputBytes   int      `json:"max_input_bytes"`
	MaxOutputTokens int      `json:"max_output_tokens"`
	TimeoutSeconds  int      `json:"timeout_seconds"`
	Concurrency     int      `json:"concurrency"`
	EvaluatorImage  string   `json:"evaluator_image,omitempty"`
	RegressionID    string   `json:"regression_id"`
}

func (p AgentPackage) Digest() string { raw, _ := json.Marshal(p); return Digest(raw) }

func (p AgentPackage) MaxTokenCharge() int {
	return p.MaxInputBytes + len(p.Instructions) + 1024 + p.MaxOutputTokens
}

func (p AgentPackage) Validate() error {
	if p.Schema != AgentPackageSchema || !ValidProductKey(p.ID) || p.Version < 1 || !boundedText(p.Instructions, 16384) || !ValidProductKey(p.RegressionID) || p.MaxInputBytes < 1024 || p.MaxInputBytes > 256*1024 || p.TimeoutSeconds < 1 || p.TimeoutSeconds > 600 || p.Concurrency < 1 || p.Concurrency > 16 || len(p.Tools) > 10 {
		return errors.New("invalid agent package identity, instructions or limits")
	}
	allowed := map[string][]string{
		"sdlc_builder":     {"context", "propose_patch"},
		"sdlc_evaluator":   {"context", "verify_patch", "verify_delivery", "verify_recovery", "source_query", "compare_observations"},
		"sdlc_delivery":    {"context", "deliver", "reconcile"},
		"sdlc_diagnosis":   {"context", "source_query", "compare_observations", "propose_diagnosis"},
		"sdlc_remediation": {"context", "remediate", "reconcile"},
	}
	tools, ok := allowed[p.Role]
	if !ok {
		return errors.New("unknown package role")
	}
	for _, tool := range p.Tools {
		if !slices.Contains(tools, tool) {
			return fmt.Errorf("tool %q is outside package role", tool)
		}
	}
	if p.Role == "sdlc_builder" || p.Role == "sdlc_diagnosis" {
		if !boundedText(p.Model, 128) || !digestPattern.MatchString(p.ModelSHA256) || p.MaxOutputTokens < 128 || p.MaxOutputTokens > 32768 {
			return errors.New("model packages require a pinned local model and output budget")
		}
	} else if p.Model != "" || p.ModelSHA256 != "" {
		return errors.New("deterministic executor packages do not use a model")
	}
	if p.Role == "sdlc_evaluator" && !evaluatorImagePattern.MatchString(p.EvaluatorImage) {
		return errors.New("evaluator image must be pinned by digest")
	}
	return nil
}

type ExecutionTarget struct {
	ID                 string            `json:"id"`
	ProjectID          string            `json:"project_id"`
	ProductID          string            `json:"product_id"`
	Environment        string            `json:"environment"`
	RepositoryID       string            `json:"repository_id"`
	Participants       map[string]string `json:"participants"`
	Packages           map[string]string `json:"packages"`
	Sources            []string          `json:"sources"`
	Purposes           []string          `json:"purposes"`
	Classifications    []string          `json:"classifications"`
	Budget             ExecutionBudget   `json:"budget"`
	ProtectedPaths     []string          `json:"protected_paths"`
	AllowedRemedies    []string          `json:"allowed_remedies"`
	DeliverySHA256     string            `json:"delivery_sha256,omitempty"`
	RemediationSHA256  string            `json:"remediation_sha256,omitempty"`
	ObservationSeconds int               `json:"observation_seconds,omitempty"`
	Qualification      bool              `json:"qualification"`
}

func (t ExecutionTarget) Digest() string { raw, _ := json.Marshal(t); return Digest(raw) }

type ExecutionUsage struct {
	Attempts      int `json:"attempts"`
	ModelCalls    int `json:"model_calls"`
	TokensCharged int `json:"tokens_charged"`
	TokensActual  int `json:"tokens_actual"`
	SourceQueries int `json:"source_queries"`
	SourceRows    int `json:"source_rows"`
}

type ExecutionRun struct {
	Schema             string                  `json:"schema"`
	ID                 string                  `json:"id"`
	ProjectID          string                  `json:"project_id"`
	ProductID          string                  `json:"product_id"`
	Kind               string                  `json:"kind"`
	RepositoryRevision string                  `json:"repository_revision"`
	Intent             ProductBinding          `json:"intent"`
	Bindings           []ProductBinding        `json:"bindings"`
	Owner              string                  `json:"owner"`
	OwnerCredential    string                  `json:"-"`
	Target             ExecutionTarget         `json:"target"`
	Packages           map[string]AgentPackage `json:"packages"`
	Status             string                  `json:"status"`
	Stage              string                  `json:"stage"`
	Version            int                     `json:"version"`
	StepNumber         int                     `json:"step_number"`
	Usage              ExecutionUsage          `json:"usage"`
	Blockers           []string                `json:"blockers"`
	CreatedAt          time.Time               `json:"created_at"`
	Deadline           time.Time               `json:"deadline"`
	UpdatedAt          time.Time               `json:"updated_at"`
	IdempotencyKey     string                  `json:"idempotency_key"`
	RequestSHA256      string                  `json:"request_sha256"`
}

func (r ExecutionRun) Package() AgentPackage { return r.Packages[ExecutionRole(r.Stage)] }
func (r ExecutionRun) Actor() string         { return r.Target.Participants[ExecutionRole(r.Stage)] }

type ExecutionStep struct {
	ID          string           `json:"id"`
	RunID       string           `json:"run_id"`
	Number      int              `json:"number"`
	Stage       string           `json:"stage"`
	Actor       string           `json:"actor"`
	Role        string           `json:"role"`
	PackageSHA  string           `json:"package_sha256"`
	Fence       int              `json:"fence"`
	LeaseSHA    string           `json:"-"`
	LeaseUntil  time.Time        `json:"lease_until"`
	StartedAt   time.Time        `json:"started_at"`
	CompletedAt *time.Time       `json:"completed_at,omitempty"`
	ContextSHA  string           `json:"context_sha256"`
	Result      *ExecutionResult `json:"result,omitempty"`
	Evidence    Artifact         `json:"evidence"`
}

type ExecutionClaim struct {
	Run   ExecutionRun  `json:"run"`
	Step  ExecutionStep `json:"step"`
	Token string        `json:"lease_token"`
}

type ExecutionLease struct {
	ProjectID string `json:"project_id"`
	RunID     string `json:"run_id"`
	StepID    string `json:"step_id"`
	Fence     int    `json:"fence"`
	Token     string `json:"lease_token"`
}

type executionLeaseKey struct{}

func WithExecutionLease(ctx context.Context, lease ExecutionLease) context.Context {
	return context.WithValue(ctx, executionLeaseKey{}, lease)
}
func ExecutionLeaseFromContext(ctx context.Context) (ExecutionLease, bool) {
	l, ok := ctx.Value(executionLeaseKey{}).(ExecutionLease)
	return l, ok
}

type ExecutionPlanStep struct {
	Description string   `json:"description"`
	Criteria    []string `json:"criteria"`
}
type ModelEvidence struct {
	Provider       string   `json:"provider"`
	Model          string   `json:"model"`
	ModelSHA256    string   `json:"model_sha256"`
	Prompt         Artifact `json:"prompt"`
	Response       Artifact `json:"response"`
	Manifest       Artifact `json:"manifest"`
	InputTokens    int      `json:"input_tokens"`
	OutputTokens   int      `json:"output_tokens"`
	DurationMillis int64    `json:"duration_millis"`
	TrialKind      string   `json:"trial_kind"`
}
type ExecutionCheck struct {
	Name       string   `json:"name"`
	ArgvSHA256 string   `json:"argv_sha256"`
	ExitCode   int      `json:"exit_code"`
	TimedOut   bool     `json:"timed_out"`
	Output     Artifact `json:"output"`
}
type ExecutionEffect struct {
	Kind           string   `json:"kind"`
	Key            string   `json:"key"`
	ExternalID     string   `json:"external_id"`
	URL            string   `json:"url"`
	BeforeSHA256   string   `json:"before_sha256"`
	AfterSHA256    string   `json:"after_sha256"`
	ObservedSHA256 string   `json:"observed_sha256"`
	Status         string   `json:"status"`
	Evidence       Artifact `json:"evidence"`
}
type ExecutionHypothesis struct {
	ID             string   `json:"id"`
	Explanation    string   `json:"explanation"`
	Supports       []string `json:"supports"`
	Contradicts    []string `json:"contradicts"`
	Status         string   `json:"status"`
	ProposedRemedy string   `json:"proposed_remedy,omitempty"`
}

// A result is an attributed worker submission, not a model's authority. The
// service validates the typed evidence and the independent role before deriving
// the next state. No caller supplies the next state or marks a run complete.
type ExecutionResult struct {
	Stage          string                `json:"stage"`
	Outcome        string                `json:"outcome"`
	Summary        string                `json:"summary"`
	Criteria       []string              `json:"criteria"`
	Plan           []ExecutionPlanStep   `json:"plan,omitempty"`
	Patch          Artifact              `json:"patch"`
	BaseRevision   string                `json:"base_revision,omitempty"`
	PacketSHA256   string                `json:"packet_sha256,omitempty"`
	CandidateSHA   string                `json:"candidate_sha256,omitempty"`
	SandboxImage   string                `json:"sandbox_image,omitempty"`
	Checks         []ExecutionCheck      `json:"checks,omitempty"`
	Effects        []ExecutionEffect     `json:"effects,omitempty"`
	Hypotheses     []ExecutionHypothesis `json:"hypotheses,omitempty"`
	ObservationIDs []string              `json:"observation_ids,omitempty"`
	EvaluationIDs  []string              `json:"evaluation_ids,omitempty"`
	Model          *ModelEvidence        `json:"model,omitempty"`
	Artifacts      []Artifact            `json:"artifacts,omitempty"`
}

type ExecutionEvent struct {
	ID        string    `json:"id"`
	RunID     string    `json:"run_id"`
	StepID    string    `json:"step_id,omitempty"`
	Kind      string    `json:"kind"`
	Actor     string    `json:"actor"`
	Version   int       `json:"version"`
	Reason    string    `json:"reason"`
	Evidence  Artifact  `json:"evidence"`
	CreatedAt time.Time `json:"created_at"`
}

type ExecutionView struct {
	Run    ExecutionRun     `json:"run"`
	Steps  []ExecutionStep  `json:"steps"`
	Events []ExecutionEvent `json:"events"`
}

// All writes recheck live actor/owner credentials, product heads and leases
// inside the transaction. Separate repositories keep legacy fakes narrow.
type ExecutionRepository interface {
	CreateExecution(context.Context, ExecutionRun) (ExecutionRun, error)
	GetExecution(context.Context, string, string) (ExecutionView, error)
	ListExecutions(context.Context, string, string, int) ([]ExecutionRun, error)
	ClaimExecution(context.Context, string, string, string, string) (ExecutionClaim, error)
	CheckExecutionLease(context.Context, ExecutionLease, string) (ExecutionView, error)
	CompleteExecution(context.Context, ExecutionLease, string, ExecutionResult, Artifact, string, string) (ExecutionRun, error)
	ControlExecution(context.Context, string, string, string, int, string, string) (ExecutionRun, error)
	ReserveExecutionSource(context.Context, ExecutionLease, string, SourceQuery) error
	RecordExecutionArtifact(context.Context, ExecutionLease, string, Artifact, bool) error
	ExecutionArtifact(context.Context, string, string, string) (Artifact, error)
	CheckExecutionReader(context.Context, ExecutionRun, string) error
}
