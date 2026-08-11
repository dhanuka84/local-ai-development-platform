package age

import (
	"context"
	"fmt"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/jackc/pgx/v5"
)

type RebuildStats struct {
	Repositories        int `json:"repositories"`
	RepositoryRelations int `json:"repository_relations"`
	CodeEntities        int `json:"code_entities"`
	CodeRelations       int `json:"code_relations"`
	KnowledgeItems      int `json:"knowledge_items"`
	KnowledgeEdges      int `json:"knowledge_edges"`
}

var repositoryEdgeLabels = map[string]string{
	"depends_on": "DEPENDS_ON", "provides_api_to": "PROVIDES_API_TO", "deploys_with": "DEPLOYS_WITH",
	"shares_contract": "SHARES_CONTRACT", "fork_of": "FORK_OF", "upstream_of": "UPSTREAM_OF",
	"successor_of": "SUCCESSOR_OF", "contains": "CONTAINS", "related_to": "RELATED_TO",
}

var codeEdgeLabels = map[string]string{
	"contains": "CONTAINS", "defines": "DEFINES", "imports": "IMPORTS", "calls": "CALLS",
	"references": "REFERENCES", "implements": "IMPLEMENTS", "embeds": "EMBEDS", "tests": "TESTS",
}

var knowledgeCodeEdgeLabels = map[string]string{
	"used_context": "USED_CONTEXT", "applies_to": "APPLIES_TO", "modifies": "MODIFIES",
	"validates": "VALIDATES", "review_concern": "REVIEW_CONCERN",
}

func (s *Store) execCypher(ctx context.Context, tx pgx.Tx, cypher string, parameters map[string]any) error {
	payload, err := marshalParameters(parameters)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, s.cypherSQL(cypher, `result ag_catalog.agtype`), payload); err != nil {
		return err
	}
	return nil
}

func (s *Store) mergeRepository(ctx context.Context, tx pgx.Tx, repository domain.SoftwareRepository) error {
	return s.execCypher(ctx, tx, `
MERGE (repository:Repository {id: $id})
SET repository.node_type = 'repository', repository.project_id = $project_id,
    repository.name = $name, repository.canonical_url = $canonical_url,
    repository.default_branch = $default_branch, repository.revision = $revision
RETURN repository`, map[string]any{
		"id": repository.ID, "project_id": repository.ProjectID, "name": repository.Name,
		"canonical_url": repository.CanonicalURL, "default_branch": repository.DefaultBranch,
		"revision": repository.Revision,
	})
}

func (s *Store) mergeRepositoryRelation(ctx context.Context, tx pgx.Tx, relation domain.RepositoryRelation) error {
	label, ok := repositoryEdgeLabels[relation.RelationType]
	if !ok {
		return fmt.Errorf("unsupported repository AGE edge %q", relation.RelationType)
	}
	if err := s.mergeRepository(ctx, tx, relation.From); err != nil {
		return err
	}
	if err := s.mergeRepository(ctx, tx, relation.To); err != nil {
		return err
	}
	cypher := fmt.Sprintf(`
MATCH (source:Repository {id: $source_id}), (target:Repository {id: $target_id})
MERGE (source)-[edge:%s {id: $id}]->(target)
SET edge.project_id = $project_id, edge.relation_type = $relation_type
RETURN edge`, label)
	return s.execCypher(ctx, tx, cypher, map[string]any{
		"id": relation.ID, "project_id": relation.ProjectID, "relation_type": relation.RelationType,
		"source_id": relation.From.ID, "target_id": relation.To.ID,
	})
}

