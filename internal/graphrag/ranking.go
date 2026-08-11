package graphrag

import (
	"sort"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

func rankGraph(graph *domain.KnowledgeSubgraph, seedScores map[string]float32) {
	bestSeed := float32(1)
	for _, score := range seedScores {
		if score > bestSeed {
			bestSeed = score
		}
	}
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		if score, ok := seedScores[node.Type+":"+node.ID]; ok {
			node.Score = score
		} else {
			node.Score = bestSeed * 0.7 / float32(node.Distance+1)
		}
	}
	sort.SliceStable(graph.Nodes, func(i, j int) bool {
		if graph.Nodes[i].Score != graph.Nodes[j].Score {
			return graph.Nodes[i].Score > graph.Nodes[j].Score
		}
		if graph.Nodes[i].Distance != graph.Nodes[j].Distance {
			return graph.Nodes[i].Distance < graph.Nodes[j].Distance
		}
		return graph.Nodes[i].Type+graph.Nodes[i].ID < graph.Nodes[j].Type+graph.Nodes[j].ID
	})
}
