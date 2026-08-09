package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) BootstrapPrincipals(ctx context.Context, principals []domain.PrincipalBootstrap) error {
	if len(principals) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, principal := range principals {
		kind := "workload"
		if principal.Human {
			kind = "human"
		}
		if _, err := tx.Exec(ctx, `INSERT INTO principals(id,display_name,kind)
            VALUES($1,$2,$3)
            ON CONFLICT (id) DO UPDATE SET display_name=EXCLUDED.display_name,
              kind=EXCLUDED.kind,active=true,updated_at=now()`, principal.ID, principal.DisplayName, kind); err != nil {
			return fmt.Errorf("bootstrap principal %q: %w", principal.ID, err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM principal_credentials
		    WHERE principal_id=$1 AND label='environment-bootstrap'`, principal.ID); err != nil {
			return err
		}
		if principal.Token != "" {
			hash := sha256.Sum256([]byte(principal.Token))
			if _, err := tx.Exec(ctx, `INSERT INTO principal_credentials(principal_id,token_sha256,label)
			    VALUES($1,$2,'environment-bootstrap')`, principal.ID, hash[:]); err != nil {
				return fmt.Errorf("bootstrap credential for %q: %w", principal.ID, err)
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM principal_role_bindings WHERE principal_id=$1`, principal.ID); err != nil {
			return err
		}
		for _, projectID := range principal.ProjectIDs {
			for _, role := range principal.Roles {
				if _, err := tx.Exec(ctx, `INSERT INTO principal_role_bindings(principal_id,project_id,role)
                    VALUES($1,$2,$3)`, principal.ID, projectID, role); err != nil {
					return fmt.Errorf("bootstrap role %s/%s for %q: %w", projectID, role, principal.ID, err)
				}
			}
		}
	}
	return tx.Commit(ctx)
}

func (r *Repository) AuthenticatePrincipal(ctx context.Context, tokenHash []byte) (domain.Principal, error) {
	rows, err := r.pool.Query(ctx, `SELECT p.id,p.display_name,p.kind,b.project_id,b.role
      FROM principal_credentials c
      JOIN principals p ON p.id=c.principal_id
      JOIN principal_role_bindings b ON b.principal_id=p.id
      WHERE c.token_sha256=$1 AND c.revoked_at IS NULL
        AND (c.expires_at IS NULL OR c.expires_at > now())
        AND p.active
        AND b.valid_from <= now() AND (b.valid_until IS NULL OR b.valid_until > now())
      ORDER BY b.project_id,b.role`, tokenHash)
	if err != nil {
		return domain.Principal{}, err
	}
	defer rows.Close()
	principal := domain.Principal{RoleBindings: make(map[string][]string)}
	for rows.Next() {
		var id, displayName, kind, projectID, role string
		if err := rows.Scan(&id, &displayName, &kind, &projectID, &role); err != nil {
			return domain.Principal{}, err
		}
		if principal.ID == "" {
			principal.ID, principal.DisplayName, principal.Human = id, displayName, kind == "human"
		}
		principal.RoleBindings[projectID] = append(principal.RoleBindings[projectID], role)
	}
	if err := rows.Err(); err != nil {
		return domain.Principal{}, err
	}
	if principal.ID == "" {
		return domain.Principal{}, errors.New("invalid or expired bearer token")
	}
	return principal, nil
}

const workflowColumns = `w.id::text,w.project_id,w.kind,w.state,w.version,w.risk,w.data_classification,
    req.sha256::text,req.uri,req.media_type,req.size_bytes,
    COALESCE(wp.sha256::text,''),COALESCE(wp.uri,''),COALESCE(wp.media_type,''),COALESCE(wp.size_bytes,0),
    w.openclaw_flow_id,w.idempotency_key,w.created_by,
    COALESCE(w.implemented_by,''),COALESCE(w.qa_validated_by,''),w.metadata,
    gp.project_id,gp.profile,gp.allow_role_overlap,gp.distinct_principal_gates,
    gp.two_person_gates,gp.always_human_gates,gp.version,
    w.created_at,w.updated_at,w.terminal_at`

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func scanWorkflow(row pgx.Row) (domain.WorkflowRun, error) {
	var run domain.WorkflowRun
	var workSHA, workURI, workMedia string
	var workSize int64
	var metadata []byte
	err := row.Scan(
		&run.ID, &run.ProjectID, &run.Kind, &run.State, &run.Version, &run.Risk, &run.DataClassification,
		&run.RequestArtifact.SHA256, &run.RequestArtifact.URI, &run.RequestArtifact.MediaType, &run.RequestArtifact.SizeBytes,
		&workSHA, &workURI, &workMedia, &workSize,
		&run.OpenClawFlowID, &run.IdempotencyKey, &run.CreatedBy, &run.ImplementedBy, &run.QAValidatedBy, &metadata,
		&run.Governance.ProjectID, &run.Governance.Profile, &run.Governance.AllowRoleOverlap,
		&run.Governance.DistinctPrincipalGates, &run.Governance.TwoPersonGates,
		&run.Governance.AlwaysHumanGates, &run.Governance.Version,
		&run.CreatedAt, &run.UpdatedAt, &run.TerminalAt,
	)
	if err != nil {
		return run, err
	}
	if workSHA != "" {
		run.WorkPacketArtifact = &domain.Artifact{SHA256: workSHA, URI: workURI, MediaType: workMedia, SizeBytes: workSize}
	}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &run.Metadata); err != nil {
			return run, fmt.Errorf("decode workflow metadata: %w", err)
		}
	}
	return run, nil
}

