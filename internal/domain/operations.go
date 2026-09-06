package domain

import (
	"context"
	"time"
)

type FailedIndex struct {
	EventID     int64     `json:"event_id"`
	KnowledgeID string    `json:"knowledge_id"`
	Attempts    int       `json:"attempts"`
	FailedAt    time.Time `json:"failed_at"`
	Recovery    string    `json:"recovery"`
}
type RetryIndexInput struct {
	ProjectID        string `json:"project_id"`
	EventID          int64  `json:"event_id"`
	ExpectedAttempts int    `json:"expected_attempts"`
	Reason           string `json:"reason"`
	IdempotencyKey   string `json:"idempotency_key"`
	Actor            string `json:"-"`
}
type EvidenceHealth struct {
	ProjectID          string `json:"project_id"`
	UnknownOutcomes    int64  `json:"unknown_outcomes"`
	FailedExports      int64  `json:"failed_exports"`
	Unexported         int64  `json:"unexported"`
	RetentionReviewDue int64  `json:"retention_review_due"`
	RetentionDays      int    `json:"retention_days"`
	Policy             string `json:"policy"`
}
type OperationsRepository interface {
	FailedKnowledgeIndexes(context.Context, string) ([]FailedIndex, error)
	RetryKnowledgeIndex(context.Context, RetryIndexInput) (int64, error)
	EvidenceHealth(context.Context, string, int) (EvidenceHealth, error)
}
