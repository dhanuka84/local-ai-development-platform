package graphrag

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type Service struct {
	repository domain.Repository
	embedder   domain.Embedder
	vectors    domain.VectorStore
	graphs     domain.GraphStore
}

func New(repository domain.Repository, embedder domain.Embedder, vectors domain.VectorStore, graphs domain.GraphStore) (*Service, error) {
	if repository == nil || embedder == nil || vectors == nil || graphs == nil {
		return nil, errors.New("GraphRAG repository, embedder, vector store, and graph store are required")
	}
	return &Service{repository: repository, embedder: embedder, vectors: vectors, graphs: graphs}, nil
}

func (s *Service) Search(ctx context.Context, request Request) (Context, error) {
	request = normalizeRequest(request)
	if request.ProjectID == "" || request.Query == "" {
		return Context{}, errors.New("project_id and query are required")
	}
	seeds, backend, err := s.semanticSeeds(ctx, request)
	if err != nil || len(seeds) == 0 {
		seeds, err = s.lexicalSeeds(ctx, request)
		backend = "postgres-lexical-fallback"
	}
	if err != nil {
		return Context{}, fmt.Errorf("discover GraphRAG seeds: %w", err)
	}
	graphRequest := domain.KnowledgeGraphRequest{
		ProjectID: request.ProjectID, MaxHops: request.MaxHops,
		MaxNodes: request.MaxNodes, MaxEdges: request.MaxEdges,
	}
	seedScores := make(map[string]float32, len(seeds))
	for _, candidate := range seeds {
		seedScores[candidate.Type+":"+candidate.ID] = candidate.Score
		switch candidate.Type {
		case domain.GraphNodeKnowledgeItem:
			graphRequest.KnowledgeSeedIDs = append(graphRequest.KnowledgeSeedIDs, candidate.ID)
		case domain.GraphNodeCodeEntity:
			graphRequest.CodeSeedIDs = append(graphRequest.CodeSeedIDs, candidate.ID)
		case domain.GraphNodeRepository:
			graphRequest.RepositorySeedIDs = append(graphRequest.RepositorySeedIDs, candidate.ID)
		}
	}
	if len(seeds) == 0 {
		return Context{Query: request.Query, SeedBackend: backend, Graph: domain.KnowledgeSubgraph{
			Backend: "none", Nodes: []domain.GraphNode{}, Edges: []domain.GraphEdge{},
		}}, nil
	}
	graph, err := s.graphs.ExpandKnowledgeGraph(ctx, graphRequest)
	if err != nil {
		return Context{}, err
	}
	rankGraph(&graph, seedScores)
	return buildContext(request.Query, backend, graph), nil
}

func normalizeRequest(request Request) Request {
	request.ProjectID = strings.TrimSpace(request.ProjectID)
	request.Query = strings.TrimSpace(request.Query)
	request.Repository = strings.TrimSpace(request.Repository)
	request.MaxHops = bounded(request.MaxHops, DefaultMaxHops, MaxAllowedHops)
	request.SeedLimit = bounded(request.SeedLimit, DefaultSeedLimit, MaxAllowedSeeds)
	request.MaxNodes = bounded(request.MaxNodes, DefaultMaxNodes, MaxAllowedNodes)
	request.MaxEdges = bounded(request.MaxEdges, DefaultMaxEdges, MaxAllowedEdges)
	return request
}

func bounded(value, fallback, maximum int) int {
	if value < 1 {
		return fallback
	}
	if value > maximum {
		return maximum
	}
	return value
}
