package age

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	graphNamePattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	ErrProjectionStale = errors.New("apache age projection is stale")
)

type Store struct {
	pool      *pgxpool.Pool
	authority *postgres.Repository
	graphName string
}

func New(pool *pgxpool.Pool, authority *postgres.Repository, graphName string) (*Store, error) {
	if pool == nil || authority == nil {
		return nil, errors.New("PostgreSQL pool and graph authority are required")
	}
	if !graphNamePattern.MatchString(graphName) {
		return nil, fmt.Errorf("invalid AGE graph name %q", graphName)
	}
	return &Store{pool: pool, authority: authority, graphName: graphName}, nil
}

func (s *Store) Ensure(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS age`); err != nil {
		return fmt.Errorf("enable Apache AGE extension: %w", err)
	}
	return s.withAGE(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ag_catalog.ag_graph WHERE name=$1)`, s.graphName).Scan(&exists); err != nil {
			return fmt.Errorf("inspect AGE graph: %w", err)
		}
		if exists {
			return nil
		}
		if _, err := tx.Exec(ctx, `SELECT ag_catalog.create_graph($1)`, s.graphName); err != nil {
			return fmt.Errorf("create AGE graph %q: %w", s.graphName, err)
		}
		return nil
	})
}

func (s *Store) Ping(ctx context.Context) error {
	var installed bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='age')`).Scan(&installed); err != nil {
		return err
	}
	if !installed {
		return errors.New("Apache AGE extension is not installed")
	}
	return s.withAGE(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ag_catalog.ag_graph WHERE name=$1)`, s.graphName).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("Apache AGE graph %q is not initialized", s.graphName)
		}
		return nil
	})
}