func (s *Store) markRepositoryHeads(ctx context.Context, tx pgx.Tx, repositories ...domain.SoftwareRepository) error {
	for _, repository := range repositories {
		if _, err := tx.Exec(ctx, `INSERT INTO graph_repository_projection_heads(
            repository_id,revision,backend,source_updated_at,projected_at,status,last_error
          ) VALUES($1,$2,'apache-age',$3,now(),'ready','')
          ON CONFLICT (repository_id) DO UPDATE SET revision=EXCLUDED.revision,backend=EXCLUDED.backend,
            source_updated_at=EXCLUDED.source_updated_at,projected_at=now(),status='ready',last_error=''`,
			repository.ID, repository.Revision, repository.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) markRepositoryRelation(ctx context.Context, tx pgx.Tx, relation domain.RepositoryRelation) error {
	_, err := tx.Exec(ctx, `INSERT INTO graph_projection_relations(
      relation_id,backend,source_updated_at,projected_at,status,last_error
    ) VALUES($1,'apache-age',$2,now(),'ready','')
    ON CONFLICT (relation_id) DO UPDATE SET backend=EXCLUDED.backend,source_updated_at=EXCLUDED.source_updated_at,
      projected_at=now(),status='ready',last_error=''`, relation.ID, relation.UpdatedAt)
	return err
}

func (s *Store) ProjectRepositoryRelation(ctx context.Context, relation domain.RepositoryRelation) error {
	return s.withAGE(ctx, func(tx pgx.Tx) error {
		if err := s.mergeRepositoryRelation(ctx, tx, relation); err != nil {
			return fmt.Errorf("project repository relation %s: %w", relation.ID, err)
		}
		if err := s.markRepositoryHeads(ctx, tx, relation.From, relation.To); err != nil {
			return err
		}
		return s.markRepositoryRelation(ctx, tx, relation)
	})
}

func (s *Store) mergeCodeEntity(ctx context.Context, tx pgx.Tx, entity domain.CodeEntity) error {
	return s.execCypher(ctx, tx, `
MERGE (entity:CodeEntity {id: $id})
SET entity.node_type = 'code_entity', entity.project_id = $project_id,
    entity.repository_id = $repository_id, entity.analysis_run_id = $analysis_run_id,
    entity.branch = $branch, entity.revision = $revision,
    entity.stable_key = $stable_key, entity.qualified_name = $qualified_name,
    entity.kind = $kind, entity.language = $language
RETURN entity`, map[string]any{
		"id": entity.ID, "project_id": entity.ProjectID, "repository_id": entity.RepositoryID,
		"analysis_run_id": entity.AnalysisRunID, "branch": entity.Branch, "revision": entity.Revision,
		"stable_key":     entity.StableKey,
		"qualified_name": entity.QualifiedName, "kind": entity.Kind, "language": entity.Language,
	})
}

func (s *Store) mergeRepositoryContainsCode(ctx context.Context, tx pgx.Tx, repositoryID, entityID, projectID string) error {
	return s.execCypher(ctx, tx, `
MATCH (repository:Repository {id: $repository_id}), (entity:CodeEntity {id: $entity_id})
MERGE (repository)-[edge:CONTAINS {id: $edge_id}]->(entity)
SET edge.project_id = $project_id, edge.relation_type = 'contains'
RETURN edge`, map[string]any{
		"repository_id": repositoryID, "entity_id": entityID, "project_id": projectID,
		"edge_id": "repo-code:" + repositoryID + ":" + entityID,
	})
}

func (s *Store) mergeCodeRelation(ctx context.Context, tx pgx.Tx, relation domain.CodeRelation, projectID string) error {
	label, ok := codeEdgeLabels[relation.RelationType]
	if !ok {
		return fmt.Errorf("unsupported code AGE edge %q", relation.RelationType)
	}
	cypher := fmt.Sprintf(`
MATCH (source:CodeEntity {id: $source_id}), (target:CodeEntity {id: $target_id})
MERGE (source)-[edge:%s {id: $id}]->(target)
SET edge.project_id = $project_id, edge.relation_type = $relation_type,
    edge.analysis_run_id = $analysis_run_id
RETURN edge`, label)
	return s.execCypher(ctx, tx, cypher, map[string]any{
		"id": relation.ID, "project_id": projectID, "relation_type": relation.RelationType,
		"analysis_run_id": relation.AnalysisRunID, "source_id": relation.SourceID, "target_id": relation.TargetID,
	})
}

func (s *Store) projectCodeGraph(ctx context.Context, tx pgx.Tx, graph domain.CodeGraph, references []domain.KnowledgeCodeReference, replace bool) error {
	repository := graph.Analysis.Repository
	if replace {
		if err := s.execCypher(ctx, tx, `
MATCH (entity:CodeEntity {repository_id: $repository_id})
DETACH DELETE entity
RETURN count(entity)`, map[string]any{"repository_id": repository.ID}); err != nil {
			return err
		}
	}
	if err := s.mergeRepository(ctx, tx, repository); err != nil {
		return err
	}
	for _, entity := range graph.Entities {
		if err := s.mergeCodeEntity(ctx, tx, entity); err != nil {
			return err
		}
		if err := s.mergeRepositoryContainsCode(ctx, tx, repository.ID, entity.ID, graph.Analysis.ProjectID); err != nil {
			return err
		}
	}
	for _, relation := range graph.Relations {
		if err := s.mergeCodeRelation(ctx, tx, relation, graph.Analysis.ProjectID); err != nil {
			return err
		}
	}
	for _, reference := range references {
		if err := s.mergeKnowledgeCodeReference(ctx, tx, reference); err != nil {
			return err
		}
	}
	if err := s.markRepositoryHeads(ctx, tx, repository); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO graph_projection_heads(
      repository_id,analysis_run_id,revision,backend,projected_at,status,last_error
    ) VALUES($1,$2,$3,'apache-age',now(),'ready','')
    ON CONFLICT (repository_id) DO UPDATE SET analysis_run_id=EXCLUDED.analysis_run_id,
      revision=EXCLUDED.revision,backend=EXCLUDED.backend,projected_at=now(),status='ready',last_error=''`,
		repository.ID, graph.Analysis.ID, graph.Analysis.Revision)
	return err
}

func (s *Store) ProjectCodeGraph(ctx context.Context, runID string) error {
	graph, active, err := s.authority.GetActiveCodeGraphForRun(ctx, runID)
	if err != nil || !active {
		return err
	}
	references, err := s.authority.GetKnowledgeReferencesForRepository(ctx, graph.Analysis.Repository.ID)
	if err != nil {
		return err
	}
	return s.withAGE(ctx, func(tx pgx.Tx) error {
		if err := s.projectCodeGraph(ctx, tx, graph, references, true); err != nil {
			return fmt.Errorf("project active code graph %s: %w", runID, err)
		}
		return nil
	})
}

func (s *Store) mergeKnowledgeItem(ctx context.Context, tx pgx.Tx, item domain.KnowledgeItem) error {
	return s.execCypher(ctx, tx, `
MERGE (knowledge:KnowledgeItem {id: $id})
SET knowledge.node_type = 'knowledge_item', knowledge.project_id = $project_id,
    knowledge.title = $title, knowledge.version = $version
RETURN knowledge`, map[string]any{
		"id": item.ID, "project_id": item.ProjectID, "title": item.Title, "version": item.Version,
	})
}

func (s *Store) mergeKnowledgeRelation(ctx context.Context, tx pgx.Tx, relation domain.KnowledgeRelation) error {
	return s.execCypher(ctx, tx, `
MATCH (source:KnowledgeItem {id: $source_id}), (target:KnowledgeItem {id: $target_id})
MERGE (source)-[edge:RELATES_TO {id: $id}]->(target)
SET edge.project_id = $project_id, edge.relation_type = $relation_type
RETURN edge`, map[string]any{
		"id": relation.ID, "project_id": relation.ProjectID, "relation_type": relation.RelationType,
		"source_id": relation.FromID, "target_id": relation.ToID,
	})
}

func (s *Store) mergeKnowledgeCodeReference(ctx context.Context, tx pgx.Tx, reference domain.KnowledgeCodeReference) error {
	label, ok := knowledgeCodeEdgeLabels[reference.Role]
	if !ok {
		return fmt.Errorf("unsupported knowledge-code AGE edge %q", reference.Role)
	}
	cypher := fmt.Sprintf(`
MATCH (knowledge:KnowledgeItem {id: $knowledge_id}), (entity:CodeEntity {id: $entity_id})
MERGE (knowledge)-[edge:%s {id: $id}]->(entity)
SET edge.project_id = $project_id, edge.relation_type = $role,
    edge.analysis_run_id = $analysis_run_id
RETURN edge`, label)
	return s.execCypher(ctx, tx, cypher, map[string]any{
		"id": reference.ID, "project_id": reference.ProjectID, "role": reference.Role,
		"analysis_run_id": reference.AnalysisRunID, "knowledge_id": reference.KnowledgeID,
		"entity_id": reference.EntityID,
	})
}

func (s *Store) ProjectKnowledge(ctx context.Context, id string) error {
	item, relations, references, approved, err := s.authority.GetKnowledgeProjection(ctx, id)
	if err != nil {
		return err
	}
	return s.withAGE(ctx, func(tx pgx.Tx) error {
		if err := s.execCypher(ctx, tx, `
MATCH (knowledge:KnowledgeItem {id: $id})
DETACH DELETE knowledge
RETURN count(knowledge)`, map[string]any{"id": id}); err != nil {
			return err
		}
		if !approved {
			_, err := tx.Exec(ctx, `DELETE FROM graph_knowledge_projection_heads WHERE knowledge_id::text=$1`, id)
			return err
		}
		if err := s.mergeKnowledgeItem(ctx, tx, item); err != nil {
			return err
		}
		for _, relation := range relations {
			if err := s.mergeKnowledgeRelation(ctx, tx, relation); err != nil {
				return err
			}
		}
		for _, reference := range references {
			if err := s.mergeKnowledgeCodeReference(ctx, tx, reference); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO graph_knowledge_projection_heads(
          knowledge_id,version,backend,projected_at,status,last_error
        ) VALUES($1,$2,'apache-age',now(),'ready','')
        ON CONFLICT (knowledge_id) DO UPDATE SET version=EXCLUDED.version,backend=EXCLUDED.backend,
          projected_at=now(),status='ready',last_error=''`, item.ID, item.Version)
		return err
	})
}

