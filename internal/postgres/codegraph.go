package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dhanuka84/hybrid-ai-platform/components/codegraph"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
)

const codeEntityColumns = `e.id::text,e.project_id,e.repository_id::text,repository.name,o.analysis_run_id::text,run.branch,run.revision,
    e.stable_key,e.language,e.kind,e.name,e.qualified_name,o.signature,COALESCE(o.content_hash::text,''),
    o.file_path,o.start_line,o.start_column,o.end_line,o.end_column,e.metadata`

type scanner interface {
	Scan(...any) error
}

func scanCodeEntity(row scanner) (domain.CodeEntity, error) {
	var entity domain.CodeEntity
	var metadata []byte
	err := row.Scan(
		&entity.ID, &entity.ProjectID, &entity.RepositoryID, &entity.RepositoryName,
		&entity.AnalysisRunID, &entity.Branch, &entity.Revision,
		&entity.StableKey, &entity.Language, &entity.Kind, &entity.Name, &entity.QualifiedName,
		&entity.Signature, &entity.ContentHash, &entity.Location.FilePath, &entity.Location.StartLine,
		&entity.Location.StartColumn, &entity.Location.EndLine, &entity.Location.EndColumn, &metadata,
	)
	if err == nil && len(metadata) > 0 {
		err = json.Unmarshal(metadata, &entity.Metadata)
	}
	return entity, err
}

