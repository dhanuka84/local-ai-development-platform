package postgres

import (
	"context"
	"errors"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) FindExecutionProposal(ctx context.Context, project, product, key string) (out domain.ProductRecord, err error) {
	out, err = scanProduct(r.pool.QueryRow(ctx, `SELECT `+productColumns+` FROM product_records r WHERE project_id=$1 AND product_id=$2 AND record_key=$3 ORDER BY version DESC LIMIT 1`, project, product, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProductRecord{}, nil
	}
	return
}
