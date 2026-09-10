package postgres

import (
	"context"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

func (r *Repository) FindQualityCandidates(ctx context.Context, project, query string, limit int) ([]domain.KnowledgeItem, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, qualitySQL(ctx, `SELECT `+knowledgeColumns+` FROM knowledge_items
        WHERE project_id=$1 AND knowledge_eligible(id) AND octet_length(content)<=$4
        AND search_document @@ websearch_to_tsquery('simple',$2)
        ORDER BY ts_rank_cd(search_document,websearch_to_tsquery('simple',$2)) DESC,id LIMIT $3`), project, query, limit, domain.QualityTextLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.KnowledgeItem{}
	for rows.Next() {
		item, err := scanKnowledge(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
