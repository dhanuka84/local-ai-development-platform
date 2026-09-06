package domain

import (
	"context"
	"encoding/json"
)

const DataProductKnowledge = "software_knowledge"
const PurposeGuidance = "development_guidance"
const PurposeCodeChange = "code_change"
const PurposeReview = "review"

type purposeKey struct{}

func WithPurpose(ctx context.Context, purpose string) context.Context {
	switch purpose {
	case PurposeCodeChange, PurposeReview:
	default:
		purpose = PurposeGuidance
	}
	return context.WithValue(ctx, purposeKey{}, purpose)
}
func Purpose(ctx context.Context) string {
	purpose, _ := ctx.Value(purposeKey{}).(string)
	if purpose == "" {
		return PurposeGuidance
	}
	return purpose
}
func TaskPurpose(taskType string) string {
	switch taskType {
	case "implementation", "testing", "bugfix", "refactoring", "code-change":
		return PurposeCodeChange
	case "review":
		return PurposeReview
	default:
		return PurposeGuidance
	}
}

type ProjectionManifest struct {
	SchemaVersion        string `json:"schema_version"`
	KnowledgeID          string `json:"knowledge_id"`
	Version              int    `json:"version"`
	ContentSHA256        string `json:"content_sha256"`
	SourceManifestSHA256 string `json:"source_manifest_sha256"`
	Provider             string `json:"provider"`
	Model                string `json:"model"`
	Dimension            int    `json:"dimension"`
	Coverage             int    `json:"coverage"`
	VerificationID       string `json:"verification_id"`
}

func (m ProjectionManifest) Digest() string { data, _ := json.Marshal(m); return Digest(data) }
func (m ProjectionManifest) Check() error {
	if m.SchemaVersion != "hybrid-ai/knowledge-projection/v1" || m.KnowledgeID == "" || m.Version < 1 || !digestPattern.MatchString(m.ContentSHA256) || !digestPattern.MatchString(m.SourceManifestSHA256) || m.Provider != "ollama" || m.Model == "" || m.Dimension < 1 || m.Coverage != 1 || m.VerificationID == "" {
		return ErrQualityBlocked
	}
	return nil
}

type ProjectionRepository interface {
	BuildKnowledgeProjection(context.Context, KnowledgeItem, string, string, int) (ProjectionManifest, error)
	KnowledgeProjection(context.Context, string) (ProjectionManifest, error)
}

type UsedContext struct {
	KnowledgeID      string `json:"knowledge_id"`
	Version          int    `json:"version"`
	TargetRepository string `json:"target_repository,omitempty"`
	TargetBranch     string `json:"target_branch,omitempty"`
	TargetRevision   string `json:"target_revision,omitempty"`
}
