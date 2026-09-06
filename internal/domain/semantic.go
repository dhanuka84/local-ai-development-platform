package domain

import (
	"context"
	"encoding/json"
	"time"
)

type ContextDefinition struct {
	ID             string   `json:"id"`
	Version        int      `json:"version"`
	Kind           string   `json:"kind"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Owner          string   `json:"owner"`
	Unit           string   `json:"unit,omitempty"`
	Formula        string   `json:"formula,omitempty"`
	Timestamp      string   `json:"timestamp,omitempty"`
	Dimensions     []string `json:"dimensions"`
	Roles          []string `json:"roles"`
	Preconditions  []string `json:"preconditions"`
	InputContract  string   `json:"input_contract,omitempty"`
	OutputContract string   `json:"output_contract,omitempty"`
	Idempotency    string   `json:"idempotency,omitempty"`
	Reversibility  string   `json:"reversibility,omitempty"`
	Compensation   string   `json:"compensation,omitempty"`
}
type GovernedDefinition struct {
	ContextDefinition
	ProjectID            string     `json:"project_id"`
	SHA256               string     `json:"sha256"`
	RegistrySHA256       string     `json:"registry_sha256"`
	Status               string     `json:"status"`
	ValidatedAt          time.Time  `json:"validated_at"`
	ApprovedBy           string     `json:"approved_by,omitempty"`
	ApprovedAt           *time.Time `json:"approved_at,omitempty"`
	ProjectionVerifiedAt *time.Time `json:"projection_verified_at,omitempty"`
	ProjectionModel      string     `json:"projection_model,omitempty"`
	ProjectionDimension  int        `json:"projection_dimension,omitempty"`
}

func (d GovernedDefinition) VectorID() string { return Digest([]byte(d.ProjectID + ":" + d.ID)) }
func (d GovernedDefinition) ProjectionDigest() string {
	raw, _ := json.Marshal([]any{d.ProjectID, d.ID, d.Version, d.SHA256, d.RegistrySHA256, "ollama", d.ProjectionModel, d.ProjectionDimension})
	return Digest(raw)
}

type RegistryValidation struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	RegistrySHA256 string    `json:"registry_sha256"`
	Actor          string    `json:"actor"`
	Evidence       Artifact  `json:"evidence"`
	CompletedAt    time.Time `json:"completed_at"`
}
type DefinitionDecision struct {
	ProjectID       string `json:"project_id"`
	DefinitionID    string `json:"definition_id"`
	ExpectedVersion int    `json:"expected_version"`
	ExpectedSHA256  string `json:"expected_sha256"`
	ValidationID    string `json:"validation_id"`
	Decision        string `json:"decision"`
	Reason          string `json:"reason"`
	IdempotencyKey  string `json:"idempotency_key"`
	Actor           string `json:"-"`
}
type MetricRequest struct {
	ProjectID  string            `json:"project_id"`
	MetricID   string            `json:"metric_id"`
	Version    int               `json:"version"`
	Start      time.Time         `json:"start"`
	End        time.Time         `json:"end"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
}
type MetricResult struct {
	MetricID         string    `json:"metric_id"`
	Version          int       `json:"version"`
	ProjectID        string    `json:"project_id"`
	Value            *float64  `json:"value"`
	Unit             string    `json:"unit"`
	Numerator        int64     `json:"numerator"`
	Denominator      int64     `json:"denominator"`
	Failures         int64     `json:"failures"`
	Backlog          int64     `json:"backlog"`
	Start            time.Time `json:"start"`
	End              time.Time `json:"end"`
	QueriedAt        time.Time `json:"queried_at"`
	DefinitionSHA256 string    `json:"definition_sha256"`
	RegistrySHA256   string    `json:"registry_sha256"`
	Freshness        string    `json:"freshness"`
	Coverage         string    `json:"coverage"`
	Explanation      string    `json:"explanation,omitempty"`
	TraceID          string    `json:"trace_id,omitempty"`
}
type SemanticRepository interface {
	ValidateContextRegistry(context.Context, RegistryValidation, []ContextDefinition) error
	DecideContextDefinition(context.Context, DefinitionDecision) (GovernedDefinition, error)
	ContextDefinition(context.Context, string, string, int) (GovernedDefinition, error)
	ApprovedContextDefinitions(context.Context, string) ([]GovernedDefinition, error)
	RecordDefinitionProjection(context.Context, GovernedDefinition) error
	DefinitionByDecision(context.Context, string) (GovernedDefinition, error)
	ClaimDefinitionRefresh(context.Context, int) ([]GovernedDefinition, error)
	QueryPlatformMetric(context.Context, MetricRequest, string) (MetricResult, error)
	RecordTaskContext(context.Context, string, int, []UsedContext, string, string) error
	TaskUsedContexts(context.Context, string) ([]UsedContext, error)
}
type SemanticVectorStore interface {
	UpsertDefinition(context.Context, GovernedDefinition, []float32) error
	SearchDefinitions(context.Context, string, []float32, int) ([]VectorHit, error)
	VerifyDefinitionProjection(context.Context, GovernedDefinition) error
}
