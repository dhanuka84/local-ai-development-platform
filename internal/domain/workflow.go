package domain

import (
	"context"
	"time"
)

const (
	GovernanceSolo      = "solo"
	GovernanceTeam      = "team"
	GovernanceRegulated = "regulated"
)

type PrincipalBootstrap struct {
	ID          string
	DisplayName string
	Token       string
	Human       bool
	Roles       []string
	ProjectIDs  []string
}

func (p PrincipalBootstrap) Principal() Principal {
	bindings := make(map[string][]string, len(p.ProjectIDs))
	for _, projectID := range p.ProjectIDs {
		bindings[projectID] = append([]string(nil), p.Roles...)
	}
	return Principal{ID: p.ID, DisplayName: p.DisplayName, Human: p.Human, RoleBindings: bindings}
}

type Principal struct {
	ID           string              `json:"id"`
	DisplayName  string              `json:"display_name,omitempty"`
	Human        bool                `json:"human"`
	RoleBindings map[string][]string `json:"role_bindings"`
}

func (p Principal) ProjectIDs() []string {
	seen := make(map[string]struct{}, len(p.RoleBindings))
	result := make([]string, 0, len(p.RoleBindings))
	for projectID := range p.RoleBindings {
		if _, ok := seen[projectID]; ok {
			continue
		}
		seen[projectID] = struct{}{}
		result = append(result, projectID)
	}
	return result
}

func (p Principal) RolesFor(projectID string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, binding := range []string{"*", projectID} {
		for _, role := range p.RoleBindings[binding] {
			if _, ok := seen[role]; ok {
				continue
			}
			seen[role] = struct{}{}
			result = append(result, role)
		}
	}
	return result
}

func (p Principal) HasRole(projectID, role string) bool {
	for _, candidate := range p.RolesFor(projectID) {
		if candidate == role {
			return true
		}
	}
	return false
}

type GovernancePolicy struct {
	ProjectID              string   `json:"project_id"`
	Profile                string   `json:"profile"`
	AllowRoleOverlap       bool     `json:"allow_role_overlap"`
	DistinctPrincipalGates []string `json:"distinct_principal_gates"`
	TwoPersonGates         []string `json:"two_person_gates"`
	AlwaysHumanGates       []string `json:"always_human_gates"`
	Version                int      `json:"version"`
}

func (p GovernancePolicy) RequiresDistinctPrincipal(gate string) bool {
	if p.Profile == GovernanceRegulated {
		switch gate {
		case "plan_approval", "qa", "product_approval", "confidential_disclosure", "maintenance_action":
			return true
		}
	}
	for _, candidate := range p.DistinctPrincipalGates {
		if candidate == gate {
			return true
		}
	}
	return false
}

func (p GovernancePolicy) RequiresHuman(gate string) bool {
	for _, candidate := range p.AlwaysHumanGates {
		if candidate == gate {
			return true
		}
	}
	return false
}

type WorkflowRun struct {
	ID                 string           `json:"id"`
	ProjectID          string           `json:"project_id"`
	Kind               string           `json:"kind"`
	State              string           `json:"state"`
	Version            int              `json:"version"`
	Risk               string           `json:"risk"`
	DataClassification string           `json:"data_classification"`
	RequestArtifact    Artifact         `json:"request_artifact"`
	WorkPacketArtifact *Artifact        `json:"work_packet_artifact,omitempty"`
	OpenClawFlowID     string           `json:"openclaw_flow_id,omitempty"`
	IdempotencyKey     string           `json:"idempotency_key"`
	CreatedBy          string           `json:"created_by"`
	ImplementedBy      string           `json:"implemented_by,omitempty"`
	QAValidatedBy      string           `json:"qa_validated_by,omitempty"`
	Metadata           map[string]any   `json:"metadata,omitempty"`
	Governance         GovernancePolicy `json:"governance"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	TerminalAt         *time.Time       `json:"terminal_at,omitempty"`
}

type WorkflowEvent struct {
	ID                    string         `json:"id"`
	WorkflowID            string         `json:"workflow_id"`
	Sequence              int            `json:"sequence"`
	EventType             string         `json:"event_type"`
	FromState             string         `json:"from_state"`
	ToState               string         `json:"to_state"`
	ActorPrincipalID      string         `json:"actor_principal_id"`
	ActorRole             string         `json:"actor_role"`
	IdempotencyKey        string         `json:"idempotency_key"`
	Payload               map[string]any `json:"payload,omitempty"`
	EvidenceArtifact      *Artifact      `json:"evidence_artifact,omitempty"`
	AuthorizationDecision string         `json:"authorization_decision"`
	CerbosCallID          string         `json:"cerbos_call_id,omitempty"`
	PolicyVersion         string         `json:"policy_version,omitempty"`
	CreatedAt             time.Time      `json:"created_at"`
}

type WorkflowTransition struct {
	WorkflowID       string
	ExpectedVersion  int
	Event            WorkflowEvent
	ExpectedState    string
	ResultingState   string
	SetImplementedBy bool
	SetQAValidatedBy bool
	Terminal         bool
}

type AuthorizationRequest struct {
	Principal    Principal
	ResourceKind string
	ResourceID   string
	Action       string
	Attributes   map[string]any
}

type AuthorizationDecision struct {
	Allowed       bool   `json:"allowed"`
	CallID        string `json:"call_id,omitempty"`
	PolicyVersion string `json:"policy_version,omitempty"`
}

type Authorizer interface {
	Authorize(context.Context, AuthorizationRequest) (AuthorizationDecision, error)
	Ping(context.Context) error
}
