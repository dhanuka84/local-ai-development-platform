package postgres

import (
	"context"
	"slices"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
)

func requireExecutionSourceActor(ctx context.Context, tx pgx.Tx, actor, project, product, querySHA string) error {
	l, ok := domain.ExecutionLeaseFromContext(ctx)
	if !ok {
		return requireProductActor(ctx, tx, actor, project, "operations", "incident_diagnosis")
	}
	run, step, err := checkExecutionLease(ctx, tx, l, actor)
	if err != nil {
		return err
	}
	if run.ProjectID != project || run.ProductID != product || !slices.Contains([]string{"diagnose", "verify_recovery"}, run.Stage) {
		return domain.ErrForbidden
	}
	if querySHA != "" {
		var reserved bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sdlc_source_reservations WHERE step_id=$1 AND query_sha256=$2)`, step.ID, querySHA).Scan(&reserved); err != nil {
			return err
		}
		if !reserved {
			return domain.ErrBudgetExhausted
		}
	}
	return nil
}
