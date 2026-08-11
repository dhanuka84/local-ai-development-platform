package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

func graphCompositeID(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(digest[:])
}

func (r *Repository) LoadGraphProjectionSnapshot(ctx context.Context) (domain.GraphProjectionSnapshot, error) {
	var snapshot domain.GraphProjectionSnapshot
	repositoryRows, err := r.pool.Query(ctx, `SELECT id::text,project_id,name,canonical_url,default_branch,revision,created_at,updated_at
      FROM software_repositories ORDER BY project_id,name,id`)
	if err != nil {
		return snapshot, err
	}
	for repositoryRows.Next() {
		var repository domain.SoftwareRepository
		if err := repositoryRows.Scan(&repository.ID, &repository.ProjectID, &repository.Name, &repository.CanonicalURL,
			&repository.DefaultBranch, &repository.Revision, &repository.CreatedAt, &repository.UpdatedAt); err != nil {
			repositoryRows.Close()
			return snapshot, err
		}
		snapshot.Repositories = append(snapshot.Repositories, repository)
	}
	if err := repositoryRows.Err(); err != nil {
		repositoryRows.Close()
		return snapshot, err
	}
	repositoryRows.Close()

	relationRows, err := r.pool.Query(ctx, `SELECT `+relationColumns+`
      FROM repository_relations r
      JOIN software_repositories f ON f.id=r.from_repository_id
      JOIN software_repositories t ON t.id=r.to_repository_id
      ORDER BY r.project_id,r.id`)
	if err != nil {
		return snapshot, err
	}
	for relationRows.Next() {
		relation, err := scanRelation(relationRows)
		if err != nil {
			relationRows.Close()
			return snapshot, err
		}
		snapshot.RepositoryRelations = append(snapshot.RepositoryRelations, relation)
	}
	if err := relationRows.Err(); err != nil {
		relationRows.Close()
		return snapshot, err
	}
	relationRows.Close()

	headRows, err := r.pool.Query(ctx, `SELECT run.id::text
      FROM code_repository_heads head JOIN code_analysis_runs run ON run.id=head.analysis_run_id
      ORDER BY run.project_id,run.repository_id`)
	if err != nil {
		return snapshot, err
	}
	runIDs := make([]string, 0)
	for headRows.Next() {
		var runID string
		if err := headRows.Scan(&runID); err != nil {
			headRows.Close()
			return snapshot, err
		}
		runIDs = append(runIDs, runID)
	}
	if err := headRows.Err(); err != nil {
		headRows.Close()
		return snapshot, err
	}
	headRows.Close()
	for _, runID := range runIDs {
		graph, active, err := r.GetActiveCodeGraphForRun(ctx, runID)
		if err != nil {
			return snapshot, err
		}
		if active {
			snapshot.CodeGraphs = append(snapshot.CodeGraphs, graph)
		}
	}

	knowledgeRows, err := r.pool.Query(ctx, `SELECT `+knowledgeColumns+` FROM knowledge_items
      WHERE status='approved' ORDER BY project_id,id`)
	if err != nil {
		return snapshot, err
	}
	for knowledgeRows.Next() {
		item, err := scanKnowledge(knowledgeRows)
		if err != nil {
			knowledgeRows.Close()
			return snapshot, err
		}
		snapshot.Knowledge = append(snapshot.Knowledge, item)
	}
	if err := knowledgeRows.Err(); err != nil {
		knowledgeRows.Close()
		return snapshot, err
	}
	knowledgeRows.Close()

	knowledgeRelationRows, err := r.pool.Query(ctx, `SELECT source.project_id,relation.from_id::text,relation.to_id::text,
        relation.relation_type,relation.confidence
      FROM knowledge_relations relation
      JOIN knowledge_items source ON source.id=relation.from_id AND source.status='approved'
      JOIN knowledge_items target ON target.id=relation.to_id AND target.status='approved'
      WHERE target.project_id=source.project_id
      ORDER BY source.project_id,relation.from_id,relation.to_id,relation.relation_type`)
	if err != nil {
		return snapshot, err
	}
	for knowledgeRelationRows.Next() {
		var relation domain.KnowledgeRelation
		if err := knowledgeRelationRows.Scan(&relation.ProjectID, &relation.FromID, &relation.ToID,
			&relation.RelationType, &relation.Confidence); err != nil {
			knowledgeRelationRows.Close()
			return snapshot, err
		}
		relation.ID = graphCompositeID("knowledge_relation", relation.FromID, relation.ToID, relation.RelationType)
		snapshot.KnowledgeRelations = append(snapshot.KnowledgeRelations, relation)
	}
	if err := knowledgeRelationRows.Err(); err != nil {
		knowledgeRelationRows.Close()
		return snapshot, err
	}
	knowledgeRelationRows.Close()

	referenceRows, err := r.pool.Query(ctx, `SELECT knowledge.project_id,reference.knowledge_id::text,reference.entity_id::text,
        reference.analysis_run_id::text,reference.role,reference.evidence
      FROM knowledge_code_references reference
      JOIN knowledge_items knowledge ON knowledge.id=reference.knowledge_id AND knowledge.status='approved'
      JOIN code_entities entity ON entity.id=reference.entity_id AND entity.project_id=knowledge.project_id
      JOIN code_repository_heads head ON head.repository_id=entity.repository_id
                                     AND head.analysis_run_id=reference.analysis_run_id
      ORDER BY knowledge.project_id,reference.knowledge_id,reference.entity_id,reference.role`)
	if err != nil {
		return snapshot, err
	}
	for referenceRows.Next() {
		var reference domain.KnowledgeCodeReference
		if err := referenceRows.Scan(&reference.ProjectID, &reference.KnowledgeID, &reference.EntityID,
			&reference.AnalysisRunID, &reference.Role, &reference.Evidence); err != nil {
			referenceRows.Close()
			return snapshot, err
		}
		reference.ID = graphCompositeID("knowledge_code", reference.KnowledgeID, reference.EntityID, reference.AnalysisRunID, reference.Role)
		snapshot.KnowledgeCodeReferences = append(snapshot.KnowledgeCodeReferences, reference)
	}
	if err := referenceRows.Err(); err != nil {
		referenceRows.Close()
		return snapshot, err
	}
	referenceRows.Close()
	return snapshot, nil
}