func (r *Repository) StoreCodeGraph(
	ctx context.Context,
	projectID string,
	repository domain.SoftwareRepository,
	requestedBy string,
	snapshot codegraph.Snapshot,
) (domain.CodeAnalysis, error) {
	if err := snapshot.Normalize(); err != nil {
		return domain.CodeAnalysis{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.CodeAnalysis{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `INSERT INTO projects(id,display_name) VALUES($1,$1) ON CONFLICT (id) DO NOTHING`, projectID); err != nil {
		return domain.CodeAnalysis{}, fmt.Errorf("ensure project: %w", err)
	}
	repository.ProjectID = projectID
	repository.Revision = snapshot.Revision
	repositoryID, err := upsertSoftwareRepository(ctx, tx, projectID, repository)
	if err != nil {
		return domain.CodeAnalysis{}, err
	}
	statistics, err := json.Marshal(snapshot.Statistics)
	if err != nil {
		return domain.CodeAnalysis{}, fmt.Errorf("encode code graph statistics: %w", err)
	}
	var runID string
	err = tx.QueryRow(ctx, `INSERT INTO code_analysis_runs(
        project_id,repository_id,branch,revision,analyzer,analyzer_version,requested_by,statistics,started_at,completed_at
      ) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id::text`,
		projectID, repositoryID, snapshot.Branch, snapshot.Revision, snapshot.Analyzer, snapshot.AnalyzerVersion,
		requestedBy, statistics, snapshot.StartedAt, snapshot.CompletedAt,
	).Scan(&runID)
	if err != nil {
		return domain.CodeAnalysis{}, fmt.Errorf("create code analysis run: %w", err)
	}

	entityIDs := make(map[string]string, len(snapshot.Entities))
	for _, entity := range snapshot.Entities {
		metadata, err := json.Marshal(entity.Metadata)
		if err != nil {
			return domain.CodeAnalysis{}, fmt.Errorf("encode entity metadata: %w", err)
		}
		var entityID string
		err = tx.QueryRow(ctx, `INSERT INTO code_entities(
            project_id,repository_id,stable_key,language,kind,name,qualified_name,metadata
          ) VALUES($1,$2,$3,$4,$5,$6,$7,$8)
          ON CONFLICT (repository_id,stable_key) DO UPDATE SET
            language=EXCLUDED.language,kind=EXCLUDED.kind,name=EXCLUDED.name,
            qualified_name=EXCLUDED.qualified_name,metadata=EXCLUDED.metadata,updated_at=now()
          RETURNING id::text`, projectID, repositoryID, entity.Key, entity.Language, string(entity.Kind),
			entity.Name, entity.QualifiedName, metadata).Scan(&entityID)
		if err != nil {
			return domain.CodeAnalysis{}, fmt.Errorf("upsert code entity %q: %w", entity.Key, err)
		}
		entityIDs[entity.Key] = entityID
		if _, err := tx.Exec(ctx, `INSERT INTO code_occurrences(
            analysis_run_id,entity_id,file_path,start_line,start_column,end_line,end_column,signature,content_hash,metadata
          ) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10)`,
			runID, entityID, entity.Location.FilePath, entity.Location.Start.Line, entity.Location.Start.Column,
			entity.Location.End.Line, entity.Location.End.Column, entity.Signature, entity.ContentHash, metadata); err != nil {
			return domain.CodeAnalysis{}, fmt.Errorf("insert occurrence %q: %w", entity.Key, err)
		}
	}

	for _, relation := range snapshot.Relations {
		sourceID, sourceOK := entityIDs[relation.SourceKey]
		targetID, targetOK := entityIDs[relation.TargetKey]
		if !sourceOK || !targetOK {
			return domain.CodeAnalysis{}, fmt.Errorf("relation endpoints missing for %s -> %s", relation.SourceKey, relation.TargetKey)
		}
		metadata, err := json.Marshal(relation.Metadata)
		if err != nil {
			return domain.CodeAnalysis{}, fmt.Errorf("encode relation metadata: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO code_relations(
            analysis_run_id,source_entity_id,target_entity_id,relation_type,evidence,confidence,
            file_path,start_line,start_column,end_line,end_column,metadata
          ) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			runID, sourceID, targetID, string(relation.Kind), relation.Evidence, relation.Confidence,
			relation.Location.FilePath, relation.Location.Start.Line, relation.Location.Start.Column,
			relation.Location.End.Line, relation.Location.End.Column, metadata); err != nil {
			return domain.CodeAnalysis{}, fmt.Errorf("insert code relation: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `INSERT INTO code_repository_heads(repository_id,analysis_run_id)
        VALUES($1,$2) ON CONFLICT (repository_id) DO UPDATE SET analysis_run_id=EXCLUDED.analysis_run_id,updated_at=now()`,
		repositoryID, runID); err != nil {
		return domain.CodeAnalysis{}, fmt.Errorf("advance code graph head: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO outbox_events(aggregate_id,topic) VALUES($1,'code_graph.project')`, runID); err != nil {
		return domain.CodeAnalysis{}, fmt.Errorf("queue code graph projection: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO outbox_events(aggregate_id,topic)
        SELECT e.id,'code_entity.upsert'
        FROM code_entities e JOIN code_occurrences o ON o.entity_id=e.id
        WHERE o.analysis_run_id=$1
          AND e.kind IN ('type','interface','function','method','test')
          AND COALESCE(e.metadata->>'external','false') <> 'true'`, runID); err != nil {
		return domain.CodeAnalysis{}, fmt.Errorf("queue code entity indexing: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO outbox_events(aggregate_id,topic)
        SELECT relation.id,'code_relation.upsert'
        FROM code_relations relation
        WHERE relation.analysis_run_id=$1
          AND relation.relation_type IN ('calls','references','implements','imports','tests')`, runID); err != nil {
		return domain.CodeAnalysis{}, fmt.Errorf("queue code relation indexing: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.CodeAnalysis{}, err
	}

	repository.ID = repositoryID
	repository.ProjectID = projectID
	repository.Revision = snapshot.Revision
	return domain.CodeAnalysis{
		ID: runID, ProjectID: projectID, Repository: repository, Branch: snapshot.Branch, Revision: snapshot.Revision,
		Analyzer: snapshot.Analyzer, AnalyzerVersion: snapshot.AnalyzerVersion, RequestedBy: requestedBy,
		EntityCount: len(snapshot.Entities), RelationCount: len(snapshot.Relations),
		StartedAt: snapshot.StartedAt, CompletedAt: snapshot.CompletedAt,
	}, nil
}

func (r *Repository) GetCodeEntity(ctx context.Context, id string) (domain.CodeEntity, error) {
	entity, err := scanCodeEntity(r.pool.QueryRow(ctx, `SELECT `+codeEntityColumns+`
      FROM code_entities e
      JOIN code_repository_heads h ON h.repository_id=e.repository_id
      JOIN code_occurrences o ON o.entity_id=e.id AND o.analysis_run_id=h.analysis_run_id
      JOIN code_analysis_runs run ON run.id=o.analysis_run_id
	  JOIN software_repositories repository ON repository.id=e.repository_id
      WHERE e.id::text=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity, fmt.Errorf("active code entity %q not found", id)
	}
	return entity, err
}

func (r *Repository) GetCodeEntitiesMany(ctx context.Context, ids []string) ([]domain.CodeEntity, error) {
	if len(ids) == 0 {
		return []domain.CodeEntity{}, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT `+codeEntityColumns+`
      FROM code_entities e
      JOIN code_repository_heads h ON h.repository_id=e.repository_id
      JOIN code_occurrences o ON o.entity_id=e.id AND o.analysis_run_id=h.analysis_run_id
      JOIN code_analysis_runs run ON run.id=o.analysis_run_id
	  JOIN software_repositories repository ON repository.id=e.repository_id
      WHERE e.id::text=ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.CodeEntity, 0, len(ids))
	for rows.Next() {
		entity, err := scanCodeEntity(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, entity)
	}
	return result, rows.Err()
}

func (r *Repository) SearchCodeEntitiesLexical(ctx context.Context, projectID, repositoryRoot, query string, limit int) ([]domain.CodeEntity, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+codeEntityColumns+`,
        ts_rank_cd(e.search_document,websearch_to_tsquery('simple',$3)) AS score
      FROM code_entities e
      JOIN software_repositories repository ON repository.id=e.repository_id
      JOIN code_repository_heads h ON h.repository_id=e.repository_id
      JOIN code_occurrences o ON o.entity_id=e.id AND o.analysis_run_id=h.analysis_run_id
      JOIN code_analysis_runs run ON run.id=o.analysis_run_id
      WHERE e.project_id=$1
        AND ($2='' OR repository.id::text=$2 OR repository.canonical_url=$2 OR repository.name=$2)
        AND (e.search_document @@ websearch_to_tsquery('simple',$3)
             OR e.name ILIKE '%' || $3 || '%' OR e.qualified_name ILIKE '%' || $3 || '%')
      ORDER BY score DESC,e.qualified_name LIMIT $4`, projectID, repositoryRoot, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.CodeEntity, 0, limit)
	for rows.Next() {
		entity, err := scanCodeEntityWithScore(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, entity)
	}
	return result, rows.Err()
}

func scanCodeEntityWithScore(row scanner) (domain.CodeEntity, error) {
	var entity domain.CodeEntity
	var metadata []byte
	err := row.Scan(
		&entity.ID, &entity.ProjectID, &entity.RepositoryID, &entity.RepositoryName,
		&entity.AnalysisRunID, &entity.Branch, &entity.Revision,
		&entity.StableKey, &entity.Language, &entity.Kind, &entity.Name, &entity.QualifiedName,
		&entity.Signature, &entity.ContentHash, &entity.Location.FilePath, &entity.Location.StartLine,
		&entity.Location.StartColumn, &entity.Location.EndLine, &entity.Location.EndColumn, &metadata, &entity.Score,
	)
	if err == nil && len(metadata) > 0 {
		err = json.Unmarshal(metadata, &entity.Metadata)
	}
	return entity, err
}

func (r *Repository) GetCodeGraph(ctx context.Context, projectID, repositoryRoot, symbolRoot string, depth int) (domain.CodeGraph, error) {
	analysis, err := r.getActiveCodeAnalysis(ctx, projectID, repositoryRoot)
	if err != nil {
		return domain.CodeGraph{}, err
	}
	rows, err := r.pool.Query(ctx, `WITH RECURSIVE walk(entity_id,path,depth) AS (
        SELECT e.id,ARRAY[e.id],0
        FROM code_entities e JOIN code_occurrences o ON o.entity_id=e.id
        WHERE o.analysis_run_id=$1
          AND (e.id::text=$2 OR e.stable_key=$2 OR e.qualified_name=$2 OR e.name=$2)
        UNION ALL
        SELECT next_entity.id,walk.path || next_entity.id,walk.depth+1
        FROM walk
        JOIN code_relations edge ON edge.analysis_run_id=$1
          AND (edge.source_entity_id=walk.entity_id OR edge.target_entity_id=walk.entity_id)
        JOIN code_entities next_entity ON next_entity.id=CASE
          WHEN edge.source_entity_id=walk.entity_id THEN edge.target_entity_id ELSE edge.source_entity_id END
        WHERE walk.depth < $3 AND NOT next_entity.id=ANY(walk.path)
      )
      SELECT `+codeEntityColumns+`
      FROM code_entities e
      JOIN code_occurrences o ON o.entity_id=e.id AND o.analysis_run_id=$1
      JOIN code_analysis_runs run ON run.id=o.analysis_run_id
	  JOIN software_repositories repository ON repository.id=e.repository_id
      WHERE e.id IN (SELECT DISTINCT entity_id FROM walk)
      ORDER BY e.qualified_name`, analysis.ID, symbolRoot, depth)
	if err != nil {
		return domain.CodeGraph{}, err
	}
	entities := make([]domain.CodeEntity, 0)
	entityIDs := make([]string, 0)
	for rows.Next() {
		entity, err := scanCodeEntity(rows)
		if err != nil {
			rows.Close()
			return domain.CodeGraph{}, err
		}
		entities = append(entities, entity)
		entityIDs = append(entityIDs, entity.ID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return domain.CodeGraph{}, err
	}
	rows.Close()
	if len(entities) == 0 {
		return domain.CodeGraph{}, fmt.Errorf("code symbol %q not found in active repository graph", symbolRoot)
	}

	relationRows, err := r.pool.Query(ctx, `SELECT id::text,analysis_run_id::text,source_entity_id::text,target_entity_id::text,
        relation_type,evidence,confidence,file_path,start_line,start_column,end_line,end_column,metadata
      FROM code_relations WHERE analysis_run_id=$1
        AND source_entity_id::text=ANY($2) AND target_entity_id::text=ANY($2)
      ORDER BY relation_type,id`, analysis.ID, entityIDs)
	if err != nil {
		return domain.CodeGraph{}, err
	}
	defer relationRows.Close()
	relations := make([]domain.CodeRelation, 0)
	for relationRows.Next() {
		relation, err := scanCodeRelation(relationRows)
		if err != nil {
			return domain.CodeGraph{}, err
		}
		relations = append(relations, relation)
	}
	return domain.CodeGraph{Analysis: analysis, Entities: entities, Relations: relations}, relationRows.Err()
}

func scanCodeRelation(row scanner) (domain.CodeRelation, error) {
	var relation domain.CodeRelation
	var metadata []byte
	err := row.Scan(
		&relation.ID, &relation.AnalysisRunID, &relation.SourceID, &relation.TargetID,
		&relation.RelationType, &relation.Evidence, &relation.Confidence, &relation.Location.FilePath,
		&relation.Location.StartLine, &relation.Location.StartColumn, &relation.Location.EndLine,
		&relation.Location.EndColumn, &metadata,
	)
	if err == nil && len(metadata) > 0 {
		err = json.Unmarshal(metadata, &relation.Metadata)
	}
	return relation, err
}

func (r *Repository) getActiveCodeAnalysis(ctx context.Context, projectID, root string) (domain.CodeAnalysis, error) {
	var analysis domain.CodeAnalysis
	var statistics []byte
	err := r.pool.QueryRow(ctx, `SELECT run.id::text,run.project_id,repository.id::text,repository.project_id,
        repository.name,repository.canonical_url,repository.default_branch,repository.revision,
		repository.created_at,repository.updated_at,run.branch,run.revision,run.analyzer,run.analyzer_version,
        run.requested_by,run.statistics,run.started_at,run.completed_at
      FROM software_repositories repository
      JOIN code_repository_heads head ON head.repository_id=repository.id
      JOIN code_analysis_runs run ON run.id=head.analysis_run_id
      WHERE repository.project_id=$1
        AND (repository.id::text=$2 OR repository.canonical_url=$2 OR repository.name=$2)`, projectID, root).Scan(
		&analysis.ID, &analysis.ProjectID, &analysis.Repository.ID, &analysis.Repository.ProjectID,
		&analysis.Repository.Name, &analysis.Repository.CanonicalURL, &analysis.Repository.DefaultBranch,
		&analysis.Repository.Revision, &analysis.Repository.CreatedAt, &analysis.Repository.UpdatedAt,
		&analysis.Branch, &analysis.Revision, &analysis.Analyzer, &analysis.AnalyzerVersion, &analysis.RequestedBy,
		&statistics, &analysis.StartedAt, &analysis.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return analysis, fmt.Errorf("active code graph for repository %q not found", root)
	}
	if err != nil {
		return analysis, err
	}
	var counts map[string]int
	if err := json.Unmarshal(statistics, &counts); err == nil {
		analysis.EntityCount = counts["entities"]
		analysis.RelationCount = counts["relations"]
	}
	return analysis, nil
}

func (r *Repository) RequeueCodeEntities(ctx context.Context) (int64, error) {
	result, err := r.pool.Exec(ctx, `INSERT INTO outbox_events(aggregate_id,topic)
      SELECT e.id,'code_entity.upsert'
      FROM code_entities e
      JOIN code_repository_heads h ON h.repository_id=e.repository_id
      JOIN code_occurrences o ON o.entity_id=e.id AND o.analysis_run_id=h.analysis_run_id
      WHERE e.kind IN ('type','interface','function','method','test')
        AND COALESCE(e.metadata->>'external','false') <> 'true'`)
	return result.RowsAffected(), err
}

// CompactCodeEntityOutbox completes superseded pending embedding events while
// retaining the newest event for every entity in an active repository graph.
// Completed events remain auditable and make reindex can recreate work later.
func (r *Repository) CompactCodeEntityOutbox(ctx context.Context) (int64, error) {
	result, err := r.pool.Exec(ctx, `WITH active_entities AS (
        SELECT DISTINCT occurrence.entity_id
        FROM code_occurrences occurrence
        JOIN code_repository_heads head ON head.analysis_run_id=occurrence.analysis_run_id
      ), keepers AS (
        SELECT max(event.id) AS id
        FROM outbox_events event
        JOIN active_entities active ON active.entity_id=event.aggregate_id
        WHERE event.topic='code_entity.upsert' AND event.completed_at IS NULL
        GROUP BY event.aggregate_id
      )
      UPDATE outbox_events event
      SET completed_at=now(),locked_at=NULL,
          last_error='superseded by active code snapshot'
      WHERE event.topic='code_entity.upsert' AND event.completed_at IS NULL
        AND event.locked_at IS NULL
        AND NOT EXISTS (SELECT 1 FROM keepers WHERE keepers.id=event.id)`)
	return result.RowsAffected(), err
}

func (r *Repository) GetActiveCodeAnalysis(ctx context.Context, projectID, root string) (domain.CodeAnalysis, error) {
	return r.getActiveCodeAnalysis(ctx, projectID, root)
}

func (r *Repository) ResolveActiveCodeEntity(ctx context.Context, analysisID, symbolRoot string) (domain.CodeEntity, error) {
	entity, err := scanCodeEntity(r.pool.QueryRow(ctx, `SELECT `+codeEntityColumns+`
      FROM code_entities e
      JOIN code_occurrences o ON o.entity_id=e.id AND o.analysis_run_id=$1
      JOIN code_analysis_runs run ON run.id=o.analysis_run_id
      JOIN software_repositories repository ON repository.id=e.repository_id
      WHERE (e.id::text=$2 OR e.stable_key=$2 OR e.qualified_name=$2 OR e.name=$2)
      ORDER BY CASE WHEN e.id::text=$2 THEN 0 WHEN e.stable_key=$2 THEN 1 WHEN e.qualified_name=$2 THEN 2 ELSE 3 END
      LIMIT 1`, analysisID, symbolRoot))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity, fmt.Errorf("code symbol %q not found in active repository graph", symbolRoot)
	}
	return entity, err
}

func (r *Repository) HydrateCodeGraph(ctx context.Context, analysis domain.CodeAnalysis, ids []string) (domain.CodeGraph, error) {
	if len(ids) == 0 {
		return domain.CodeGraph{}, errors.New("cannot hydrate an empty code graph")
	}
	rows, err := r.pool.Query(ctx, `SELECT `+codeEntityColumns+`
      FROM code_entities e
      JOIN code_occurrences o ON o.entity_id=e.id AND o.analysis_run_id=$1
      JOIN code_analysis_runs run ON run.id=o.analysis_run_id
      JOIN software_repositories repository ON repository.id=e.repository_id
      JOIN code_repository_heads head ON head.repository_id=e.repository_id AND head.analysis_run_id=o.analysis_run_id
      WHERE e.id::text=ANY($2) ORDER BY e.qualified_name`, analysis.ID, ids)
	if err != nil {
		return domain.CodeGraph{}, err
	}
	entities := make([]domain.CodeEntity, 0, len(ids))
	entityIDs := make([]string, 0, len(ids))
	for rows.Next() {
		entity, err := scanCodeEntity(rows)
		if err != nil {
			rows.Close()
			return domain.CodeGraph{}, err
		}
		entities = append(entities, entity)
		entityIDs = append(entityIDs, entity.ID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return domain.CodeGraph{}, err
	}
	rows.Close()
	if len(entities) == 0 {
		return domain.CodeGraph{}, errors.New("active code graph projection returned no authoritative entities")
	}
	relationRows, err := r.pool.Query(ctx, `SELECT id::text,analysis_run_id::text,source_entity_id::text,target_entity_id::text,
        relation_type,evidence,confidence,file_path,start_line,start_column,end_line,end_column,metadata
      FROM code_relations WHERE analysis_run_id=$1
        AND source_entity_id::text=ANY($2) AND target_entity_id::text=ANY($2)
      ORDER BY relation_type,id`, analysis.ID, entityIDs)
	if err != nil {
		return domain.CodeGraph{}, err
	}
	defer relationRows.Close()
	relations := make([]domain.CodeRelation, 0)
	for relationRows.Next() {
		relation, err := scanCodeRelation(relationRows)
		if err != nil {
			return domain.CodeGraph{}, err
		}
		relations = append(relations, relation)
	}
	return domain.CodeGraph{Analysis: analysis, Entities: entities, Relations: relations}, relationRows.Err()
}

func (r *Repository) GetActiveCodeGraphForRun(ctx context.Context, runID string) (domain.CodeGraph, bool, error) {
	var projectID, repositoryID string
	err := r.pool.QueryRow(ctx, `SELECT run.project_id,run.repository_id::text
      FROM code_analysis_runs run
      JOIN code_repository_heads head ON head.repository_id=run.repository_id AND head.analysis_run_id=run.id
      WHERE run.id::text=$1`, runID).Scan(&projectID, &repositoryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CodeGraph{}, false, nil
	}
	if err != nil {
		return domain.CodeGraph{}, false, err
	}
	analysis, err := r.getActiveCodeAnalysis(ctx, projectID, repositoryID)
	if err != nil {
		return domain.CodeGraph{}, false, err
	}
	rows, err := r.pool.Query(ctx, `SELECT entity_id::text FROM code_occurrences WHERE analysis_run_id=$1`, runID)
	if err != nil {
		return domain.CodeGraph{}, false, err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return domain.CodeGraph{}, false, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return domain.CodeGraph{}, false, err
	}
	rows.Close()
	graph, err := r.HydrateCodeGraph(ctx, analysis, ids)
	return graph, true, err
}

func (r *Repository) CodeProjectionCurrent(ctx context.Context, repositoryID, analysisID, revision string) (bool, error) {
	var current bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(
        SELECT 1 FROM code_repository_heads active
        JOIN graph_projection_heads projected ON projected.repository_id=active.repository_id
        WHERE active.repository_id::text=$1 AND active.analysis_run_id::text=$2
          AND projected.analysis_run_id=active.analysis_run_id AND projected.revision=$3
          AND projected.backend='apache-age' AND projected.status='ready'
      )`, repositoryID, analysisID, revision).Scan(&current)
	return current, err
}

func (r *Repository) CodeProjectionsCurrentForEntities(ctx context.Context, projectID string, entityIDs []string) (bool, error) {
	if len(entityIDs) == 0 {
		return true, nil
	}
	var current bool
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(bool_and(projected.analysis_run_id=active.analysis_run_id
        AND projected.revision=run.revision AND projected.backend='apache-age' AND projected.status='ready'),false)
      FROM code_entities entity
      JOIN code_repository_heads active ON active.repository_id=entity.repository_id
      JOIN code_analysis_runs run ON run.id=active.analysis_run_id
      LEFT JOIN graph_projection_heads projected ON projected.repository_id=entity.repository_id
      WHERE entity.project_id=$1 AND entity.id::text=ANY($2)`, projectID, entityIDs).Scan(&current)
	return current, err
}
