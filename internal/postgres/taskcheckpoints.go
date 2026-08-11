package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
)

const workflowTaskColumns = `t.id::text,t.workflow_id::text,w.project_id,t.ordinal,t.task_key,t.title,t.task_type,
    t.state,t.route,t.execution_mode,t.version,t.rag_query,t.rag_backend,t.rag_hit_ids,t.rag_max_score,t.match_threshold,
    t.review_influence_weight,COALESCE(t.candidate_id::text,''),t.local_provider,t.local_model,
    t.cloud_provider,t.cloud_model,req.sha256::text,req.uri,req.media_type,req.size_bytes,
    lookup.sha256::text,lookup.uri,lookup.media_type,lookup.size_bytes,t.created_by,
    t.created_at,t.updated_at,t.completed_at`

func workflowTaskQuery(where string) string {
	return `SELECT ` + workflowTaskColumns + `
      FROM workflow_task_checkpoints t
      JOIN workflow_runs w ON w.id=t.workflow_id
      JOIN artifacts req ON req.sha256=t.request_artifact_sha256
      LEFT JOIN artifacts lookup ON lookup.sha256=t.lookup_artifact_sha256
      ` + where
}

func scanWorkflowTask(row pgx.Row) (domain.WorkflowTaskCheckpoint, error) {
	var task domain.WorkflowTaskCheckpoint
	var lookupSHA, lookupURI, lookupMedia *string
	var lookupSize *int64
	err := row.Scan(&task.ID, &task.WorkflowID, &task.ProjectID, &task.Ordinal, &task.TaskKey, &task.Title,
		&task.TaskType, &task.State, &task.Route, &task.ExecutionMode, &task.Version, &task.RAGQuery, &task.RAGBackend,
		&task.RAGHitIDs, &task.RAGMaxScore, &task.MatchThreshold, &task.ReviewInfluenceWeight,
		&task.CandidateID, &task.LocalProvider, &task.LocalModel, &task.CloudProvider, &task.CloudModel,
		&task.RequestArtifact.SHA256, &task.RequestArtifact.URI, &task.RequestArtifact.MediaType,
		&task.RequestArtifact.SizeBytes, &lookupSHA, &lookupURI, &lookupMedia, &lookupSize,
		&task.CreatedBy, &task.CreatedAt, &task.UpdatedAt, &task.CompletedAt)
	if err == nil && lookupSHA != nil {
		task.LookupArtifact = &domain.Artifact{SHA256: *lookupSHA, URI: *lookupURI, MediaType: *lookupMedia, SizeBytes: *lookupSize}
	}
	return task, err
}

func getWorkflowTask(ctx context.Context, queryer rowQuerier, id string, forUpdate bool) (domain.WorkflowTaskCheckpoint, error) {
	query := workflowTaskQuery(`WHERE t.id::text=$1`)
	if forUpdate {
		query += ` FOR UPDATE OF t`
	}
	task, err := scanWorkflowTask(queryer.QueryRow(ctx, query, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return task, fmt.Errorf("workflow task %q not found", id)
	}
	return task, err
}

const workflowTaskEventColumns = `e.id::text,e.task_id::text,e.sequence,e.event_type,e.from_state,e.to_state,
    e.actor_principal_id,e.actor_role,e.provider,e.model,e.idempotency_key,e.payload,
    evidence.sha256::text,evidence.uri,evidence.media_type,evidence.size_bytes,
    e.authorization_decision,e.cerbos_call_id,e.policy_version,e.created_at`

func scanWorkflowTaskEvent(row pgx.Row) (domain.WorkflowTaskEvent, error) {
	var event domain.WorkflowTaskEvent
	var payload []byte
	var evidenceSHA, evidenceURI, evidenceMedia *string
	var evidenceSize *int64
	err := row.Scan(&event.ID, &event.TaskID, &event.Sequence, &event.EventType, &event.FromState,
		&event.ToState, &event.ActorPrincipalID, &event.ActorRole, &event.Provider, &event.Model,
		&event.IdempotencyKey, &payload, &evidenceSHA, &evidenceURI, &evidenceMedia, &evidenceSize,
		&event.AuthorizationDecision, &event.CerbosCallID, &event.PolicyVersion, &event.CreatedAt)
	if err != nil {
		return event, err
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &event.Payload); err != nil {
			return event, err
		}
	}
	if evidenceSHA != nil {
		event.EvidenceArtifact = &domain.Artifact{
			SHA256: *evidenceSHA, URI: *evidenceURI, MediaType: *evidenceMedia, SizeBytes: *evidenceSize,
		}
	}
	return event, nil
}

