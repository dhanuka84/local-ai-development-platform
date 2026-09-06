// Package contextregistry is a source-controlled, reviewed query allowlist.
// Descriptions and model output never become executable SQL.
package contextregistry

import (
	"encoding/json"
	"fmt"
	"github.com/dhanuka84/hybrid-ai-platform/contracts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"time"
)

func Load() ([]domain.ContextDefinition, string, error) {
	raw, err := contracts.Files.ReadFile("context/v1/registry.valid.json")
	if err != nil {
		return nil, "", err
	}
	if err = contracts.Validate("context/v1/registry.schema.json", raw); err != nil {
		return nil, "", err
	}
	var defs []domain.ContextDefinition
	if err = json.Unmarshal(raw, &defs); err != nil {
		return nil, "", err
	}
	seen := map[string]bool{}
	for _, d := range defs {
		if seen[d.ID] {
			return nil, "", fmt.Errorf("duplicate definition")
		}
		seen[d.ID] = true
		if d.Kind == "metric" {
			if _, err = SQL(d.ID); err != nil {
				return nil, "", err
			}
		}
	}
	canonical, _ := json.Marshal(defs)
	// Bind executable meaning as well as prose; changing a fixed query requires
	// a new registry validation and versioned approval, not silent semantic drift.
	for _, d := range defs {
		if d.Kind == "metric" {
			query, _ := SQL(d.ID)
			canonical = append(canonical, []byte("\n"+d.ID+"\n"+query)...)
		}
		if d.InputContract != "" {
			schema, err := contracts.Files.ReadFile(d.InputContract)
			if err != nil {
				return nil, "", err
			}
			canonical = append(canonical, schema...)
		}
	}
	return defs, domain.Digest(canonical), nil
}
func ValidateRequest(r domain.MetricRequest) error {
	if r.ProjectID == "" || r.Version != 1 || r.Start.IsZero() || !r.End.After(r.Start) || r.End.Sub(r.Start) > 366*24*time.Hour || r.End.After(time.Now().Add(time.Minute)) {
		return domain.ErrValidationRequired
	}
	for k, v := range r.Dimensions {
		if k != "task_type" || len(v) < 1 || len(v) > 64 || r.MetricID == "pending_index_age_seconds" {
			return domain.ErrValidationRequired
		}
	}
	_, err := SQL(r.MetricID)
	return err
}

// Every query takes project, start, end and optional task_type in that order.
// Aggregates are calculated in PostgreSQL over authoritative, immutable facts.
func SQL(id string) (string, error) {
	switch id {
	case "pending_index_age_seconds":
		return `SELECT COALESCE(max(extract(epoch from now()-o.created_at)) FILTER(WHERE o.failed_at IS NULL),0)::float8,
 count(*) FILTER(WHERE o.failed_at IS NULL),count(*),count(*) FILTER(WHERE o.failed_at IS NOT NULL)
 FROM outbox_events o JOIN knowledge_items k ON k.id=o.aggregate_id
 WHERE k.project_id=$1 AND o.topic='knowledge.upsert' AND o.completed_at IS NULL AND knowledge_eligible(k.id)
 AND $2::timestamptz<$3::timestamptz AND $4::text=''`, nil
	case "candidate_validation_rate":
		return `WITH latest AS (
 SELECT DISTINCT ON(v.knowledge_id,v.candidate_version) v.verdict FROM knowledge_validations v
 WHERE v.project_id=$1 AND v.completed_at >= $2 AND v.completed_at < $3
 AND ($4::text='' OR EXISTS(SELECT 1 FROM workflow_task_checkpoints t WHERE t.candidate_id=v.knowledge_id AND t.task_type=$4))
 ORDER BY v.knowledge_id,v.candidate_version,v.completed_at DESC,v.id DESC)
 SELECT count(*) FILTER(WHERE verdict='pass')::float8/NULLIF(count(*),0),count(*) FILTER(WHERE verdict='pass'),count(*),count(*) FILTER(WHERE verdict='fail') FROM latest`, nil
	case "candidate_approval_rate":
		return `SELECT count(*) FILTER(WHERE d.decision='approve')::float8/NULLIF(count(*),0),count(*) FILTER(WHERE d.decision='approve'),count(*),count(*) FILTER(WHERE d.decision='reject')
 FROM knowledge_decisions d JOIN knowledge_items k ON k.id=d.knowledge_id
 WHERE k.project_id=$1 AND d.created_at >= $2 AND d.created_at < $3
 AND ($4::text='' OR EXISTS(SELECT 1 FROM workflow_task_checkpoints t WHERE t.candidate_id=k.id AND t.task_type=$4))`, nil
	case "validated_reuse_rate":
		return `WITH eligible AS (
 SELECT t.id,EXISTS(SELECT 1 FROM workflow_task_used_context u WHERE u.task_id=t.id) AS reused
 FROM workflow_task_checkpoints t JOIN workflow_runs w ON w.id=t.workflow_id
 WHERE w.project_id=$1 AND t.state='completed' AND t.completed_at >= $2 AND t.completed_at < $3
 AND ($4::text='' OR t.task_type=$4) AND EXISTS(SELECT 1 FROM workflow_task_local_validations l
 JOIN knowledge_validations v ON v.id=l.validation_id WHERE l.task_id=t.id AND v.knowledge_id=t.candidate_id AND v.candidate_version=l.candidate_version AND v.verdict='pass' AND v.method='workpacket'))
 SELECT count(*) FILTER(WHERE reused)::float8/NULLIF(count(*),0),count(*) FILTER(WHERE reused),count(*),0::bigint FROM eligible`, nil
	}
	return "", fmt.Errorf("%w: unknown reviewed metric", domain.ErrValidationRequired)
}
