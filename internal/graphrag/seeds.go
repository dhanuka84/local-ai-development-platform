package graphrag

import (
	"context"
	"errors"
	"sort"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

func (s *Service) semanticSeeds(ctx context.Context, request Request) ([]seed, string, error) {
	embeddings, err := s.embedder.Embed(ctx, []string{request.Query})
	if err != nil || len(embeddings) != 1 {
		if err == nil {
			err = errors.New("embedding provider returned an unexpected result count")
		}
		return nil, "", err
	}
	vector := embeddings[0]
	knowledgeHits, knowledgeErr := s.vectors.Search(ctx, request.ProjectID, vector, request.SeedLimit)
	codeHits, codeErr := s.vectors.SearchCodeEntities(ctx, request.ProjectID, "", vector, request.SeedLimit)
	relationHits, relationErr := s.vectors.SearchRelations(ctx, request.ProjectID, vector, request.SeedLimit)
	edgeHits, edgeErr := s.vectors.SearchGraphEdges(ctx, request.ProjectID, "", vector, request.SeedLimit)
	if knowledgeErr != nil && codeErr != nil && relationErr != nil && edgeErr != nil {
		return nil, "", errors.Join(knowledgeErr, codeErr, relationErr, edgeErr)
	}
	candidates := make([]seed, 0, request.SeedLimit*4)
	knowledgeIDs := hitIDs(knowledgeHits)
	knowledge, err := s.repository.GetKnowledgeMany(ctx, knowledgeIDs)
	if err != nil {
		return nil, "", err
	}
	knowledgeScores := hitScores(knowledgeHits)
	for _, item := range knowledge {
		candidates = append(candidates, seed{ID: item.ID, Type: domain.GraphNodeKnowledgeItem, Score: knowledgeScores[item.ID]})
	}
	code, err := s.repository.GetCodeEntitiesMany(ctx, hitIDs(codeHits))
	if err != nil {
		return nil, "", err
	}
	codeScores := hitScores(codeHits)
	for _, entity := range code {
		if request.Repository != "" && request.Repository != entity.RepositoryID && request.Repository != entity.RepositoryName {
			continue
		}
		candidates = append(candidates, seed{ID: entity.ID, Type: domain.GraphNodeCodeEntity, Score: codeScores[entity.ID]})
	}
	relations, err := s.repository.GetRepositoryRelationsMany(ctx, hitIDs(relationHits))
	if err != nil {
		return nil, "", err
	}
	relationScores := hitScores(relationHits)
	for _, relation := range relations {
		score := relationScores[relation.ID]
		candidates = append(candidates,
			seed{ID: relation.From.ID, Type: domain.GraphNodeRepository, Score: score},
			seed{ID: relation.To.ID, Type: domain.GraphNodeRepository, Score: score})
	}
	edges, err := s.repository.GetSemanticGraphEdgesMany(ctx, hitIDs(edgeHits))
	if err != nil {
		return nil, "", err
	}
	edgeScores := hitScores(edgeHits)
	for _, edge := range edges {
		score := edgeScores[edge.ID]
		candidates = append(candidates,
			seed{ID: edge.SourceID, Type: edge.SourceType, Score: score},
			seed{ID: edge.TargetID, Type: edge.TargetType, Score: score})
	}
	return topSeeds(candidates, request.SeedLimit), "milvus", nil
}

func (s *Service) lexicalSeeds(ctx context.Context, request Request) ([]seed, error) {
	candidates := make([]seed, 0, request.SeedLimit*2)
	knowledge, knowledgeErr := s.repository.SearchApprovedLexical(ctx, request.ProjectID, request.Query, request.SeedLimit)
	if knowledgeErr == nil {
		for _, hit := range knowledge {
			candidates = append(candidates, seed{ID: hit.ID, Type: domain.GraphNodeKnowledgeItem, Score: hit.Score})
		}
	}
	code, codeErr := s.repository.SearchCodeEntitiesLexical(ctx, request.ProjectID, request.Repository, request.Query, request.SeedLimit)
	if codeErr == nil {
		for _, entity := range code {
			candidates = append(candidates, seed{ID: entity.ID, Type: domain.GraphNodeCodeEntity, Score: entity.Score})
		}
	}
	if knowledgeErr != nil && codeErr != nil {
		return nil, errors.Join(knowledgeErr, codeErr)
	}
	return topSeeds(candidates, request.SeedLimit), nil
}

func topSeeds(candidates []seed, limit int) []seed {
	best := make(map[string]seed, len(candidates))
	for _, candidate := range candidates {
		key := candidate.Type + ":" + candidate.ID
		if current, ok := best[key]; !ok || candidate.Score > current.Score {
			best[key] = candidate
		}
	}
	result := make([]seed, 0, len(best))
	for _, candidate := range best {
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		if result[i].Type != result[j].Type {
			return result[i].Type < result[j].Type
		}
		return result[i].ID < result[j].ID
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

func hitIDs(hits []domain.VectorHit) []string {
	result := make([]string, 0, len(hits))
	for _, hit := range hits {
		result = append(result, hit.ID)
	}
	return result
}

func hitScores(hits []domain.VectorHit) map[string]float32 {
	result := make(map[string]float32, len(hits))
	for _, hit := range hits {
		result[hit.ID] = hit.Score
	}
	return result
}
