package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

func (r *Repository) ReadExecutionTrace(ctx context.Context, in domain.ExecutionTraceInput) (out domain.ExecutionTracePage, err error) {
	out.Through = in.Through
	out.Records = []domain.OperationRecord{}
	afterTime := time.Unix(0, 0).UTC()
	afterID := ""
	if in.After != "" {
		err = r.pool.QueryRow(ctx, `SELECT recorded_at,id::text FROM operation_records WHERE project_id=$1 AND record->>'execution_id'=$2 AND id::text=$3`, in.ProjectID, in.RunID, in.After).Scan(&afterTime, &afterID)
		if err != nil {
			return out, domain.ErrForbidden
		}
	}
	rows, err := r.pool.Query(ctx, `SELECT record FROM operation_records WHERE project_id=$1 AND record->>'execution_id'=$2 AND recorded_at<=$3 AND (recorded_at,id::text)>($4,$5) AND record->>'name' NOT IN ('mcp.sdlc_trace_get','mcp.envelope.sdlc_trace_get') ORDER BY recorded_at,id::text LIMIT 201`, in.ProjectID, in.RunID, in.Through, afterTime, afterID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var record domain.OperationRecord
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal(raw, &record); err != nil {
			return out, err
		}
		if len(out.Records) == 200 {
			out.Next = out.Records[199].ID
			break
		}
		out.Records = append(out.Records, record)
	}
	return out, rows.Err()
}