func (s *Store) withAGE(ctx context.Context, operation func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `LOAD 'age'`); err != nil {
		return fmt.Errorf("load Apache AGE: %w", err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL search_path = ag_catalog, "$user", public`); err != nil {
		return fmt.Errorf("configure Apache AGE search path: %w", err)
	}
	if err := operation(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) cypherSQL(cypher, columns string) string {
	return fmt.Sprintf(`SELECT * FROM ag_catalog.cypher('%s', $cypher$%s$cypher$, $1::ag_catalog.agtype) AS (%s)`,
		s.graphName, cypher, columns)
}

func marshalParameters(values map[string]any) (string, error) {
	encoded, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("encode Cypher parameters: %w", err)
	}
	return string(encoded), nil
}

func decodeAGString(value string) (string, error) {
	var decoded string
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return "", fmt.Errorf("decode AGE string %q: %w", value, err)
	}
	return decoded, nil
}

func (s *Store) queryNodes(ctx context.Context, label, seedID, projectID string, maxHops, limit int) ([]domain.GraphNode, error) {
	parameters, err := marshalParameters(map[string]any{"seed_id": seedID, "project_id": projectID})
	if err != nil {
		return nil, err
	}
	cypher := fmt.Sprintf(`
MATCH p=(seed:%s {id: $seed_id})-[*1..%d]-(related)
WHERE related.project_id = $project_id
RETURN related.id, related.node_type, length(p)
LIMIT %d`, label, maxHops, limit)
	result := make([]domain.GraphNode, 0)
	err = s.withAGE(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, s.cypherSQL(cypher, `id ag_catalog.agtype,node_type ag_catalog.agtype,distance ag_catalog.agtype`), parameters)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var rawID, rawType, rawDistance string
			if err := rows.Scan(&rawID, &rawType, &rawDistance); err != nil {
				return err
			}
			id, err := decodeAGString(rawID)
			if err != nil {
				return err
			}
			nodeType, err := decodeAGString(rawType)
			if err != nil {
				return err
			}
			distance, err := strconv.Atoi(strings.TrimSpace(rawDistance))
			if err != nil {
				return fmt.Errorf("decode AGE distance %q: %w", rawDistance, err)
			}
			result = append(result, domain.GraphNode{ID: id, Type: nodeType, Distance: distance})
		}
		return rows.Err()
	})
	return result, err
}

func (s *Store) ExpandRepositoryGraph(ctx context.Context, request domain.RepositoryGraphRequest) ([]domain.RepositoryRelation, error) {
	repository, err := s.authority.ResolveSoftwareRepository(ctx, request.ProjectID, request.Root)
	if err != nil {
		return nil, err
	}
	current, err := s.authority.RepositoryProjectionCurrent(ctx, request.ProjectID, []string{repository.ID})
	if err != nil {
		return nil, err
	}
	if !current {
		return nil, fmt.Errorf("%w: repository %s", ErrProjectionStale, repository.ID)
	}
	nodes, err := s.queryNodes(ctx, "Repository", repository.ID, request.ProjectID, request.MaxHops, 1001)
	if err != nil {
		return nil, fmt.Errorf("expand AGE repository graph: %w", err)
	}
	ids := uniqueNodeIDs(append(nodes, domain.GraphNode{ID: repository.ID, Type: domain.GraphNodeRepository}))
	if len(ids) > 1000 {
		return nil, errors.New("AGE repository traversal exceeded the 1000-node safety limit")
	}
	current, err = s.authority.RepositoryProjectionCurrent(ctx, request.ProjectID, ids)
	if err != nil {
		return nil, err
	}
	if !current {
		return nil, fmt.Errorf("%w: repository traversal", ErrProjectionStale)
	}
	return s.authority.GetRepositoryRelationsForNodes(ctx, request.ProjectID, ids)
}

func (s *Store) ExpandCodeGraph(ctx context.Context, request domain.CodeGraphRequest) (domain.CodeGraph, error) {
	analysis, err := s.authority.GetActiveCodeAnalysis(ctx, request.ProjectID, request.RepositoryRoot)
	if err != nil {
		return domain.CodeGraph{}, err
	}
	seed, err := s.authority.ResolveActiveCodeEntity(ctx, analysis.ID, request.SymbolRoot)
	if err != nil {
		return domain.CodeGraph{}, err
	}
	current, err := s.authority.CodeProjectionCurrent(ctx, analysis.Repository.ID, analysis.ID, analysis.Revision)
	if err != nil {
		return domain.CodeGraph{}, err
	}
	if !current {
		return domain.CodeGraph{}, fmt.Errorf("%w: code repository %s", ErrProjectionStale, analysis.Repository.ID)
	}
	nodes, err := s.queryNodes(ctx, "CodeEntity", seed.ID, request.ProjectID, request.MaxHops, 2001)
	if err != nil {
		return domain.CodeGraph{}, fmt.Errorf("expand AGE code graph: %w", err)
	}
	ids := uniqueNodeIDsOfType(append(nodes, domain.GraphNode{ID: seed.ID, Type: domain.GraphNodeCodeEntity}), domain.GraphNodeCodeEntity)
	if len(ids) > 2000 {
		return domain.CodeGraph{}, errors.New("AGE code traversal exceeded the 2000-node safety limit")
	}
	current, err = s.authority.CodeProjectionCurrent(ctx, analysis.Repository.ID, analysis.ID, analysis.Revision)
	if err != nil {
		return domain.CodeGraph{}, err
	}
	if !current {
		return domain.CodeGraph{}, fmt.Errorf("%w: code traversal", ErrProjectionStale)
	}
	graph, err := s.authority.HydrateCodeGraph(ctx, analysis, ids)
	if err != nil {
		return domain.CodeGraph{}, err
	}
	return graph, nil
}

func (s *Store) ExpandKnowledgeGraph(ctx context.Context, request domain.KnowledgeGraphRequest) (domain.KnowledgeSubgraph, error) {
	seedNodes := make([]domain.GraphNode, 0, len(request.KnowledgeSeedIDs)+len(request.CodeSeedIDs)+len(request.RepositorySeedIDs))
	appendSeeds := func(ids []string, nodeType string) {
		for _, id := range ids {
			seedNodes = append(seedNodes, domain.GraphNode{ID: id, Type: nodeType})
		}
	}
	appendSeeds(request.KnowledgeSeedIDs, domain.GraphNodeKnowledgeItem)
	appendSeeds(request.CodeSeedIDs, domain.GraphNodeCodeEntity)
	appendSeeds(request.RepositorySeedIDs, domain.GraphNodeRepository)
	if err := s.verifyProjection(ctx, request.ProjectID, seedNodes); err != nil {
		return domain.KnowledgeSubgraph{}, err
	}
	nodeMap := make(map[string]domain.GraphNode, len(seedNodes))
	for _, seed := range seedNodes {
		nodeMap[seed.Type+":"+seed.ID] = seed
		label := graphLabel(seed.Type)
		if label == "" {
			continue
		}
		nodes, err := s.queryNodes(ctx, label, seed.ID, request.ProjectID, request.MaxHops, request.MaxNodes+1)
		if err != nil {
			return domain.KnowledgeSubgraph{}, fmt.Errorf("expand AGE unified graph: %w", err)
		}
		for _, node := range nodes {
			key := node.Type + ":" + node.ID
			if existing, ok := nodeMap[key]; !ok || node.Distance < existing.Distance {
				nodeMap[key] = node
			}
		}
	}
	nodes := make([]domain.GraphNode, 0, len(nodeMap))
	for _, node := range nodeMap {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Distance != nodes[j].Distance {
			return nodes[i].Distance < nodes[j].Distance
		}
		if nodes[i].Type != nodes[j].Type {
			return nodes[i].Type < nodes[j].Type
		}
		return nodes[i].ID < nodes[j].ID
	})
	truncated := len(nodes) > request.MaxNodes
	if truncated {
		nodes = nodes[:request.MaxNodes]
	}
	if err := s.verifyProjection(ctx, request.ProjectID, nodes); err != nil {
		return domain.KnowledgeSubgraph{}, err
	}
	result, err := s.authority.HydrateKnowledgeSubgraph(ctx, request.ProjectID, nodes, request.MaxEdges)
	if err != nil {
		return domain.KnowledgeSubgraph{}, err
	}
	result.Backend = "apache-age"
	result.Truncated = result.Truncated || truncated
	return result, nil
}

func (s *Store) verifyProjection(ctx context.Context, projectID string, nodes []domain.GraphNode) error {
	byType := map[string][]string{}
	for _, node := range nodes {
		byType[node.Type] = append(byType[node.Type], node.ID)
	}
	if ids := uniqueStrings(byType[domain.GraphNodeRepository]); len(ids) > 0 {
		current, err := s.authority.RepositoryProjectionCurrent(ctx, projectID, ids)
		if err != nil {
			return err
		}
		if !current {
			return fmt.Errorf("%w: repository nodes", ErrProjectionStale)
		}
	}
	if ids := uniqueStrings(byType[domain.GraphNodeCodeEntity]); len(ids) > 0 {
		current, err := s.authority.CodeProjectionsCurrentForEntities(ctx, projectID, ids)
		if err != nil {
			return err
		}
		if !current {
			return fmt.Errorf("%w: code nodes", ErrProjectionStale)
		}
	}
	if ids := uniqueStrings(byType[domain.GraphNodeKnowledgeItem]); len(ids) > 0 {
		current, err := s.authority.KnowledgeProjectionsCurrent(ctx, projectID, ids)
		if err != nil {
			return err
		}
		if !current {
			return fmt.Errorf("%w: knowledge nodes", ErrProjectionStale)
		}
	}
	return nil
}

func graphLabel(nodeType string) string {
	switch nodeType {
	case domain.GraphNodeRepository:
		return "Repository"
	case domain.GraphNodeCodeEntity:
		return "CodeEntity"
	case domain.GraphNodeKnowledgeItem:
		return "KnowledgeItem"
	default:
		return ""
	}
}

func uniqueNodeIDs(nodes []domain.GraphNode) []string {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.ID)
	}
	return uniqueStrings(ids)
}

func uniqueNodeIDsOfType(nodes []domain.GraphNode, nodeType string) []string {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node.Type == nodeType {
			ids = append(ids, node.ID)
		}
	}
	return uniqueStrings(ids)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

var _ domain.GraphStore = (*Store)(nil)
var _ domain.HealthChecker = (*Store)(nil)
