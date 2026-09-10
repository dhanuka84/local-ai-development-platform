package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
)

const delegationColumns = `d.id::text,d.principal_id,d.delegated_by,d.parent_credential_id::text,d.project_id,d.workflow_id::text,d.task_id::text,d.created_at,d.expires_at,d.revoked_at`

func (r *Repository) OwnsDelegatedCandidate(ctx context.Context, principal, task, candidate string) (bool, error) {
	var owned bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM operation_records o
        WHERE o.task_id=$1 AND o.phase='commit' AND o.outcome='success'
        AND o.record->>'name'='knowledge.capture' AND o.record->>'actor'=$2
        AND EXISTS(SELECT 1 FROM jsonb_array_elements(o.record->'references') ref
            WHERE ref->>'kind'='knowledge' AND ref->>'id'=$3))`, task, principal, candidate).Scan(&owned)
	return owned, err
}

func (r *Repository) RecheckTaskDelegation(ctx context.Context, p domain.Principal) (domain.Principal, error) {
	var hash []byte
	if p.Delegation == nil || p.CredentialID == "" {
		return domain.Principal{}, errors.New("task credential required")
	}
	if err := r.pool.QueryRow(ctx, `SELECT token_sha256 FROM principal_credentials WHERE id::text=$1 AND principal_id=$2`, p.CredentialID, p.ID).Scan(&hash); err != nil {
		return domain.Principal{}, err
	}
	return r.AuthenticatePrincipal(ctx, hash)
}

func scanDelegation(row pgx.Row) (d domain.TaskDelegation, err error) {
	err = row.Scan(&d.ID, &d.PrincipalID, &d.DelegatedBy, &d.ParentCredentialID, &d.ProjectID, &d.WorkflowID, &d.TaskID, &d.CreatedAt, &d.ExpiresAt, &d.RevokedAt)
	return
}

func (r *Repository) constrainDelegatedPrincipal(ctx context.Context, p domain.Principal) (domain.Principal, error) {
	d, err := scanDelegation(r.pool.QueryRow(ctx, `SELECT `+delegationColumns+` FROM task_delegations d WHERE d.principal_id=$1`, p.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return domain.Principal{}, err
	}
	var active bool
	err = r.pool.QueryRow(ctx, `SELECT EXISTS(
        SELECT 1 FROM principal_credentials c JOIN principals p ON p.id=c.principal_id
        JOIN principal_role_bindings b ON b.principal_id=p.id
        JOIN workflow_task_checkpoints t ON t.id=$3 JOIN workflow_runs w ON w.id=t.workflow_id
        WHERE c.id::text=$1 AND p.id=$2 AND p.active AND p.kind='human'
        AND c.revoked_at IS NULL AND (c.expires_at IS NULL OR c.expires_at>now())
        AND b.role='development' AND b.project_id IN ('*',$4)
        AND b.valid_from<=now() AND (b.valid_until IS NULL OR b.valid_until>now())
        AND w.project_id=$4 AND t.workflow_id::text=$5 AND t.completed_at IS NULL
        AND t.state NOT IN ('queued','blocked','rejected') AND w.terminal_at IS NULL
        AND NOT EXISTS(SELECT 1 FROM task_delegations parent WHERE parent.principal_id=p.id))`, d.ParentCredentialID, d.DelegatedBy, d.TaskID, d.ProjectID, d.WorkflowID).Scan(&active)
	if err != nil {
		return domain.Principal{}, err
	}
	if !active || p.Human || d.RevokedAt != nil || !d.ExpiresAt.After(time.Now()) || !p.HasRole(d.ProjectID, "development") {
		return domain.Principal{}, errors.New("invalid or expired task delegation")
	}
	p.RoleBindings = map[string][]string{d.ProjectID: {"development"}}
	p.Delegation = &d
	return p, nil
}

func (r *Repository) IssueTaskDelegation(ctx context.Context, d domain.TaskDelegation, hash []byte) (domain.TaskDelegation, error) {
	if len(hash) != 32 || d.ExpiresAt.Sub(d.CreatedAt) <= 0 || d.ExpiresAt.Sub(d.CreatedAt) > time.Hour {
		return d, domain.ErrQualityBlocked
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return d, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Lock task and exact parent credential, rechecking live authority rather than
	// relying on an earlier authentication snapshot.
	var parentExpiry *time.Time
	err = tx.QueryRow(ctx, `SELECT c.expires_at FROM principal_credentials c JOIN principals p ON p.id=c.principal_id
        WHERE c.id::text=$1 AND p.id=$2 AND p.active AND p.kind='human' AND c.revoked_at IS NULL
        AND (c.expires_at IS NULL OR c.expires_at>now())
        AND NOT EXISTS(SELECT 1 FROM task_delegations x WHERE x.principal_id=p.id)
        AND EXISTS(SELECT 1 FROM principal_role_bindings b WHERE b.principal_id=p.id AND b.role='development'
          AND b.project_id IN ('*',$3) AND b.valid_from<=now() AND (b.valid_until IS NULL OR b.valid_until>now()))
        FOR UPDATE OF c,p`, d.ParentCredentialID, d.DelegatedBy, d.ProjectID).Scan(&parentExpiry)
	if err != nil {
		return d, fmt.Errorf("delegation requires an active human development credential: %w", err)
	}
	if parentExpiry != nil && d.ExpiresAt.After(*parentExpiry) {
		d.ExpiresAt = *parentExpiry
	}
	var taskID string
	err = tx.QueryRow(ctx, `SELECT t.id::text FROM workflow_task_checkpoints t JOIN workflow_runs w ON w.id=t.workflow_id
        WHERE t.id::text=$1 AND t.workflow_id::text=$2 AND w.project_id=$3 AND t.completed_at IS NULL
        AND t.state NOT IN ('queued','blocked','rejected') AND w.terminal_at IS NULL FOR UPDATE OF t,w`, d.TaskID, d.WorkflowID, d.ProjectID).Scan(&taskID)
	if err != nil {
		return d, domain.ErrVersionConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO principals(id,display_name,kind) VALUES($1,'Scoped task delegate','workload')`, d.PrincipalID); err != nil {
		return d, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO principal_credentials(principal_id,token_sha256,label,expires_at) VALUES($1,$2,'task-delegation',$3)`, d.PrincipalID, hash, d.ExpiresAt); err != nil {
		return d, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO principal_role_bindings(principal_id,project_id,role,valid_until) VALUES($1,$2,'development',$3)`, d.PrincipalID, d.ProjectID, d.ExpiresAt); err != nil {
		return d, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO task_delegations(id,principal_id,delegated_by,parent_credential_id,project_id,workflow_id,task_id,created_at,expires_at)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, d.ID, d.PrincipalID, d.DelegatedBy, d.ParentCredentialID, d.ProjectID, d.WorkflowID, d.TaskID, d.CreatedAt, d.ExpiresAt); err != nil {
		return d, err
	}
	if err = auditMutation(ctx, tx, "credential.delegation.issue", domain.OperationScope{ProjectID: d.ProjectID, WorkflowID: d.WorkflowID, TaskID: d.TaskID}, domain.EvidenceReference{Kind: "delegation", ID: d.ID}, domain.EvidenceReference{Kind: "principal", ID: d.DelegatedBy}); err != nil {
		return d, err
	}
	return d, tx.Commit(ctx)
}

func (r *Repository) RevokeTaskDelegation(ctx context.Context, id, actor string) (domain.TaskDelegation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.TaskDelegation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	d, err := scanDelegation(tx.QueryRow(ctx, `SELECT `+delegationColumns+` FROM task_delegations d JOIN principals p ON p.id=d.delegated_by WHERE d.id::text=$1 AND d.delegated_by=$2 AND p.active AND p.kind='human' FOR UPDATE OF d`, id, actor))
	if err != nil {
		return d, err
	}
	if d.RevokedAt != nil {
		return d, nil
	}
	var revoked time.Time
	if err = tx.QueryRow(ctx, `UPDATE task_delegations SET revoked_at=now() WHERE id::text=$1 RETURNING revoked_at`, id).Scan(&revoked); err != nil {
		return d, err
	}
	d.RevokedAt = &revoked
	if err = auditMutation(ctx, tx, "credential.delegation.revoke", domain.OperationScope{ProjectID: d.ProjectID, WorkflowID: d.WorkflowID, TaskID: d.TaskID}, domain.EvidenceReference{Kind: "delegation", ID: d.ID}); err != nil {
		return d, err
	}
	return d, tx.Commit(ctx)
}
