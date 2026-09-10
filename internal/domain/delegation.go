package domain

import (
	"context"
	"time"
)

// TaskDelegation authorizes project reads and development mutations on one
// existing task. It never carries human approval or validation-executor roles.
type TaskDelegation struct {
	ID                 string     `json:"id"`
	PrincipalID        string     `json:"principal_id"`
	DelegatedBy        string     `json:"delegated_by"`
	ParentCredentialID string     `json:"-"`
	ProjectID          string     `json:"project_id"`
	WorkflowID         string     `json:"workflow_id"`
	TaskID             string     `json:"task_id"`
	CreatedAt          time.Time  `json:"created_at"`
	ExpiresAt          time.Time  `json:"expires_at"`
	RevokedAt          *time.Time `json:"revoked_at,omitempty"`
}

type DelegationRepository interface {
	RecheckTaskDelegation(context.Context, Principal) (Principal, error)
	OwnsDelegatedCandidate(context.Context, string, string, string) (bool, error)
	IssueTaskDelegation(context.Context, TaskDelegation, []byte) (TaskDelegation, error)
	RevokeTaskDelegation(context.Context, string, string) (TaskDelegation, error)
}
