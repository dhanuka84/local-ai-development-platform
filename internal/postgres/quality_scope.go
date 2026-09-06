package postgres

import (
	"context"
	"strings"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

// Only server-owned enum literals are substituted into reviewed SQL. No SQL,
// policy name, purpose or dimension supplied by a tool is interpolated.
func qualitySQL(ctx context.Context, query string) string {
	purpose := domain.Purpose(ctx)
	switch purpose {
	case domain.PurposeCodeChange, domain.PurposeReview:
	default:
		return query
	}
	for _, id := range []string{"id", "k.id", "item.id", "source.id", "target.id", "knowledge.id"} {
		query = strings.ReplaceAll(query, "knowledge_eligible("+id+")", "knowledge_eligible("+id+",'"+purpose+"')")
		query = strings.ReplaceAll(query, "knowledge_quality_reason("+id+")", "knowledge_quality_reason("+id+",'"+purpose+"')")
	}
	query = strings.ReplaceAll(query, "knowledge_quality_policy(k.project_id)", "knowledge_quality_policy(k.project_id,'software_knowledge','"+purpose+"')")
	return query
}
