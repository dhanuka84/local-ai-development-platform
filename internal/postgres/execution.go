package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	"github.com/jackc/pgx/v5"
)

func scanExecution(row pgx.Row) (r domain.ExecutionRun, err error) {
	var raw []byte
	var credential string
	err = row.Scan(&raw, &credential)
	if err == nil {
		err = json.Unmarshal(raw, &r)
		r.OwnerCredential = credential
	}
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrForbidden
	}
	return
}

func executionActor(ctx context.Context, tx pgx.Tx, actor, project, role string, human bool) error {
	p, ok := identity.PrincipalFromContext(ctx)
	if !ok || p.ID != actor || p.CredentialID == "" || p.Human != human || p.Delegation != nil {
		return domain.ErrForbidden
	}
	return liveExecutionCredential(ctx, tx, actor, p.CredentialID, project, role, human)
}

func liveExecutionCredential(ctx context.Context, tx pgx.Tx, actor, credential, project, role string, human bool) error {
	var active bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM principals p JOIN principal_credentials c ON c.principal_id=p.id
 WHERE p.id=$1 AND c.id::text=$2 AND p.active AND (p.kind='human')=$5 AND c.revoked_at IS NULL AND (c.expires_at IS NULL OR c.expires_at>now())
 AND EXISTS(SELECT 1 FROM principal_role_bindings b WHERE b.principal_id=p.id AND b.project_id IN ('*',$3) AND b.role=$4 AND b.valid_from<=now() AND (b.valid_until IS NULL OR b.valid_until>now())))`, actor, credential, project, role, human).Scan(&active)
	if err != nil {
		return err
	}
	if !active {
		return domain.ErrForbidden
	}
	return nil
}

func executionOwner(ctx context.Context, tx pgx.Tx, run domain.ExecutionRun) error {
	return liveExecutionCredential(ctx, tx, run.Owner, run.OwnerCredential, run.ProjectID, "operations", true)
}

func lockExecutionRequirements(ctx context.Context, tx pgx.Tx, run domain.ExecutionRun) error {
	if err := lockActivePackages(ctx, tx, run); err != nil {
		return err
	}
	bindings := append([]domain.ProductBinding{run.Intent}, run.Bindings...)
	slices.SortFunc(bindings, func(a, b domain.ProductBinding) int {
		if a.RecordID < b.RecordID {
			return -1
		}
		if a.RecordID > b.RecordID {
			return 1
		}
		return 0
	})
	for _, b := range bindings {
		var id string
		err := tx.QueryRow(ctx, `SELECT r.id::text FROM product_records r JOIN product_record_heads h ON h.record_id=r.id
 WHERE r.project_id=$1 AND r.product_id=$2 AND r.id::text=$3 AND r.sha256=$4 AND r.kind=$5 AND product_record_eligible(r.id) FOR SHARE OF h`, run.ProjectID, run.ProductID, b.RecordID, b.SHA256, b.Kind).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrVersionConflict
		}
		if err != nil {
			return err
		}
		if b.Kind == "code" {
			var content string
			if err = tx.QueryRow(ctx, `SELECT record->>'content' FROM product_records WHERE id=$1`, b.RecordID).Scan(&content); err != nil {
				return err
			}
			var code domain.ProductCodeBinding
			if json.Unmarshal([]byte(content), &code) != nil || code.Schema != "hybrid-ai/product-code/v1" || code.RepositoryID != run.Target.RepositoryID || run.Kind == "feature" && code.Revision != run.RepositoryRevision {
				return domain.ErrValidationRequired
			}
			var revision string
			err = tx.QueryRow(ctx, `SELECT a.revision FROM code_repository_heads h JOIN software_repositories r ON r.id=h.repository_id JOIN code_analysis_runs a ON a.id=h.analysis_run_id WHERE r.project_id=$1 AND (r.id::text=$2 OR r.name=$2 OR r.canonical_url=$2) FOR SHARE OF h`, run.ProjectID, code.RepositoryID).Scan(&revision)
			if err != nil || revision != code.Revision {
				return domain.ErrVersionConflict
			}
		}
	}
	return nil
}

func saveExecution(ctx context.Context, tx pgx.Tx, run *domain.ExecutionRun, kind, actor, step, reason string, evidence domain.Artifact) error {
	run.Version++
	run.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(run)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE sdlc_executions SET record=$2 WHERE id=$1`, run.ID, raw); err != nil {
		return err
	}
	id, err := domain.NewID()
	if err != nil {
		return err
	}
	event := domain.ExecutionEvent{ID: id, RunID: run.ID, StepID: step, Kind: kind, Actor: actor, Version: run.Version, Reason: reason, Evidence: evidence, CreatedAt: run.UpdatedAt}
	raw, err = json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO sdlc_execution_events(id,run_id,record) VALUES($1,$2,$3)`, id, run.ID, raw); err != nil {
		return err
	}
	return auditMutation(ctx, tx, "sdlc."+kind, domain.OperationScope{ProjectID: run.ProjectID, ExecutionID: run.ID, ExecutionStepID: step}, domain.EvidenceReference{Kind: "execution", ID: run.ID, Version: run.Version}, domain.EvidenceReference{Kind: "artifact", ID: evidence.SHA256, SHA256: evidence.SHA256})
}

func (r *Repository) CreateExecution(ctx context.Context, in domain.ExecutionRun) (domain.ExecutionRun, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return in, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = executionActor(ctx, tx, in.Owner, in.ProjectID, "operations", true); err != nil {
		return in, err
	}
	p, _ := identity.PrincipalFromContext(ctx)
	if in.OwnerCredential != p.CredentialID || in.Schema != domain.ExecutionSchema || in.Status != "ready" || in.Version != 0 || in.StepNumber != 0 || in.Usage != (domain.ExecutionUsage{}) || (in.Kind != "feature" && in.Kind != "incident") {
		return in, domain.ErrValidationRequired
	}
	if err = lockExecutionRequirements(ctx, tx, in); err != nil {
		return in, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "sdlc-create:"+in.ProjectID+":"+in.Owner); err != nil {
		return in, err
	}
	existing, err := scanExecution(tx.QueryRow(ctx, `SELECT record,owner_credential_id::text FROM sdlc_executions WHERE project_id=$1 AND owner=$2 AND idempotency_key=$3`, in.ProjectID, in.Owner, in.IdempotencyKey))
	if err == nil {
		if existing.RequestSHA256 != in.RequestSHA256 {
			return in, domain.ErrVersionConflict
		}
		return existing, nil
	}
	if !errors.Is(err, domain.ErrForbidden) {
		return in, err
	}
	for role, actor := range in.Target.Participants {
		var allowed bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM principals p WHERE p.id=$1 AND p.active AND p.kind='workload'
 AND EXISTS(SELECT 1 FROM principal_credentials c WHERE c.principal_id=p.id AND c.revoked_at IS NULL AND (c.expires_at IS NULL OR c.expires_at>now()))
 AND EXISTS(SELECT 1 FROM principal_role_bindings b WHERE b.principal_id=p.id AND b.project_id IN ('*',$2) AND b.role=$3 AND b.valid_from<=now() AND (b.valid_until IS NULL OR b.valid_until>now()))
 AND NOT EXISTS(SELECT 1 FROM principal_role_bindings b WHERE b.principal_id=p.id AND b.role<>$3 AND b.valid_from<=now() AND (b.valid_until IS NULL OR b.valid_until>now())))`, actor, in.ProjectID, role).Scan(&allowed)
		if err != nil {
			return in, err
		}
		if !allowed {
			return in, domain.ErrForbidden
		}
	}
	var active int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM sdlc_executions WHERE project_id=$1 AND record->'target'->>'id'=$2 AND record->>'status' IN ('ready','running','reconciling')`, in.ProjectID, in.Target.ID).Scan(&active); err != nil {
		return in, err
	}
	if active >= in.Target.Budget.MaxConcurrentRuns {
		return in, domain.ErrResourceBusy
	}
	raw, _ := json.Marshal(in)
	if _, err = tx.Exec(ctx, `INSERT INTO sdlc_executions(id,project_id,product_id,owner,owner_credential_id,idempotency_key,request_sha256,record) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, in.ID, in.ProjectID, in.ProductID, in.Owner, in.OwnerCredential, in.IdempotencyKey, in.RequestSHA256, raw); err != nil {
		return in, err
	}
	if err = saveExecution(ctx, tx, &in, "created", in.Owner, "", "accepted_intent_and_distinct_scoped_workers", domain.Artifact{}); err != nil {
		return in, err
	}
	return in, tx.Commit(ctx)
}

