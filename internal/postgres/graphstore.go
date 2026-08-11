package postgres

import (
	"context"
	"sort"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type RecursiveGraphStore struct {
	repository *Repository
}

func NewRecursiveGraphStore(repository *Repository) *RecursiveGraphStore {
	return &RecursiveGraphStore{repository: repository}
}

func (s *RecursiveGraphStore) ExpandRepositoryGraph(ctx context.Context, request domain.RepositoryGraphRequest) ([]domain.RepositoryRelation, error) {
	return s.repository.GetRepositoryGraph(ctx, request.ProjectID, request.Root, request.MaxHops)
}

func (s *RecursiveGraphStore) ExpandCodeGraph(ctx context.Context, request domain.CodeGraphRequest) (domain.CodeGraph, error) {
	return s.repository.GetCodeGraph(ctx, request.ProjectID, request.RepositoryRoot, request.SymbolRoot, request.MaxHops)
}

const unifiedGraphEdgesSQL = `
    SELECT relation.project_id,relation.id::text,relation.relation_type,
           relation.from_repository_id::text,'repository',relation.to_repository_id::text,'repository',
           relation.evidence,relation.confidence
    FROM repository_relations relation
    UNION ALL
    SELECT entity.project_id,'repo-code:' || entity.repository_id::text || ':' || entity.id::text,'contains',
           entity.repository_id::text,'repository',entity.id::text,'code_entity','',1::real
    FROM code_entities entity
    JOIN code_repository_heads head ON head.repository_id=entity.repository_id
    JOIN code_occurrences occurrence ON occurrence.entity_id=entity.id AND occurrence.analysis_run_id=head.analysis_run_id
    UNION ALL
    SELECT entity.project_id,relation.id::text,relation.relation_type,
           relation.source_entity_id::text,'code_entity',relation.target_entity_id::text,'code_entity',
           relation.evidence,relation.confidence
    FROM code_relations relation
    JOIN code_entities entity ON entity.id=relation.source_entity_id
    JOIN code_repository_heads head ON head.repository_id=entity.repository_id AND head.analysis_run_id=relation.analysis_run_id
    UNION ALL
    SELECT source.project_id,
           encode(digest(concat_ws(chr(31),'knowledge_relation',relation.from_id::text,relation.to_id::text,relation.relation_type),'sha256'),'hex'),
           relation.relation_type,relation.from_id::text,'knowledge_item',relation.to_id::text,'knowledge_item','',relation.confidence
    FROM knowledge_relations relation
    JOIN knowledge_items source ON source.id=relation.from_id AND source.status='approved'
    JOIN knowledge_items target ON target.id=relation.to_id AND target.status='approved' AND target.project_id=source.project_id
    UNION ALL
    SELECT knowledge.project_id,
           encode(digest(concat_ws(chr(31),'knowledge_code',reference.knowledge_id::text,reference.entity_id::text,reference.analysis_run_id::text,reference.role),'sha256'),'hex'),
           reference.role,reference.knowledge_id::text,'knowledge_item',reference.entity_id::text,'code_entity',
           reference.evidence,1::real
    FROM knowledge_code_references reference
    JOIN knowledge_items knowledge ON knowledge.id=reference.knowledge_id AND knowledge.status='approved'
    JOIN code_entities entity ON entity.id=reference.entity_id AND entity.project_id=knowledge.project_id
    JOIN code_repository_heads head ON head.repository_id=entity.repository_id AND head.analysis_run_id=reference.analysis_run_id`

func (s *RecursiveGraphStore) ExpandKnowledgeGraph(ctx context.Context, request domain.KnowledgeGraphRequest) (domain.KnowledgeSubgraph, error) {
	seedIDs := make([]string, 0, len(request.KnowledgeSeedIDs)+len(request.CodeSeedIDs)+len(request.RepositorySeedIDs))
	seedTypes := make([]string, 0, cap(seedIDs))
	appendSeeds := func(ids []string, nodeType string) {
		for _, id := range ids {
			seedIDs = append(seedIDs, id)
			seedTypes = append(seedTypes, nodeType)
		}
	}
	appendSeeds(request.KnowledgeSeedIDs, domain.GraphNodeKnowledgeItem)
	appendSeeds(request.CodeSeedIDs, domain.GraphNodeCodeEntity)
	appendSeeds(request.RepositorySeedIDs, domain.GraphNodeRepository)
	if len(seedIDs) == 0 {
		return domain.KnowledgeSubgraph{Backend: "postgres-recursive", Nodes: []domain.GraphNode{}, Edges: []domain.GraphEdge{}}, nil
	}
	rows, err := s.repository.pool.Query(ctx, `WITH RECURSIVE graph_edges(
        project_id,edge_id,edge_type,source_id,source_type,target_id,target_type,evidence,confidence
      ) AS MATERIALIZED (`+unifiedGraphEdgesSQL+`), seeds(node_id,node_type) AS (
        SELECT * FROM unnest($2::text[],$3::text[])
      ), walk(node_id,node_type,path,depth) AS (
        SELECT node_id,node_type,ARRAY[node_type || ':' || node_id],0 FROM seeds
        UNION ALL
        SELECT CASE WHEN edge.source_id=walk.node_id AND edge.source_type=walk.node_type THEN edge.target_id ELSE edge.source_id END,
               CASE WHEN edge.source_id=walk.node_id AND edge.source_type=walk.node_type THEN edge.target_type ELSE edge.source_type END,
               walk.path || CASE WHEN edge.source_id=walk.node_id AND edge.source_type=walk.node_type
                                  THEN edge.target_type || ':' || edge.target_id ELSE edge.source_type || ':' || edge.source_id END,
               walk.depth+1
        FROM walk JOIN graph_edges edge ON edge.project_id=$1 AND (
          (edge.source_id=walk.node_id AND edge.source_type=walk.node_type) OR
          (edge.target_id=walk.node_id AND edge.target_type=walk.node_type))
        WHERE walk.depth < $4 AND NOT (CASE WHEN edge.source_id=walk.node_id AND edge.source_type=walk.node_type
          THEN edge.target_type || ':' || edge.target_id ELSE edge.source_type || ':' || edge.source_id END)=ANY(walk.path)
      )
      SELECT node_id,node_type,min(depth)::int FROM walk
      GROUP BY node_id,node_type ORDER BY min(depth),node_type,node_id LIMIT $5`,
		request.ProjectID, seedIDs, seedTypes, request.MaxHops, request.MaxNodes+1)
	if err != nil {
		return domain.KnowledgeSubgraph{}, err
	}
	nodes := make([]domain.GraphNode, 0, request.MaxNodes+1)
	for rows.Next() {
		var node domain.GraphNode
		if err := rows.Scan(&node.ID, &node.Type, &node.Distance); err != nil {
			rows.Close()
			return domain.KnowledgeSubgraph{}, err
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return domain.KnowledgeSubgraph{}, err
	}
	rows.Close()
	truncated := len(nodes) > request.MaxNodes
	if truncated {
		nodes = nodes[:request.MaxNodes]
	}
	result, err := s.repository.HydrateKnowledgeSubgraph(ctx, request.ProjectID, nodes, request.MaxEdges)
	if err != nil {
		return domain.KnowledgeSubgraph{}, err
	}
	result.Backend = "postgres-recursive"
	result.Truncated = result.Truncated || truncated
	return result, nil
}

func (r *Repository) HydrateKnowledgeSubgraph(ctx context.Context, projectID string, nodes []domain.GraphNode, maxEdges int) (domain.KnowledgeSubgraph, error) {
	result := domain.KnowledgeSubgraph{Nodes: nodes, Edges: []domain.GraphEdge{}}
	idsByType := map[string][]string{
		domain.GraphNodeRepository:    {},
		domain.GraphNodeCodeEntity:    {},
		domain.GraphNodeKnowledgeItem: {},
	}
	for _, node := range nodes {
		idsByType[node.Type] = append(idsByType[node.Type], node.ID)
	}
	var err error
	result.Repositories, err = r.GetSoftwareRepositoriesMany(ctx, projectID, idsByType[domain.GraphNodeRepository])
	if err != nil {
		return result, err
	}
	result.CodeEntities, err = r.GetCodeEntitiesMany(ctx, idsByType[domain.GraphNodeCodeEntity])
	if err != nil {
		return result, err
	}
	result.Knowledge, err = r.GetKnowledgeMany(ctx, idsByType[domain.GraphNodeKnowledgeItem])
	if err != nil {
		return result, err
	}
	nodeSet := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		nodeSet[node.Type+":"+node.ID] = struct{}{}
	}
	edgeRows, err := r.pool.Query(ctx, `WITH graph_edges(
        project_id,edge_id,edge_type,source_id,source_type,target_id,target_type,evidence,confidence
      ) AS (`+unifiedGraphEdgesSQL+`)
      SELECT edge_id,edge_type,source_id,source_type,target_id,target_type,evidence,confidence
      FROM graph_edges WHERE project_id=$1
        AND source_type || ':' || source_id=ANY($2)
        AND target_type || ':' || target_id=ANY($2)
      ORDER BY edge_type,edge_id LIMIT $3`, projectID, mapKeys(nodeSet), maxEdges+1)
	if err != nil {
		return result, err
	}
	for edgeRows.Next() {
		var edge domain.GraphEdge
		if err := edgeRows.Scan(&edge.ID, &edge.Type, &edge.SourceID, &edge.SourceType,
			&edge.TargetID, &edge.TargetType, &edge.Evidence, &edge.Confidence); err != nil {
			edgeRows.Close()
			return result, err
		}
		result.Edges = append(result.Edges, edge)
	}
	if err := edgeRows.Err(); err != nil {
		edgeRows.Close()
		return result, err
	}
	edgeRows.Close()
	if len(result.Edges) > maxEdges {
		result.Edges = result.Edges[:maxEdges]
		result.Truncated = true
	}
	return result, nil
}

func mapKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

var _ domain.GraphStore = (*RecursiveGraphStore)(nil)
