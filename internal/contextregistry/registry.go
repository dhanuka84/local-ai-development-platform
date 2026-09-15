// Package contextregistry is a source-controlled, reviewed query allowlist.
// Descriptions and model output never become executable SQL.
package contextregistry

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/contracts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

// Load validates the embedded registry and hashes its definitions, SQL, and
// input contracts in registry order. It performs no database or network work.
func Load() ([]domain.ContextDefinition, string, error) {
	raw, err := contracts.Files.ReadFile("context/v1/registry.valid.json")
	if err != nil {
		return nil, "", err
	}
	if err = contracts.Validate("context/v1/registry.schema.json", raw); err != nil {
		return nil, "", err
	}
	var definitions []domain.ContextDefinition
	if err = json.Unmarshal(raw, &definitions); err != nil {
		return nil, "", err
	}
	seen := map[string]bool{}
	for _, definition := range definitions {
		if seen[definition.ID] {
			return nil, "", fmt.Errorf("duplicate definition")
		}
		seen[definition.ID] = true
		if definition.Kind == "metric" {
			if _, err = SQL(definition.ID); err != nil {
				return nil, "", err
			}
		}
	}
	canonical, err := json.Marshal(definitions)
	if err != nil {
		return nil, "", fmt.Errorf("encode context registry: %w", err)
	}
	// Bind executable meaning as well as prose; changing a fixed query requires
	// a new registry validation and versioned approval, not silent semantic drift.
	for _, definition := range definitions {
		if definition.Kind == "metric" {
			query, err := SQL(definition.ID)
			if err != nil {
				return nil, "", err
			}
			canonical = append(canonical, []byte("\n"+definition.ID+"\n"+query)...)
		}
		if definition.InputContract != "" {
			schema, err := contracts.Files.ReadFile(definition.InputContract)
			if err != nil {
				return nil, "", err
			}
			canonical = append(canonical, schema...)
		}
	}
	return definitions, domain.Digest(canonical), nil
}
func ValidateRequest(request domain.MetricRequest) error {
	if request.ProjectID == "" || request.Version != 1 || request.Start.IsZero() || !request.End.After(request.Start) || request.End.Sub(request.Start) > 366*24*time.Hour || request.End.After(time.Now().Add(time.Minute)) {
		return domain.ErrValidationRequired
	}
	for k, v := range request.Dimensions {
		if k != "task_type" || len(v) < 1 || len(v) > 64 || request.MetricID == "pending_index_age_seconds" {
			return domain.ErrValidationRequired
		}
	}
	_, err := SQL(request.MetricID)
	return err
}

// SQL returns a reviewed query for id, rejecting all unknown metric IDs.
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