func (r *Repository) CreateWorkflowTask(ctx context.Context, task domain.WorkflowTaskCheckpoint, event domain.WorkflowTaskEvent) (domain.WorkflowTaskCheckpoint, domain.WorkflowTaskEvent, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var workflowExists bool
	if err := tx.QueryRow(ctx, `SELECT true FROM workflow_runs WHERE id::text=$1 FOR UPDATE`, task.WorkflowID).Scan(&workflowExists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("workflow %q not found", task.WorkflowID)
		}
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}

	existing, existingErr := scanWorkflowTask(tx.QueryRow(ctx, workflowTaskQuery(`WHERE t.workflow_id::text=$1 AND t.task_key=$2`), task.WorkflowID, task.TaskKey))
	if existingErr == nil {
		if existing.Title != task.Title || existing.TaskType != task.TaskType || existing.RAGQuery != task.RAGQuery ||
			existing.ExecutionMode != task.ExecutionMode || existing.MatchThreshold != task.MatchThreshold {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("workflow task key %q was reused with a different payload", task.TaskKey)
		}
		firstEvent, err := scanWorkflowTaskEvent(tx.QueryRow(ctx, `SELECT `+workflowTaskEventColumns+`
          FROM workflow_task_events e LEFT JOIN artifacts evidence ON evidence.sha256=e.evidence_artifact_sha256
          WHERE e.task_id=$1 AND e.sequence=1`, existing.ID))
		if err != nil {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
		}
		return existing, firstEvent, nil
	}
	if !errors.Is(existingErr, pgx.ErrNoRows) {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, existingErr
	}

	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(ordinal),0)+1 FROM workflow_task_checkpoints
      WHERE workflow_id::text=$1`, task.WorkflowID).Scan(&task.Ordinal); err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO artifacts(sha256,uri,media_type,size_bytes)
      VALUES($1,$2,$3,$4) ON CONFLICT (sha256) DO NOTHING`, task.RequestArtifact.SHA256,
		task.RequestArtifact.URI, task.RequestArtifact.MediaType, task.RequestArtifact.SizeBytes); err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO workflow_task_checkpoints(
      id,workflow_id,ordinal,task_key,title,task_type,state,route,execution_mode,version,rag_query,rag_backend,
      rag_hit_ids,rag_max_score,match_threshold,request_artifact_sha256,created_by
	) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,1,$10,$11,$12,$13,$14,$15,$16)`, task.ID, task.WorkflowID,
		task.Ordinal, task.TaskKey, task.Title, task.TaskType, task.State, task.Route, task.ExecutionMode,
		task.RAGQuery, task.RAGBackend, task.RAGHitIDs, task.RAGMaxScore, task.MatchThreshold,
		task.RequestArtifact.SHA256, task.CreatedBy); err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO workflow_task_events(
      id,task_id,sequence,event_type,from_state,to_state,actor_principal_id,actor_role,provider,model,
      idempotency_key,payload,evidence_artifact_sha256,authorization_decision,cerbos_call_id,policy_version
    ) VALUES($1,$2,1,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, event.ID, task.ID,
		event.EventType, event.FromState, event.ToState, event.ActorPrincipalID, event.ActorRole,
		event.Provider, event.Model, event.IdempotencyKey, payload, task.RequestArtifact.SHA256,
		event.AuthorizationDecision, event.CerbosCallID, event.PolicyVersion); err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	created, err := getWorkflowTask(ctx, tx, task.ID, false)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	event.EvidenceArtifact = &created.RequestArtifact
	event.Sequence = 1
	if err := tx.Commit(ctx); err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	return created, event, nil
}

func (r *Repository) GetWorkflowTask(ctx context.Context, id string) (domain.WorkflowTaskCheckpoint, error) {
	return getWorkflowTask(ctx, r.pool, id, false)
}

func (r *Repository) GetWorkflowTaskByCandidate(ctx context.Context, candidateID string) (domain.WorkflowTaskCheckpoint, error) {
	task, err := scanWorkflowTask(r.pool.QueryRow(ctx, workflowTaskQuery(`WHERE t.candidate_id::text=$1`), candidateID))
	if errors.Is(err, pgx.ErrNoRows) {
		return task, fmt.Errorf("workflow task for candidate %q not found", candidateID)
	}
	return task, err
}

func (r *Repository) GetActivatableWorkflowTask(ctx context.Context, workflowID string) (domain.WorkflowTaskCheckpoint, bool, error) {
	task, err := scanWorkflowTask(r.pool.QueryRow(ctx, workflowTaskQuery(`WHERE t.workflow_id::text=$1
      AND t.state='queued'
      AND NOT EXISTS (
        SELECT 1 FROM workflow_task_checkpoints active
        WHERE active.workflow_id=t.workflow_id
          AND active.state NOT IN ('queued','completed','rejected')
      )
      ORDER BY t.ordinal LIMIT 1`), workflowID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.WorkflowTaskCheckpoint{}, false, nil
	}
	return task, err == nil, err
}

func (r *Repository) TransitionWorkflowTask(ctx context.Context, transition domain.WorkflowTaskTransition) (domain.WorkflowTaskCheckpoint, domain.WorkflowTaskEvent, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	existingEvent, existingErr := scanWorkflowTaskEvent(tx.QueryRow(ctx, `SELECT `+workflowTaskEventColumns+`
      FROM workflow_task_events e LEFT JOIN artifacts evidence ON evidence.sha256=e.evidence_artifact_sha256
      WHERE e.task_id::text=$1 AND e.idempotency_key=$2`, transition.TaskID, transition.Event.IdempotencyKey))
	if existingErr == nil {
		if existingEvent.EventType != transition.Event.EventType || existingEvent.ToState != transition.ResultingState {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf("workflow task idempotency key %q was reused with a different transition", transition.Event.IdempotencyKey)
		}
		task, err := getWorkflowTask(ctx, tx, transition.TaskID, false)
		if err != nil {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
		}
		return task, existingEvent, nil
	}
	if !errors.Is(existingErr, pgx.ErrNoRows) {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, existingErr
	}
	task, err := getWorkflowTask(ctx, tx, transition.TaskID, true)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	if task.Version != transition.ExpectedVersion || task.State != transition.ExpectedState {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, fmt.Errorf(
			"workflow task conflict: expected state=%s version=%d, current state=%s version=%d",
			transition.ExpectedState, transition.ExpectedVersion, task.State, task.Version)
	}
	if transition.Event.EvidenceArtifact != nil {
		artifact := transition.Event.EvidenceArtifact
		if _, err := tx.Exec(ctx, `INSERT INTO artifacts(sha256,uri,media_type,size_bytes)
        VALUES($1,$2,$3,$4) ON CONFLICT (sha256) DO NOTHING`, artifact.SHA256, artifact.URI,
			artifact.MediaType, artifact.SizeBytes); err != nil {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
		}
	}
	if transition.LookupArtifact != nil {
		artifact := transition.LookupArtifact
		if _, err := tx.Exec(ctx, `INSERT INTO artifacts(sha256,uri,media_type,size_bytes)
        VALUES($1,$2,$3,$4) ON CONFLICT (sha256) DO NOTHING`, artifact.SHA256, artifact.URI,
			artifact.MediaType, artifact.SizeBytes); err != nil {
			return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
		}
	}
	completedAt := any(nil)
	if transition.Completed {
		completedAt = time.Now().UTC()
	}
	if _, err := tx.Exec(ctx, `UPDATE workflow_task_checkpoints SET
      state=$2,version=version+1,updated_at=now(),
      candidate_id=COALESCE(NULLIF($3,'')::uuid,candidate_id),
      local_provider=CASE WHEN $4 <> '' THEN $4 ELSE local_provider END,
      local_model=CASE WHEN $5 <> '' THEN $5 ELSE local_model END,
      cloud_provider=CASE WHEN $6 <> '' THEN $6 ELSE cloud_provider END,
      cloud_model=CASE WHEN $7 <> '' THEN $7 ELSE cloud_model END,
      review_influence_weight=GREATEST(review_influence_weight,$8),
	  route=CASE WHEN $9 <> '' THEN $9 ELSE route END,
	  rag_backend=CASE WHEN $10 <> '' THEN $10 ELSE rag_backend END,
	  rag_hit_ids=CASE WHEN $10 <> '' THEN $11 ELSE rag_hit_ids END,
	  rag_max_score=CASE WHEN $10 <> '' THEN $12 ELSE rag_max_score END,
	  lookup_artifact_sha256=COALESCE(NULLIF($13,''),lookup_artifact_sha256),
      completed_at=COALESCE($14,completed_at)
    WHERE id::text=$1`, transition.TaskID, transition.ResultingState, transition.CandidateID,
		transition.LocalProvider, transition.LocalModel, transition.CloudProvider, transition.CloudModel,
		transition.InfluenceWeight, transition.RAGRoute, transition.RAGBackend, transition.RAGHitIDs,
		transition.RAGMaxScore, artifactSHA(transition.LookupArtifact), completedAt); err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	payload, err := json.Marshal(transition.Event.Payload)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	evidenceSHA := ""
	if transition.Event.EvidenceArtifact != nil {
		evidenceSHA = transition.Event.EvidenceArtifact.SHA256
	}
	sequence := task.Version + 1
	event, err := scanWorkflowTaskEvent(tx.QueryRow(ctx, `INSERT INTO workflow_task_events(
      id,task_id,sequence,event_type,from_state,to_state,actor_principal_id,actor_role,provider,model,
      idempotency_key,payload,evidence_artifact_sha256,authorization_decision,cerbos_call_id,policy_version
    ) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NULLIF($13,''),$14,$15,$16)
    RETURNING id::text,task_id::text,sequence,event_type,from_state,to_state,actor_principal_id,
      actor_role,provider,model,idempotency_key,payload,NULL::text,NULL::text,NULL::text,NULL::bigint,
      authorization_decision,cerbos_call_id,policy_version,created_at`, transition.Event.ID, transition.TaskID,
		sequence, transition.Event.EventType, transition.ExpectedState, transition.ResultingState,
		transition.Event.ActorPrincipalID, transition.Event.ActorRole, transition.Event.Provider,
		transition.Event.Model, transition.Event.IdempotencyKey, payload, evidenceSHA,
		transition.Event.AuthorizationDecision, transition.Event.CerbosCallID, transition.Event.PolicyVersion))
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	event.EvidenceArtifact = transition.Event.EvidenceArtifact
	updated, err := getWorkflowTask(ctx, tx, transition.TaskID, false)
	if err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.WorkflowTaskCheckpoint{}, domain.WorkflowTaskEvent{}, err
	}
	return updated, event, nil
}

func artifactSHA(artifact *domain.Artifact) string {
	if artifact == nil {
		return ""
	}
	return artifact.SHA256
}
