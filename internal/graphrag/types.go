package graphrag

import "github.com/dhanuka84/hybrid-ai-platform/internal/domain"

const (
	DefaultMaxHops   = 2
	DefaultSeedLimit = 8
	DefaultMaxNodes  = 40
	DefaultMaxEdges  = 80
	MaxAllowedHops   = 5
	MaxAllowedSeeds  = 32
	MaxAllowedNodes  = 200
	MaxAllowedEdges  = 400
)

type Request struct {
	ProjectID  string
	Query      string
	Repository string
	MaxHops    int
	SeedLimit  int
	MaxNodes   int
	MaxEdges   int
}

type Context struct {
	Query       string                   `json:"query"`
	SeedBackend string                   `json:"seed_backend"`
	Graph       domain.KnowledgeSubgraph `json:"graph"`
}

type seed struct {
	ID    string
	Type  string
	Score float32
}
