package sourceadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/mcpclient"
	"github.com/jackc/pgx/v5"
)

func sqlView(name string) string { return pgx.Identifier(strings.Split(name, ".")).Sanitize() }
func readPostgres(ctx context.Context, v View, q domain.SourceQuery) (out domain.SourceEnvelope, err error) {
	dsn, err := mcpclient.ReadToken(v.Backend.DatabaseURLFile)
	if err != nil {
		return out, err
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return out, err
	}
	if !v.Backend.AllowPlaintextLocal && config.TLSConfig == nil {
		return out, errors.New("native audit connection requires TLS")
	}
	if v.Backend.AllowPlaintextLocal && config.TLSConfig == nil {
		config.DialFunc = localPlainDial
	}
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return out, err
	}
	defer conn.Close(context.Background())
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout='20s'"); err != nil {
		return out, err
	}
	out = envelope(v, q)
	var watermark *time.Time
	var snapshot string
	if err = tx.QueryRow(ctx, "SELECT min(watermark) FROM "+sqlView(v.Backend.WatermarkView)).Scan(&watermark); err != nil {
		return out, err
	}
	if err = tx.QueryRow(ctx, "SELECT pg_current_snapshot()::text").Scan(&snapshot); err != nil {
		return out, err
	}
	if watermark != nil {
		out.Watermark = watermark.UTC()
	} else {
		out.Complete = false
	}
	out.Revision = "postgres:" + snapshot
	fields := []string{}
	for _, name := range q.Fields {
		fields = append(fields, pgx.Identifier{name}.Sanitize())
	}
	params := []any{q.Start, q.End}
	where := `event_time >= $1 AND event_time < $2`
	filters := []string{}
	for name := range q.Filters {
		filters = append(filters, name)
	}
	sort.Strings(filters)
	for _, name := range filters {
		params = append(params, q.Filters[name])
		where += fmt.Sprintf(" AND %s=$%d", pgx.Identifier{name}.Sanitize(), len(params))
	}
	params = append(params, min(q.Limit, v.Backend.MaxScanRecords)+1)
	query := fmt.Sprintf("SELECT row_to_json(typed) FROM (SELECT %s FROM %s WHERE %s ORDER BY event_time,event_id LIMIT $%d) typed", strings.Join(fields, ","), sqlView(v.Backend.View), where, len(params))
	rows, err := tx.Query(ctx, query, params...)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return out, err
		}
		var row map[string]json.RawMessage
		if json.Unmarshal(raw, &row) != nil {
			rows.Close()
			return out, errors.New("audit view returned invalid typed row")
		}
		if len(out.Rows) >= q.Limit || len(out.Rows) >= v.Backend.MaxScanRecords {
			out.Complete = false
			continue
		}
		if err = appendRow(&out, q, row); err != nil {
			rows.Close()
			return out, err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
