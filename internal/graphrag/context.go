package graphrag

import "github.com/dhanuka84/hybrid-ai-platform/internal/domain"

func buildContext(query, seedBackend string, graph domain.KnowledgeSubgraph) Context {
	if graph.Nodes == nil {
		graph.Nodes = []domain.GraphNode{}
	}
	if graph.Edges == nil {
		graph.Edges = []domain.GraphEdge{}
	}
	return Context{Query: query, SeedBackend: seedBackend, Graph: graph}
}