func (s *Store) Rebuild(ctx context.Context) (RebuildStats, error) {
	snapshot, err := s.authority.LoadGraphProjectionSnapshot(ctx)
	if err != nil {
		return RebuildStats{}, err
	}
	stats := RebuildStats{
		Repositories: len(snapshot.Repositories), RepositoryRelations: len(snapshot.RepositoryRelations),
		KnowledgeItems: len(snapshot.Knowledge),
		KnowledgeEdges: len(snapshot.KnowledgeRelations) + len(snapshot.KnowledgeCodeReferences),
	}
	for _, graph := range snapshot.CodeGraphs {
		stats.CodeEntities += len(graph.Entities)
		stats.CodeRelations += len(graph.Relations)
	}
	err = s.withAGE(ctx, func(tx pgx.Tx) error {
		if err := s.execCypher(ctx, tx, `MATCH (node) DETACH DELETE node RETURN count(node)`, map[string]any{}); err != nil {
			return fmt.Errorf("clear AGE graph: %w", err)
		}
		if _, err := tx.Exec(ctx, `TRUNCATE graph_repository_projection_heads,graph_projection_relations,
          graph_projection_heads,graph_knowledge_projection_heads`); err != nil {
			return err
		}
		for _, repository := range snapshot.Repositories {
			if err := s.mergeRepository(ctx, tx, repository); err != nil {
				return err
			}
			if err := s.markRepositoryHeads(ctx, tx, repository); err != nil {
				return err
			}
		}
		for _, relation := range snapshot.RepositoryRelations {
			if err := s.mergeRepositoryRelation(ctx, tx, relation); err != nil {
				return err
			}
			if err := s.markRepositoryRelation(ctx, tx, relation); err != nil {
				return err
			}
		}
		for _, item := range snapshot.Knowledge {
			if err := s.mergeKnowledgeItem(ctx, tx, item); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO graph_knowledge_projection_heads(
              knowledge_id,version,backend,projected_at,status,last_error
            ) VALUES($1,$2,'apache-age',now(),'ready','')`, item.ID, item.Version); err != nil {
				return err
			}
		}
		for _, graph := range snapshot.CodeGraphs {
			if err := s.projectCodeGraph(ctx, tx, graph, nil, false); err != nil {
				return err
			}
		}
		for _, relation := range snapshot.KnowledgeRelations {
			if err := s.mergeKnowledgeRelation(ctx, tx, relation); err != nil {
				return err
			}
		}
		for _, reference := range snapshot.KnowledgeCodeReferences {
			if err := s.mergeKnowledgeCodeReference(ctx, tx, reference); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return RebuildStats{}, err
	}
	return stats, nil
}

var _ domain.GraphProjector = (*Store)(nil)