func (r *Repository) GetKnowledgeProjection(ctx context.Context, id string) (domain.KnowledgeItem, []domain.KnowledgeRelation, []domain.KnowledgeCodeReference, bool, error) {
	item, err := r.GetKnowledge(ctx, id, false)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return item, nil, nil, false, nil
		}
		return item, nil, nil, false, err
	}
	snapshot, err := r.LoadGraphProjectionSnapshot(ctx)
	if err != nil {
		return item, nil, nil, false, err
	}
	relations := make([]domain.KnowledgeRelation, 0)
	for _, relation := range snapshot.KnowledgeRelations {
		if relation.FromID == id || relation.ToID == id {
			relations = append(relations, relation)
		}
	}
	references := make([]domain.KnowledgeCodeReference, 0)
	for _, reference := range snapshot.KnowledgeCodeReferences {
		if reference.KnowledgeID == id {
			references = append(references, reference)
		}
	}
	return item, relations, references, true, nil
}

func (r *Repository) GetKnowledgeReferencesForRepository(ctx context.Context, repositoryID string) ([]domain.KnowledgeCodeReference, error) {
	snapshot, err := r.LoadGraphProjectionSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	entityRepository := make(map[string]string)
	for _, graph := range snapshot.CodeGraphs {
		for _, entity := range graph.Entities {
			entityRepository[entity.ID] = entity.RepositoryID
		}
	}
	result := make([]domain.KnowledgeCodeReference, 0)
	for _, reference := range snapshot.KnowledgeCodeReferences {
		if entityRepository[reference.EntityID] == repositoryID {
			result = append(result, reference)
		}
	}
	return result, nil
}

func (r *Repository) KnowledgeProjectionsCurrent(ctx context.Context, projectID string, ids []string) (bool, error) {
	if len(ids) == 0 {
		return true, nil
	}
	var current bool
	err := r.pool.QueryRow(ctx, `SELECT count(*)=cardinality($2::text[])
        AND COALESCE(bool_and(item.status='approved' AND projected.version=item.version
                     AND projected.backend='apache-age' AND projected.status='ready'),false)
      FROM knowledge_items item
      LEFT JOIN graph_knowledge_projection_heads projected ON projected.knowledge_id=item.id
      WHERE item.project_id=$1 AND item.id::text=ANY($2)`, projectID, ids).Scan(&current)
	return current, err
}

func (r *Repository) GetSemanticGraphEdge(ctx context.Context, id string) (domain.SemanticGraphEdge, bool, error) {
	rows, err := r.GetSemanticGraphEdgesMany(ctx, []string{id})
	if err != nil {
		return domain.SemanticGraphEdge{}, false, err
	}
	if len(rows) == 0 {
		return domain.SemanticGraphEdge{}, false, nil
	}
	return rows[0], true, nil
}

