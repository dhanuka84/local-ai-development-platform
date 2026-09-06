package domain

import (
	"context"
	"fmt"
	"math"
	"time"
)

// Approval is historical. Eligibility is evaluated against current evidence and
// policy; refreshing a projection must never refresh source or content clocks.
type KnowledgeQuality struct {
	KnowledgeID          string     `json:"knowledge_id"`
	ProjectID            string     `json:"project_id"`
	Version              int        `json:"version"`
	ValidationID         string     `json:"validation_id,omitempty"`
	PolicyVersion        int        `json:"policy_version"`
	PolicyOwner          string     `json:"policy_owner"`
	Eligible             bool       `json:"eligible"`
	Reason               string     `json:"reason"`
	SourceVerifiedAt     *time.Time `json:"source_verified_at,omitempty"`
	ContentValidatedAt   *time.Time `json:"content_validated_at,omitempty"`
	ProjectionVerifiedAt *time.Time `json:"projection_verified_at,omitempty"`
}

type KnowledgeQualityRepository interface {
	KnowledgeQuality(context.Context, string) (KnowledgeQuality, error)
	RecordSourceCheck(context.Context, string, int, string, bool, string) error
	RecordProjectionCheck(context.Context, KnowledgeItem) error
	ListQualityRefreshIDs(context.Context, int) ([]string, error)
	ListQualityReviews(context.Context, string, int) ([]KnowledgeQuality, error)
}

func ValidateEmbedding(vector []float32, dimension int) error {
	if dimension < 1 || len(vector) != dimension {
		return fmt.Errorf("%w: embedding dimension mismatch", ErrQualityBlocked)
	}
	norm := float64(0)
	for _, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("%w: non-finite embedding", ErrQualityBlocked)
		}
		norm += float64(value) * float64(value)
	}
	if norm == 0 {
		return fmt.Errorf("%w: zero embedding", ErrQualityBlocked)
	}
	return nil
}

func FiniteScore(score float32) bool {
	return !math.IsNaN(float64(score)) && !math.IsInf(float64(score), 0)
}

// Vector metadata is untrusted. Missing metadata (legacy rows) fails closed.
func (hit VectorHit) Matches(item KnowledgeItem, projectID string) bool {
	return item.Version > 0 && item.ProjectID == projectID && hit.ID == item.ID &&
		item.Status == CandidateApproved && hit.Version == item.Version &&
		hit.ContentSHA256 == Digest([]byte(item.RetrievalText())) && FiniteScore(hit.Score)
}
