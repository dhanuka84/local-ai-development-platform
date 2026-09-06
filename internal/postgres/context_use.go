package postgres

import (
	"context"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) TaskUsedContexts(ctx context.Context, taskID string) ([]domain.UsedContext, error) {
	rows, err := r.pool.Query(ctx, `SELECT knowledge_id::text,candidate_version,target_repository,target_branch,target_revision FROM workflow_task_used_context WHERE task_id::text=$1 ORDER BY knowledge_id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.UsedContext{}
	for rows.Next() {
		var use domain.UsedContext
		if err = rows.Scan(&use.KnowledgeID, &use.Version, &use.TargetRepository, &use.TargetBranch, &use.TargetRevision); err != nil {
			return nil, err
		}
		out = append(out, use)
	}
	return out, rows.Err()
}

func (r *Repository) RecordTaskContext(ctx context.Context, taskID string, version int, uses []domain.UsedContext, actor, key string) error {
	if len(uses) == 0 || len(uses) > 50 || key == "" {
		return domain.ErrValidationRequired
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	task, err := getWorkflowTask(ctx, tx, taskID, true)
	if err != nil {
		return err
	}
	if task.Version != version || task.State != domain.TaskStateLocalExecution {
		return domain.ErrVersionConflict
	}
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM principals p JOIN principal_role_bindings b ON b.principal_id=p.id WHERE p.id=$1 AND p.active AND b.project_id IN('*',$2) AND b.role IN('controller','development') AND b.valid_from<=now() AND (b.valid_until IS NULL OR b.valid_until>now()))`, actor, task.ProjectID).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return domain.ErrValidationRequired
	}
	refs := []domain.EvidenceReference{}
	seen := map[string]bool{}
	for _, use := range uses {
		if seen[use.KnowledgeID] {
			return domain.ErrVersionConflict
		}
		seen[use.KnowledgeID] = true
		var validation, source string
		purpose := domain.TaskPurpose(task.TaskType)
		if use.TargetRepository != "" {
			purpose = domain.PurposeCodeChange
		}
		err = tx.QueryRow(ctx, `SELECT k.approval_validation_id::text,v.source_manifest_sha256 FROM knowledge_items k JOIN knowledge_validations v ON v.id=k.approval_validation_id WHERE k.id::text=$1 AND k.project_id=$2 AND k.version=$3 AND knowledge_eligible(k.id,$4) FOR SHARE OF k`, use.KnowledgeID, task.ProjectID, use.Version, purpose).Scan(&validation, &source)
		if err != nil {
			return domain.ErrQualityBlocked
		}
		tag, err := tx.Exec(ctx, `INSERT INTO workflow_task_used_context(task_id,knowledge_id,candidate_version,validation_id,source_manifest_sha256,target_repository,target_branch,target_revision,actor,idempotency_key)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`, task.ID, use.KnowledgeID, use.Version, validation, source, use.TargetRepository, use.TargetBranch, use.TargetRevision, actor, key)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			var same bool
			err = tx.QueryRow(ctx, `SELECT candidate_version=$3 AND validation_id::text=$4 AND source_manifest_sha256=$5 AND target_repository=$6 AND target_branch=$7 AND target_revision=$8 AND actor=$9 AND idempotency_key=$10 FROM workflow_task_used_context WHERE task_id=$1 AND knowledge_id=$2`, task.ID, use.KnowledgeID, use.Version, validation, source, use.TargetRepository, use.TargetBranch, use.TargetRevision, actor, key).Scan(&same)
			if err != nil {
				return err
			}
			if !same {
				return domain.ErrVersionConflict
			}
		}
		refs = append(refs, domain.EvidenceReference{Kind: "used_context", ID: use.KnowledgeID, Version: use.Version, SHA256: source})
	}
	if err = auditMutation(ctx, tx, "workflow.task.context.use", domain.OperationScope{ProjectID: task.ProjectID, WorkflowID: task.WorkflowID, TaskID: task.ID}, refs...); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Attach only a trusted executed report, never a caller's textual "pass".