func (r *Repository) GetSemanticGraphEdgesMany(ctx context.Context, ids []string) ([]domain.SemanticGraphEdge, error) {
	if len(ids) == 0 {
		return []domain.SemanticGraphEdge{}, nil
	}
	snapshot, err := r.LoadGraphProjectionSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	requested := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		requested[id] = struct{}{}
	}
	result := make([]domain.SemanticGraphEdge, 0, len(ids))
	for _, graph := range snapshot.CodeGraphs {
		labels := make(map[string]string, len(graph.Entities))
		for _, entity := range graph.Entities {
			labels[entity.ID] = entity.QualifiedName
		}
		for _, relation := range graph.Relations {
			if _, ok := requested[relation.ID]; !ok {
				continue
			}
			result = append(result, domain.SemanticGraphEdge{GraphEdge: domain.GraphEdge{
				ID: relation.ID, Type: relation.RelationType,
				SourceID: relation.SourceID, SourceType: domain.GraphNodeCodeEntity,
				TargetID: relation.TargetID, TargetType: domain.GraphNodeCodeEntity,
				Evidence: relation.Evidence, Confidence: relation.Confidence,
			}, ProjectID: graph.Analysis.ProjectID, RepositoryID: graph.Analysis.Repository.ID,
				SourceLabel: labels[relation.SourceID], TargetLabel: labels[relation.TargetID]})
		}
	}
	knowledgeLabels := make(map[string]string, len(snapshot.Knowledge))
	for _, item := range snapshot.Knowledge {
		knowledgeLabels[item.ID] = item.Title
	}
	codeLabels := make(map[string]string)
	for _, graph := range snapshot.CodeGraphs {
		for _, entity := range graph.Entities {
			codeLabels[entity.ID] = entity.QualifiedName
		}
	}
	for _, relation := range snapshot.KnowledgeRelations {
		if _, ok := requested[relation.ID]; !ok {
			continue
		}
		result = append(result, domain.SemanticGraphEdge{GraphEdge: domain.GraphEdge{
			ID: relation.ID, Type: relation.RelationType,
			SourceID: relation.FromID, SourceType: domain.GraphNodeKnowledgeItem,
			TargetID: relation.ToID, TargetType: domain.GraphNodeKnowledgeItem,
			Confidence: relation.Confidence,
		}, ProjectID: relation.ProjectID, SourceLabel: knowledgeLabels[relation.FromID], TargetLabel: knowledgeLabels[relation.ToID]})
	}
	for _, reference := range snapshot.KnowledgeCodeReferences {
		if _, ok := requested[reference.ID]; !ok {
			continue
		}
		result = append(result, domain.SemanticGraphEdge{GraphEdge: domain.GraphEdge{
			ID: reference.ID, Type: reference.Role,
			SourceID: reference.KnowledgeID, SourceType: domain.GraphNodeKnowledgeItem,
			TargetID: reference.EntityID, TargetType: domain.GraphNodeCodeEntity,
			Evidence: reference.Evidence, Confidence: 1,
		}, ProjectID: reference.ProjectID, SourceLabel: knowledgeLabels[reference.KnowledgeID], TargetLabel: codeLabels[reference.EntityID]})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (r *Repository) GetSemanticGraphEdgesForKnowledge(ctx context.Context, knowledgeID string) ([]domain.SemanticGraphEdge, error) {
	snapshot, err := r.LoadGraphProjectionSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for _, relation := range snapshot.KnowledgeRelations {
		if relation.FromID == knowledgeID || relation.ToID == knowledgeID {
			ids = append(ids, relation.ID)
		}
	}
	for _, reference := range snapshot.KnowledgeCodeReferences {
		if reference.KnowledgeID == knowledgeID {
			ids = append(ids, reference.ID)
		}
	}
	return r.GetSemanticGraphEdgesMany(ctx, ids)
}

func (r *Repository) RequeueSemanticGraphEdges(ctx context.Context) (int64, error) {
	result, err := r.pool.Exec(ctx, `INSERT INTO outbox_events(aggregate_id,topic)
      SELECT relation.id,'code_relation.upsert'
      FROM code_relations relation
      JOIN code_repository_heads head ON head.analysis_run_id=relation.analysis_run_id
      WHERE relation.relation_type IN ('calls','references','implements','imports','tests')`)
	return result.RowsAffected(), err
}