func workflowQuery(where string) string {
	return `SELECT ` + workflowColumns + `
      FROM workflow_runs w
      JOIN artifacts req ON req.sha256=w.request_artifact_sha256
      LEFT JOIN artifacts wp ON wp.sha256=w.work_packet_artifact_sha256
      JOIN project_governance_policies gp ON gp.project_id=w.project_id
      ` + where
}

func getWorkflowByID(ctx context.Context, queryer rowQuerier, id string, forUpdate bool) (domain.WorkflowRun, error) {
	query := workflowQuery(`WHERE w.id::text=$1`)
	if forUpdate {
		query += ` FOR UPDATE OF w`
	}
	run, err := scanWorkflow(queryer.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return run, fmt.Errorf("workflow %q not found", id)
	}
	return run, err
}

func (r *Repository) CreateWorkflow(ctx context.Context, run domain.WorkflowRun, event domain.WorkflowEvent) (domain.WorkflowRun, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO projects(id,display_name) VALUES($1,$1) ON CONFLICT (id) DO NOTHING`, run.ProjectID); err != nil {
		return domain.WorkflowRun{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO project_governance_policies(project_id,updated_by)
        VALUES($1,$2) ON CONFLICT (project_id) DO NOTHING`, run.ProjectID, run.CreatedBy); err != nil {
		return domain.WorkflowRun{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO artifacts(sha256,uri,media_type,size_bytes)
        VALUES($1,$2,$3,$4) ON CONFLICT (sha256) DO NOTHING`, run.RequestArtifact.SHA256,
		run.RequestArtifact.URI, run.RequestArtifact.MediaType, run.RequestArtifact.SizeBytes); err != nil {
		return domain.WorkflowRun{}, err
	}
	metadata, err := json.Marshal(run.Metadata)
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	result, err := tx.Exec(ctx, `INSERT INTO workflow_runs(
        id,project_id,kind,state,version,risk,data_classification,request_artifact_sha256,
        openclaw_flow_id,idempotency_key,created_by,metadata
      ) VALUES($1,$2,$3,$4,1,$5,$6,$7,$8,$9,$10,$11)
      ON CONFLICT (project_id,idempotency_key) DO NOTHING`, run.ID, run.ProjectID, run.Kind, run.State,
		run.Risk, run.DataClassification, run.RequestArtifact.SHA256, run.OpenClawFlowID,
		run.IdempotencyKey, run.CreatedBy, metadata)
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	if result.RowsAffected() == 0 {
		existing, err := scanWorkflow(tx.QueryRow(ctx, workflowQuery(`WHERE w.project_id=$1 AND w.idempotency_key=$2`), run.ProjectID, run.IdempotencyKey))
		if err != nil {
			return domain.WorkflowRun{}, err
		}
		if existing.RequestArtifact.SHA256 != run.RequestArtifact.SHA256 || existing.CreatedBy != run.CreatedBy {
			return domain.WorkflowRun{}, fmt.Errorf("workflow idempotency key %q was reused with a different payload", run.IdempotencyKey)
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.WorkflowRun{}, err
		}
		return existing, nil
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO workflow_events(
        id,workflow_id,sequence,event_type,from_state,to_state,actor_principal_id,
        actor_role,idempotency_key,payload,authorization_decision,cerbos_call_id,policy_version
      ) VALUES($1,$2,1,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, event.ID, run.ID,
		event.EventType, event.FromState, event.ToState, event.ActorPrincipalID, event.ActorRole,
		event.IdempotencyKey, payload, event.AuthorizationDecision, event.CerbosCallID, event.PolicyVersion); err != nil {
		return domain.WorkflowRun{}, err
	}
	created, err := getWorkflowByID(ctx, tx, run.ID, false)
	if err != nil {
		return domain.WorkflowRun{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.WorkflowRun{}, err
	}
	return created, nil
}

func (r *Repository) GetWorkflow(ctx context.Context, id string) (domain.WorkflowRun, error) {
	return getWorkflowByID(ctx, r.pool, id, false)
}

func scanWorkflowEvent(row pgx.Row) (domain.WorkflowEvent, error) {
	var event domain.WorkflowEvent
	var payload []byte
	var evidenceSHA, evidenceURI, evidenceMedia *string
	var evidenceSize *int64
	err := row.Scan(&event.ID, &event.WorkflowID, &event.Sequence, &event.EventType, &event.FromState,
		&event.ToState, &event.ActorPrincipalID, &event.ActorRole, &event.IdempotencyKey, &payload,
		&evidenceSHA, &evidenceURI, &evidenceMedia, &evidenceSize, &event.AuthorizationDecision,
		&event.CerbosCallID, &event.PolicyVersion, &event.CreatedAt)
	if err != nil {
		return event, err
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &event.Payload); err != nil {
			return event, err
		}
	}
	if evidenceSHA != nil {
		event.EvidenceArtifact = &domain.Artifact{SHA256: *evidenceSHA, URI: *evidenceURI, MediaType: *evidenceMedia, SizeBytes: *evidenceSize}
	}
	return event, nil
}

const workflowEventColumns = `e.id::text,e.workflow_id::text,e.sequence,e.event_type,e.from_state,e.to_state,
    e.actor_principal_id,e.actor_role,e.idempotency_key,e.payload,
    evidence.sha256::text,evidence.uri,evidence.media_type,evidence.size_bytes,
    e.authorization_decision,e.cerbos_call_id,e.policy_version,e.created_at`

func (r *Repository) TransitionWorkflow(ctx context.Context, transition domain.WorkflowTransition) (domain.WorkflowRun, domain.WorkflowEvent, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	existingEvent, existingErr := scanWorkflowEvent(tx.QueryRow(ctx, `SELECT `+workflowEventColumns+`
      FROM workflow_events e LEFT JOIN artifacts evidence ON evidence.sha256=e.evidence_artifact_sha256
      WHERE e.workflow_id::text=$1 AND e.idempotency_key=$2`, transition.WorkflowID, transition.Event.IdempotencyKey))
	if existingErr == nil {
		if existingEvent.EventType != transition.Event.EventType || existingEvent.ToState != transition.ResultingState {
			return domain.WorkflowRun{}, domain.WorkflowEvent{}, fmt.Errorf("workflow idempotency key %q was reused with a different transition", transition.Event.IdempotencyKey)
		}
		run, err := getWorkflowByID(ctx, tx, transition.WorkflowID, false)
		if err != nil {
			return domain.WorkflowRun{}, domain.WorkflowEvent{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.WorkflowRun{}, domain.WorkflowEvent{}, err
		}
		return run, existingEvent, nil
	}
	if !errors.Is(existingErr, pgx.ErrNoRows) {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, existingErr
	}
	run, err := getWorkflowByID(ctx, tx, transition.WorkflowID, true)
	if err != nil {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, err
	}
	if run.Version != transition.ExpectedVersion || run.State != transition.ExpectedState {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, fmt.Errorf(
			"workflow conflict: expected state=%s version=%d, current state=%s version=%d",
			transition.ExpectedState, transition.ExpectedVersion, run.State, run.Version)
	}
	if transition.Event.EvidenceArtifact != nil {
		artifact := transition.Event.EvidenceArtifact
		if _, err := tx.Exec(ctx, `INSERT INTO artifacts(sha256,uri,media_type,size_bytes)
          VALUES($1,$2,$3,$4) ON CONFLICT (sha256) DO NOTHING`, artifact.SHA256, artifact.URI, artifact.MediaType, artifact.SizeBytes); err != nil {
			return domain.WorkflowRun{}, domain.WorkflowEvent{}, err
		}
	}
	terminalAt := any(nil)
	if transition.Terminal {
		terminalAt = time.Now().UTC()
	}
	if _, err := tx.Exec(ctx, `UPDATE workflow_runs SET state=$2,version=version+1,updated_at=now(),
        implemented_by=CASE WHEN $3 THEN $4 ELSE implemented_by END,
        qa_validated_by=CASE WHEN $5 THEN $4 ELSE qa_validated_by END,
        terminal_at=COALESCE($6,terminal_at)
      WHERE id::text=$1`, transition.WorkflowID, transition.ResultingState, transition.SetImplementedBy,
		transition.Event.ActorPrincipalID, transition.SetQAValidatedBy, terminalAt); err != nil {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, err
	}
	transition.Event.Sequence = run.Version + 1
	payload, err := json.Marshal(transition.Event.Payload)
	if err != nil {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, err
	}
	evidenceSHA := ""
	if transition.Event.EvidenceArtifact != nil {
		evidenceSHA = transition.Event.EvidenceArtifact.SHA256
	}
	event, err := scanWorkflowEvent(tx.QueryRow(ctx, `INSERT INTO workflow_events(
        id,workflow_id,sequence,event_type,from_state,to_state,actor_principal_id,actor_role,
        idempotency_key,payload,evidence_artifact_sha256,authorization_decision,cerbos_call_id,policy_version
      ) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),$12,$13,$14)
      RETURNING id::text,workflow_id::text,sequence,event_type,from_state,to_state,
        actor_principal_id,actor_role,idempotency_key,payload,
        NULL::text,NULL::text,NULL::text,NULL::bigint,
        authorization_decision,cerbos_call_id,policy_version,created_at`, transition.Event.ID,
		transition.WorkflowID, transition.Event.Sequence, transition.Event.EventType, transition.ExpectedState,
		transition.ResultingState, transition.Event.ActorPrincipalID, transition.Event.ActorRole,
		transition.Event.IdempotencyKey, payload, evidenceSHA, transition.Event.AuthorizationDecision,
		transition.Event.CerbosCallID, transition.Event.PolicyVersion))
	if err != nil {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, err
	}
	event.EvidenceArtifact = transition.Event.EvidenceArtifact
	updated, err := getWorkflowByID(ctx, tx, transition.WorkflowID, false)
	if err != nil {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.WorkflowRun{}, domain.WorkflowEvent{}, err
	}
	return updated, event, nil
}