func recordTaskLocalValidation(ctx context.Context, tx pgx.Tx, task domain.WorkflowTaskCheckpoint, event domain.WorkflowTaskEvent) error {
	validation, _ := event.Payload["validation_id"].(string)
	if validation == "" {
		return domain.ErrValidationRequired
	}
	var version int
	err := tx.QueryRow(ctx, `SELECT v.candidate_version FROM knowledge_validations v JOIN knowledge_items k ON k.id=v.knowledge_id
 JOIN knowledge_sources s ON s.validation_id=v.id JOIN LATERAL knowledge_quality_policy(k.project_id,'software_knowledge',$4) p ON true
 WHERE v.id::text=$1 AND v.knowledge_id::text=$2 AND v.project_id=$3 AND v.method='workpacket' AND v.verdict='pass'
 AND v.candidate_version=k.version AND v.content_sha256=encode(sha256(convert_to(k.content,'UTF8')),'hex')
 AND v.valid_until>now() AND v.completed_at<=now() AND v.completed_at+p.patch_max_age>now()
 AND s.valid AND s.manifest_sha256=v.source_manifest_sha256 AND s.verified_at+p.source_max_age>now() FOR SHARE OF k`, validation, task.CandidateID, task.ProjectID, domain.TaskPurpose(task.TaskType)).Scan(&version)
	if err != nil {
		return domain.ErrValidationRequired
	}
	_, err = tx.Exec(ctx, `INSERT INTO workflow_task_local_validations(task_id,validation_id,candidate_version) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, task.ID, validation, version)
	return err
}
func verifyTaskContextCompletion(ctx context.Context, tx pgx.Tx, task domain.WorkflowTaskCheckpoint, requireUse bool) error {
	// Lock the authority rows before inspecting all recorded versions. A stale
	// context cannot become successful reuse between lookup and completion.
	rows, err := tx.Query(ctx, `SELECT k.id FROM knowledge_items k JOIN workflow_task_used_context u ON u.knowledge_id=k.id WHERE u.task_id=$1 FOR SHARE OF k`, task.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var used, invalid int
	err = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE k.project_id<>$2 OR k.version<>u.candidate_version OR k.approval_validation_id IS DISTINCT FROM u.validation_id OR v.source_manifest_sha256 IS DISTINCT FROM u.source_manifest_sha256 OR u.target_repository='' OR NOT knowledge_eligible(k.id,'code_change')) FROM workflow_task_used_context u JOIN knowledge_items k ON k.id=u.knowledge_id LEFT JOIN knowledge_validations v ON v.id=k.approval_validation_id WHERE u.task_id=$1`, task.ID, task.ProjectID).Scan(&used, &invalid)
	if err != nil {
		return err
	}
	if invalid > 0 || (requireUse && used == 0) {
		return domain.ErrQualityBlocked
	}
	var validated bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workflow_task_local_validations l JOIN knowledge_validations v ON v.id=l.validation_id JOIN knowledge_items k ON k.id=v.knowledge_id JOIN knowledge_sources s ON s.validation_id=v.id JOIN LATERAL knowledge_quality_policy(k.project_id,'software_knowledge',$3) p ON true WHERE l.task_id=$1 AND v.knowledge_id::text=$2 AND v.method='workpacket' AND v.verdict='pass' AND v.candidate_version=k.version AND v.valid_until>now() AND v.completed_at+p.patch_max_age>now() AND s.valid AND s.manifest_sha256=v.source_manifest_sha256 AND s.verified_at<=now() AND s.verified_at+p.source_max_age>now())`, task.ID, task.CandidateID, domain.TaskPurpose(task.TaskType)).Scan(&validated)
	if err != nil {
		return err
	}
	if !validated {
		return domain.ErrValidationRequired
	}
	return nil
}
