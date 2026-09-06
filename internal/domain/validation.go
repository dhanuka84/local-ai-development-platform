package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const ValidationSchema = "hybrid-ai/knowledge-validation/v1"

var (
	ErrValidationRequired  = errors.New("validation_required")
	ErrVersionConflict     = errors.New("version_conflict")
	ErrQualityBlocked      = errors.New("quality_blocked")
	ErrEvidenceUnavailable = errors.New("evidence_unavailable")
	ErrForbidden           = errors.New("forbidden")
	digestPattern          = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func Digest(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

type KnowledgeSource struct {
	Kind              string `json:"kind"`
	Reference         string `json:"reference"`
	Revision          string `json:"revision,omitempty"`
	Branch            string `json:"branch,omitempty"`
	ApplicableThrough string `json:"applicable_through_revision,omitempty"`
	ArtifactSHA256    string `json:"artifact_sha256,omitempty"`
}

type SourceManifest struct {
	SchemaVersion string            `json:"schema_version"`
	Sources       []KnowledgeSource `json:"sources"`
}

func (m SourceManifest) Validate() error {
	if m.SchemaVersion != "hybrid-ai/knowledge-source/v1" || len(m.Sources) == 0 || len(m.Sources) > 100 {
		return fmt.Errorf("%w: source manifest version and 1..100 sources required", ErrValidationRequired)
	}
	for _, source := range m.Sources {
		if strings.TrimSpace(source.Reference) == "" {
			return fmt.Errorf("%w: source reference required", ErrValidationRequired)
		}
		switch source.Kind {
		case "repository":
			if source.ApplicableThrough != "" && !regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`).MatchString(source.ApplicableThrough) {
				return ErrValidationRequired
			}
			if !regexp.MustCompile(`^[a-f0-9]{40}([a-f0-9]{24})?$`).MatchString(source.Revision) || strings.TrimSpace(source.Branch) == "" {
				return fmt.Errorf("%w: repository source requires full revision and branch", ErrValidationRequired)
			}
		case "document", "procedure", "patch", "work_packet":
			if !digestPattern.MatchString(source.ArtifactSHA256) {
				return fmt.Errorf("%w: source artifact digest required", ErrValidationRequired)
			}
		default:
			return fmt.Errorf("%w: unsupported source kind", ErrValidationRequired)
		}
	}
	return nil
}

type ValidationCriterion struct {
	Name        string `json:"name"`
	Passed      bool   `json:"passed"`
	Observation string `json:"observation"`
}

type ValidationCommand struct {
	Argv         []string `json:"argv"`
	ExitCode     int      `json:"exit_code"`
	OutputSHA256 string   `json:"output_sha256"`
}

// Identity, receipt times and the verdict are assigned by the authenticated
// verifier/QA service boundary, never taken from model-authored reports.
type KnowledgeValidation struct {
	ID                   string                `json:"id"`
	SchemaVersion        string                `json:"schema_version"`
	KnowledgeID          string                `json:"knowledge_id"`
	ProjectID            string                `json:"project_id"`
	CandidateVersion     int                   `json:"candidate_version"`
	ContentSHA256        string                `json:"content_sha256"`
	SourceManifest       SourceManifest        `json:"source_manifest"`
	SourceManifestSHA256 string                `json:"source_manifest_sha256"`
	Method               string                `json:"method"`
	Criteria             []ValidationCriterion `json:"criteria"`
	Commands             []ValidationCommand   `json:"commands"`
	Verdict              string                `json:"verdict"`
	ValidatedBy          string                `json:"validated_by"`
	StartedAt            time.Time             `json:"started_at"`
	CompletedAt          time.Time             `json:"completed_at"`
	ValidUntil           time.Time             `json:"valid_until"`
	ReportArtifact       Artifact              `json:"report_artifact"`
	SourceArtifact       Artifact              `json:"source_artifact"`
}

func (v KnowledgeValidation) Check() error {
	if v.SchemaVersion != ValidationSchema || v.KnowledgeID == "" || v.ProjectID == "" || v.CandidateVersion < 1 ||
		!digestPattern.MatchString(v.ContentSHA256) || v.ValidatedBy == "" || len(v.Criteria) == 0 || len(v.Criteria) > 100 {
		return fmt.Errorf("%w: incomplete validation", ErrValidationRequired)
	}
	if err := v.SourceManifest.Validate(); err != nil {
		return err
	}
	manifest, err := json.Marshal(v.SourceManifest)
	if err != nil {
		return err
	}
	if Digest(manifest) != v.SourceManifestSHA256 {
		return fmt.Errorf("%w: source digest mismatch", ErrValidationRequired)
	}
	if v.Method != "manual" && v.Method != "workpacket" {
		return fmt.Errorf("%w: unsupported validation method", ErrValidationRequired)
	}
	if v.StartedAt.IsZero() || v.CompletedAt.Before(v.StartedAt) || !v.ValidUntil.After(v.CompletedAt) || v.ValidUntil.Sub(v.CompletedAt) > 30*24*time.Hour {
		return fmt.Errorf("%w: invalid validation time range", ErrValidationRequired)
	}
	passed := true
	for _, criterion := range v.Criteria {
		if strings.TrimSpace(criterion.Name) == "" || strings.TrimSpace(criterion.Observation) == "" {
			return fmt.Errorf("%w: criterion and observation required", ErrValidationRequired)
		}
		passed = passed && criterion.Passed
	}
	if v.Method == "manual" && len(v.Commands) != 0 {
		return fmt.Errorf("%w: manual attestations cannot claim command execution", ErrValidationRequired)
	}
	if v.Method == "workpacket" && len(v.Commands) == 0 {
		return fmt.Errorf("%w: executed checks required", ErrValidationRequired)
	}
	for _, command := range v.Commands {
		if len(command.Argv) == 0 || !digestPattern.MatchString(command.OutputSHA256) {
			return fmt.Errorf("%w: command evidence required", ErrValidationRequired)
		}
		passed = passed && command.ExitCode == 0
	}
	expected := "fail"
	if passed {
		expected = "pass"
	}
	if v.Verdict != expected {
		return fmt.Errorf("%w: verdict disagrees with evidence", ErrValidationRequired)
	}
	return nil
}

type KnowledgeDecision struct {
	ID              string                `json:"id"`
	KnowledgeID     string                `json:"knowledge_id"`
	ExpectedVersion int                   `json:"expected_version"`
	ValidationID    string                `json:"validation_id,omitempty"`
	Decision        string                `json:"decision"`
	Reason          string                `json:"reason"`
	IdempotencyKey  string                `json:"idempotency_key"`
	Actor           string                `json:"actor"`
	RequestSHA256   string                `json:"request_sha256"`
	Authorization   AuthorizationDecision `json:"authorization"`
	CreatedAt       time.Time             `json:"created_at"`
}

// Kept separate from the graph/storage interface so unrelated consumers do not
// acquire publication authority merely by implementing retrieval.
type KnowledgeGovernanceRepository interface {
	RecordKnowledgeValidation(context.Context, KnowledgeValidation) (KnowledgeValidation, error)
	GetKnowledgeValidation(context.Context, string) (KnowledgeValidation, error)
	DecideKnowledge(context.Context, KnowledgeDecision) (KnowledgeItem, error)
}