func (r *Repository) GetExecution(ctx context.Context, project, id string) (out domain.ExecutionView, err error) {
	out.Run, err = scanExecution(r.pool.QueryRow(ctx, `SELECT record,owner_credential_id::text FROM sdlc_executions WHERE project_id=$1 AND id::text=$2`, project, id))
	if err != nil {
		return
	}
	out.Steps = []domain.ExecutionStep{}
	out.Events = []domain.ExecutionEvent{}
	rows, err := r.pool.Query(ctx, `SELECT record FROM sdlc_execution_steps WHERE run_id=$1 ORDER BY step_number`, out.Run.ID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var raw []byte
		var step domain.ExecutionStep
		if err = rows.Scan(&raw); err != nil {
			break
		}
		if err = json.Unmarshal(raw, &step); err != nil {
			break
		}
		out.Steps = append(out.Steps, step)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if rowErr != nil {
		return out, rowErr
	}
	rows, err = r.pool.Query(ctx, `SELECT record FROM sdlc_execution_events WHERE run_id=$1 ORDER BY (record->>'version')::integer`, out.Run.ID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var event domain.ExecutionEvent
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal(raw, &event); err != nil {
			return out, err
		}
		out.Events = append(out.Events, event)
	}
	return out, rows.Err()
}

func (r *Repository) ListExecutions(ctx context.Context, project, actor string, limit int) (out []domain.ExecutionRun, err error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	rows, err := r.pool.Query(ctx, `SELECT record,owner_credential_id::text FROM sdlc_executions WHERE project_id=$1 AND (owner=$2 OR EXISTS(SELECT 1 FROM jsonb_each_text(record->'target'->'participants') a WHERE a.value=$2)) ORDER BY (record->>'created_at')::timestamptz DESC LIMIT $3`, project, actor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out = []domain.ExecutionRun{}
	for rows.Next() {
		run, e := scanExecution(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func lockExecution(ctx context.Context, tx pgx.Tx, project, id string) (domain.ExecutionRun, error) {
	return scanExecution(tx.QueryRow(ctx, `SELECT record,owner_credential_id::text FROM sdlc_executions WHERE project_id=$1 AND id::text=$2 FOR UPDATE`, project, id))
}
func saveExecutionStep(ctx context.Context, tx pgx.Tx, step domain.ExecutionStep) error {
	raw, err := json.Marshal(step)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE sdlc_execution_steps SET lease_sha256=$2,lease_until=$3,fence=$4,record=$5,completed_at=$6 WHERE id=$1`, step.ID, step.LeaseSHA, step.LeaseUntil, step.Fence, raw, step.CompletedAt)
	return err
}
func loadExecutionStep(ctx context.Context, tx pgx.Tx, run string, number int) (step domain.ExecutionStep, err error) {
	var raw []byte
	var hash string
	err = tx.QueryRow(ctx, `SELECT record,lease_sha256 FROM sdlc_execution_steps WHERE run_id=$1 AND step_number=$2 FOR UPDATE`, run, number).Scan(&raw, &hash)
	if err == nil {
		err = json.Unmarshal(raw, &step)
		step.LeaseSHA = hash
	}
	return
}

func (r *Repository) ClaimExecution(ctx context.Context, project, id, actor, token string) (out domain.ExecutionClaim, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	run, err := lockExecution(ctx, tx, project, id)
	if err != nil {
		return out, err
	}
	if actor != run.Actor() {
		return out, domain.ErrForbidden
	}
	if err = executionActor(ctx, tx, actor, project, domain.ExecutionRole(run.Stage), false); err != nil {
		return out, err
	}
	if err = executionOwner(ctx, tx, run); err != nil {
		return out, err
	}
	if run.Status != "ready" && run.Status != "running" && run.Status != "reconciling" {
		return out, domain.ErrExecutionBlocked
	}
	if err = lockExecutionRequirements(ctx, tx, run); err != nil {
		run.Status = "blocked"
		run.Blockers = []string{"accepted_context_changed"}
		if e := saveExecution(ctx, tx, &run, "blocked", actor, "", "accepted_context_changed", domain.Artifact{}); e != nil {
			return out, e
		}
		if e := tx.Commit(ctx); e != nil {
			return out, e
		}
		return out, err
	}
	now := time.Now().UTC()
	pkg := run.Package()
	if !run.Deadline.After(now) {
		run.Status = "blocked"
		run.Blockers = []string{"deadline_exhausted"}
		if err = saveExecution(ctx, tx, &run, "blocked", actor, "", "deadline_exhausted", domain.Artifact{}); err != nil {
			return out, err
		}
		if err = tx.Commit(ctx); err != nil {
			return out, err
		}
		return out, domain.ErrBudgetExhausted
	}
	step, err := loadExecutionStep(ctx, tx, run.ID, run.StepNumber)
	reclaim := err == nil && step.CompletedAt == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	if reclaim && step.LeaseUntil.After(now) {
		return out, domain.ErrResourceBusy
	}
	resource := "executor:" + pkg.Role
	if pkg.Model != "" {
		resource = "ollama:" + pkg.ModelSHA256
	}
	if pkg.EvaluatorImage != "" {
		resource = "sandbox:" + pkg.EvaluatorImage
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, resource); err != nil {
		return out, err
	}
	var active, concurrency int
	if err = tx.QueryRow(ctx, `SELECT count(*),COALESCE(min(concurrency),$2) FROM sdlc_execution_steps WHERE resource_key=$1 AND completed_at IS NULL AND lease_until>now()`, resource, pkg.Concurrency).Scan(&active, &concurrency); err != nil {
		return out, err
	}
	if active >= min(concurrency, pkg.Concurrency) {
		return out, domain.ErrResourceBusy
	}
	if pkg.Model != "" {
		charge := pkg.MaxTokenCharge()
		if run.Usage.ModelCalls >= run.Target.Budget.MaxModelCalls || run.Usage.TokensCharged+charge > run.Target.Budget.MaxTokens || run.Usage.Attempts >= run.Target.Budget.MaxAttempts {
			run.Status = "blocked"
			run.Blockers = []string{"model_budget_exhausted"}
			if err = saveExecution(ctx, tx, &run, "blocked", actor, "", "model_budget_exhausted", domain.Artifact{}); err != nil {
				return out, err
			}
			if err = tx.Commit(ctx); err != nil {
				return out, err
			}
			return out, domain.ErrBudgetExhausted
		}
		run.Usage.ModelCalls++
		run.Usage.TokensCharged += charge
		run.Usage.Attempts++
	}
	if !reclaim {
		sid, e := domain.NewID()
		if e != nil {
			return out, e
		}
		run.StepNumber++
		step = domain.ExecutionStep{ID: sid, RunID: run.ID, Number: run.StepNumber, Stage: run.Stage, Actor: actor, Role: pkg.Role, PackageSHA: pkg.Digest(), StartedAt: now}
	}
	step.Fence++
	step.LeaseSHA = domain.Digest([]byte(token))
	step.LeaseUntil = minTime(now.Add(time.Duration(pkg.TimeoutSeconds+30)*time.Second), run.Deadline)
	raw, _ := json.Marshal(step)
	if !reclaim {
		_, err = tx.Exec(ctx, `INSERT INTO sdlc_execution_steps(id,run_id,step_number,actor,lease_sha256,lease_until,fence,resource_key,concurrency,record) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, step.ID, run.ID, step.Number, actor, step.LeaseSHA, step.LeaseUntil, step.Fence, resource, pkg.Concurrency, raw)
	} else {
		err = saveExecutionStep(ctx, tx, step)
	}
	if err != nil {
		return out, err
	}
	if run.Status != "reconciling" {
		run.Status = "running"
	}
	run.Blockers = []string{}
	kind := "claimed"
	if reclaim {
		kind = "reclaimed"
	}
	if err = saveExecution(ctx, tx, &run, kind, actor, step.ID, "lease_and_conservative_budget_reserved", domain.Artifact{}); err != nil {
		return out, err
	}
	out = domain.ExecutionClaim{Run: run, Step: step, Token: token}
	return out, tx.Commit(ctx)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func checkExecutionLease(ctx context.Context, tx pgx.Tx, l domain.ExecutionLease, actor string) (run domain.ExecutionRun, step domain.ExecutionStep, err error) {
	run, err = lockExecution(ctx, tx, l.ProjectID, l.RunID)
	if err != nil {
		return
	}
	if actor != run.Actor() {
		err = domain.ErrForbidden
		return
	}
	if err = executionActor(ctx, tx, actor, run.ProjectID, domain.ExecutionRole(run.Stage), false); err != nil {
		return
	}
	if err = executionOwner(ctx, tx, run); err != nil {
		return
	}
	step, err = loadExecutionStep(ctx, tx, run.ID, run.StepNumber)
	if err != nil {
		return
	}
	if step.ID != l.StepID || step.Actor != actor || step.CompletedAt != nil || step.Fence != l.Fence || step.LeaseSHA != domain.Digest([]byte(l.Token)) || !step.LeaseUntil.After(time.Now()) || (run.Status != "running" && run.Status != "reconciling") {
		err = domain.ErrLeaseLost
		return
	}
	if !run.Deadline.After(time.Now()) {
		err = domain.ErrBudgetExhausted
		return
	}
	err = lockExecutionRequirements(ctx, tx, run)
	return
}

func (r *Repository) CheckExecutionLease(ctx context.Context, l domain.ExecutionLease, actor string) (domain.ExecutionView, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ExecutionView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, _, err = checkExecutionLease(ctx, tx, l, actor)
	if err != nil {
		return domain.ExecutionView{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.ExecutionView{}, err
	}
	return r.GetExecution(ctx, l.ProjectID, l.RunID)
}

func (r *Repository) CompleteExecution(ctx context.Context, l domain.ExecutionLease, actor string, result domain.ExecutionResult, evidence domain.Artifact, status, stage string) (run domain.ExecutionRun, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return run, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// An acknowledged exact result can be retried after a lost response, but a
	// different result for the same step cannot overwrite immutable evidence.
	run, err = lockExecution(ctx, tx, l.ProjectID, l.RunID)
	if err != nil {
		return run, err
	}
	var priorSHA, priorActor string
	e := tx.QueryRow(ctx, `SELECT x.sha256,x.actor FROM sdlc_execution_results x JOIN sdlc_execution_steps s ON s.id=x.step_id WHERE s.run_id=$1 AND s.id::text=$2`, run.ID, l.StepID).Scan(&priorSHA, &priorActor)
	if e == nil {
		if priorActor != actor || priorSHA != evidence.SHA256 {
			return run, domain.ErrVersionConflict
		}
		if err = executionActor(ctx, tx, actor, run.ProjectID, domain.ExecutionRole(result.Stage), false); err != nil {
			return run, err
		}
		return run, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return run, e
	}
	run, step, err := checkExecutionLease(ctx, tx, l, actor)
	if err != nil {
		return run, err
	}
	if step.ContextSHA == "" || result.Stage != step.Stage || !validExecutionTransition(step.Stage, result.Outcome, status, stage) {
		return run, domain.ErrValidationRequired
	}
	if err = saveProductArtifact(ctx, tx, evidence); err != nil {
		return run, err
	}
	raw, _ := json.Marshal(result)
	if _, err = tx.Exec(ctx, `INSERT INTO sdlc_execution_results(step_id,actor,sha256,result) VALUES($1,$2,$3,$4)`, step.ID, actor, evidence.SHA256, raw); err != nil {
		return run, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO sdlc_execution_artifacts(run_id,step_id,sha256,actor) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, run.ID, step.ID, evidence.SHA256, actor); err != nil {
		return run, err
	}
	now := time.Now().UTC()
	step.CompletedAt = &now
	step.Result = &result
	step.Evidence = evidence
	if err = saveExecutionStep(ctx, tx, step); err != nil {
		return run, err
	}
	if result.Model != nil {
		run.Usage.TokensActual += result.Model.InputTokens + result.Model.OutputTokens
	}
	if run.Status == "reconciling" {
		status = "blocked"
		if result.Outcome == "succeeded" {
			status = "cancelled"
		}
		stage = run.Stage
	}
	run.Status = status
	run.Stage = stage
	run.Blockers = []string{}
	if status == "blocked" {
		run.Blockers = []string{result.Outcome + ": " + result.Summary}
	}
	if err = saveExecution(ctx, tx, &run, "result", actor, step.ID, result.Outcome, evidence); err != nil {
		return run, err
	}
	return run, tx.Commit(ctx)
}

func validExecutionTransition(from, outcome, status, to string) bool {
	if outcome == "blocked" || outcome == "unavailable" || outcome == "inconclusive" {
		return status == "blocked" && from == to
	}
	if outcome == "failed" {
		return (from == "evaluate" && status == "ready" && to == "build") || (status == "blocked" && from == to)
	}
	if outcome != "succeeded" {
		return false
	}
	next := map[string]string{"build": "evaluate", "evaluate": "deliver", "deliver": "verify_delivery", "diagnose": "remediate", "remediate": "verify_recovery"}
	if from == "verify_delivery" || from == "verify_recovery" {
		return status == "completed" && to == from
	}
	return status == "ready" && next[from] == to
}

func (r *Repository) ControlExecution(ctx context.Context, project, id, actor string, version int, action, reason string) (run domain.ExecutionRun, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return run, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	run, err = lockExecution(ctx, tx, project, id)
	if err != nil {
		return run, err
	}
	if actor != run.Owner {
		return run, domain.ErrForbidden
	}
	if err = executionActor(ctx, tx, actor, project, "operations", true); err != nil {
		return run, err
	}
	if run.Version != version {
		return run, domain.ErrVersionConflict
	}
	if run.Status == "completed" || run.Status == "cancelled" {
		return run, domain.ErrExecutionBlocked
	}
	step, stepErr := loadExecutionStep(ctx, tx, run.ID, run.StepNumber)
	inFlight := stepErr == nil && step.CompletedAt == nil
	switch action {
	case "reconcile":
		if !inFlight || (run.Stage != "deliver" && run.Stage != "remediate") || step.ContextSHA == "" {
			return run, domain.ErrExecutionBlocked
		}
		// An explicit current operator decision grants only bounded read-back.
		// It cannot resume writes or silently renew the original delegation.
		p, _ := identity.PrincipalFromContext(ctx)
		run.OwnerCredential = p.CredentialID
		run.Deadline = time.Now().UTC().Add(5 * time.Minute)
		run.Status = "reconciling"
		run.Blockers = []string{"operator_authorized_effect_read_back_only"}
		step.LeaseUntil = time.Now().UTC()
		step.Fence++
		if _, err = tx.Exec(ctx, `UPDATE sdlc_executions SET owner_credential_id=$2 WHERE id=$1`, run.ID, run.OwnerCredential); err != nil {
			return run, err
		}
		if err = saveExecutionStep(ctx, tx, step); err != nil {
			return run, err
		}
	case "pause", "cancel":
		if inFlight && (run.Stage == "deliver" || run.Stage == "remediate") {
			// A timeout/cancellation is not proof that an external write did not
			// occur. Keep its action identity and require its executor's read-back.
			run.Status = "reconciling"
			run.Blockers = []string{"cancellation_requires_effect_read_back"}
		} else {
			run.Status = "paused"
			if action == "cancel" {
				run.Status = "cancelled"
			}
			run.Blockers = []string{reason}
			if inFlight {
				step.LeaseUntil = time.Now().UTC()
				step.Fence++
				if err = saveExecutionStep(ctx, tx, step); err != nil {
					return run, err
				}
			}
		}
	case "resume":
		if run.Status != "paused" && run.Status != "blocked" {
			return run, domain.ErrExecutionBlocked
		}
		if err = executionOwner(ctx, tx, run); err != nil {
			return run, err
		}
		if err = lockExecutionRequirements(ctx, tx, run); err != nil {
			return run, err
		}
		if !run.Deadline.After(time.Now()) {
			return run, domain.ErrBudgetExhausted
		}
		run.Status = "ready"
		run.Blockers = []string{}
	default:
		return run, domain.ErrValidationRequired
	}
	if err = saveExecution(ctx, tx, &run, action, actor, "", reason, domain.Artifact{}); err != nil {
		return run, err
	}
	return run, tx.Commit(ctx)
}

func (r *Repository) ReserveExecutionSource(ctx context.Context, l domain.ExecutionLease, actor string, q domain.SourceQuery) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	run, step, err := checkExecutionLease(ctx, tx, l, actor)
	if err != nil {
		return err
	}
	if !slices.Contains([]string{"diagnose", "verify_recovery"}, run.Stage) || q.ProjectID != run.ProjectID || q.ProductID != run.ProductID || !slices.Contains(run.Target.Sources, q.SourceID) || !slices.Contains(run.Target.Purposes, q.Purpose) || q.Limit < 1 {
		return domain.ErrForbidden
	}
	raw, _ := json.Marshal(q)
	sha := domain.Digest(raw)
	var prior string
	err = tx.QueryRow(ctx, `SELECT query_sha256 FROM sdlc_source_reservations WHERE step_id=$1 AND idempotency_key=$2`, step.ID, q.IdempotencyKey).Scan(&prior)
	if err == nil {
		if prior != sha {
			return domain.ErrVersionConflict
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if run.Usage.SourceQueries >= run.Target.Budget.MaxSourceQueries || run.Usage.SourceRows+q.Limit > run.Target.Budget.MaxSourceRows {
		return domain.ErrBudgetExhausted
	}
	run.Usage.SourceQueries++
	run.Usage.SourceRows += q.Limit
	if _, err = tx.Exec(ctx, `INSERT INTO sdlc_source_reservations(step_id,idempotency_key,query_sha256) VALUES($1,$2,$3)`, step.ID, q.IdempotencyKey, sha); err != nil {
		return err
	}
	if err = saveExecution(ctx, tx, &run, "source_reserved", actor, step.ID, "bounded_query_reserved_before_dispatch", domain.Artifact{}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) RecordExecutionArtifact(ctx context.Context, l domain.ExecutionLease, actor string, a domain.Artifact, bindContext bool) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	run, step, err := checkExecutionLease(ctx, tx, l, actor)
	if err != nil {
		return err
	}
	if bindContext && step.ContextSHA != "" && step.ContextSHA != a.SHA256 {
		return domain.ErrVersionConflict
	}
	var count int
	var bytes int64
	if err = tx.QueryRow(ctx, `SELECT count(*),COALESCE(sum(a.size_bytes),0) FROM sdlc_execution_artifacts x JOIN artifacts a ON a.sha256=x.sha256 WHERE x.run_id=$1`, run.ID).Scan(&count, &bytes); err != nil {
		return err
	}
	if count >= 256 || bytes+a.SizeBytes > 64*1024*1024 {
		return domain.ErrBudgetExhausted
	}
	if err = saveProductArtifact(ctx, tx, a); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO sdlc_execution_artifacts(run_id,step_id,sha256,actor) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, run.ID, step.ID, a.SHA256, actor); err != nil {
		return err
	}
	if bindContext {
		step.ContextSHA = a.SHA256
		if err = saveExecutionStep(ctx, tx, step); err != nil {
			return err
		}
	}
	if err = auditMutation(ctx, tx, "sdlc.artifact", domain.OperationScope{ProjectID: run.ProjectID, ExecutionID: run.ID, ExecutionStepID: step.ID}, domain.EvidenceReference{Kind: "artifact", ID: a.SHA256, SHA256: a.SHA256}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) ExecutionArtifact(ctx context.Context, project, run, sha string) (a domain.Artifact, err error) {
	err = r.pool.QueryRow(ctx, `SELECT a.sha256,a.uri,a.media_type,a.size_bytes FROM artifacts a WHERE a.sha256=$3 AND EXISTS(SELECT 1 FROM sdlc_execution_artifacts x JOIN sdlc_executions r ON r.id=x.run_id WHERE r.project_id=$1 AND r.id::text=$2 AND x.sha256=a.sha256)`, project, run, sha).Scan(&a.SHA256, &a.URI, &a.MediaType, &a.SizeBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrForbidden
	}
	return
}

var _ domain.ExecutionRepository = (*Repository)(nil)

func (r *Repository) CheckExecutionReader(ctx context.Context, run domain.ExecutionRun, actor string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if !run.Deadline.After(time.Now()) {
		return domain.ErrForbidden
	}
	if err = executionOwner(ctx, tx, run); err != nil {
		return err
	}
	for role, id := range run.Target.Participants {
		if id == actor {
			return executionActor(ctx, tx, actor, run.ProjectID, role, false)
		}
	}
	return domain.ErrForbidden
}

// Keep database errors contextual without including worker result bodies.
func executionError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("execution persistence: %w", err)
}
