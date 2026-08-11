package domain

import "time"

const (
	TaskStateQueued                 = "queued"
	TaskStateLocalExecution         = "local_execution"
	TaskStateReviewApprovalRequired = "review_approval_required"
	TaskStateCloudReviewRequired    = "cloud_review_required"
	TaskStateLocalRevisionRequired  = "local_revision_required"
	TaskStateValidationRequired     = "validation_required"
	TaskStatePromotionRequired      = "promotion_required"
	TaskStateRAGReadbackRequired    = "rag_readback_required"
	TaskStateCompleted              = "completed"
	TaskStateBlocked                = "blocked"
	TaskStateRejected               = "rejected"

	TaskRoutePending            = "pending"
	TaskRouteRAGHit             = "rag_hit"
	TaskRouteRAGMissCloudReview = "rag_miss_cloud_review"
	TaskRouteRAGMissLocalOnly   = "rag_miss_local_only"
)

type WorkflowTaskCheckpoint struct {
	ID                    string     `json:"id"`
	WorkflowID            string     `json:"workflow_id"`
	ProjectID             string     `json:"project_id"`
	Ordinal               int        `json:"ordinal"`
	TaskKey               string     `json:"task_key"`
	Title                 string     `json:"title"`
	TaskType              string     `json:"task_type"`
	State                 string     `json:"state"`
	Route                 string     `json:"route"`
	ExecutionMode         string     `json:"execution_mode"`
	Version               int        `json:"version"`
	RAGQuery              string     `json:"rag_query"`
	RAGBackend            string     `json:"rag_backend"`
	RAGHitIDs             []string   `json:"rag_hit_ids"`
	RAGMaxScore           float32    `json:"rag_max_score"`
	MatchThreshold        float32    `json:"match_threshold"`
	ReviewInfluenceWeight float32    `json:"review_influence_weight"`
	CandidateID           string     `json:"candidate_id,omitempty"`
	LocalProvider         string     `json:"local_provider,omitempty"`
	LocalModel            string     `json:"local_model,omitempty"`
	CloudProvider         string     `json:"cloud_provider,omitempty"`
	CloudModel            string     `json:"cloud_model,omitempty"`
	RequestArtifact       Artifact   `json:"request_artifact"`
	LookupArtifact        *Artifact  `json:"lookup_artifact,omitempty"`
	CreatedBy             string     `json:"created_by"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	CompletedAt           *time.Time `json:"completed_at,omitempty"`
}

type WorkflowTaskEvent struct {
	ID                    string         `json:"id"`
	TaskID                string         `json:"task_id"`
	Sequence              int            `json:"sequence"`
	EventType             string         `json:"event_type"`
	FromState             string         `json:"from_state"`
	ToState               string         `json:"to_state"`
	ActorPrincipalID      string         `json:"actor_principal_id"`
	ActorRole             string         `json:"actor_role"`
	Provider              string         `json:"provider,omitempty"`
	Model                 string         `json:"model,omitempty"`
	IdempotencyKey        string         `json:"idempotency_key"`
	Payload               map[string]any `json:"payload,omitempty"`
	EvidenceArtifact      *Artifact      `json:"evidence_artifact,omitempty"`
	AuthorizationDecision string         `json:"authorization_decision"`
	CerbosCallID          string         `json:"cerbos_call_id,omitempty"`
	PolicyVersion         string         `json:"policy_version,omitempty"`
	CreatedAt             time.Time      `json:"created_at"`
}

type WorkflowTaskTransition struct {
	TaskID          string
	ExpectedVersion int
	ExpectedState   string
	ResultingState  string
	CandidateID     string
	LocalProvider   string
	LocalModel      string
	CloudProvider   string
	CloudModel      string
	InfluenceWeight float32
	RAGRoute        string
	RAGBackend      string
	RAGHitIDs       []string
	RAGMaxScore     float32
	LookupArtifact  *Artifact
	Completed       bool
	Event           WorkflowTaskEvent
}
