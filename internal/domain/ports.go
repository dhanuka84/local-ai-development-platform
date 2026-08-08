package domain

import "context"

import "github.com/dhanuka84/hybrid-ai-platform/components/codegraph"

type Repository interface {
	Ping(context.Context) error
	RecordGeneration(context.Context, GenerationCapture) (KnowledgeItem, error)
	GetKnowledge(context.Context, string, bool) (KnowledgeItem, error)
	GetKnowledgeMany(context.Context, []string) ([]KnowledgeItem, error)
	SearchApprovedLexical(context.Context, string, string, int) ([]SearchHit, error)
	ListCandidates(context.Context, string, int) ([]KnowledgeItem, error)
	ApproveCandidate(context.Context, string, string) (KnowledgeItem, error)
	RejectCandidate(context.Context, string, string) (KnowledgeItem, error)
	RecordReview(context.Context, ReviewRecord) error
	ClaimOutbox(context.Context, int) ([]OutboxEvent, error)
	CompleteOutbox(context.Context, int64) error
	FailOutbox(context.Context, int64, string) error
	RequeueApprovedKnowledge(context.Context) (int64, error)
	RequeueRepositoryRelations(context.Context) (int64, error)
	UpsertRepositoryRelation(context.Context, RepositoryRelation) (RepositoryRelation, error)
	GetRepositoryRelation(context.Context, string) (RepositoryRelation, error)
	GetRepositoryRelationsMany(context.Context, []string) ([]RepositoryRelation, error)
	GetRepositoryGraph(context.Context, string, string, int) ([]RepositoryRelation, error)
	StoreCodeGraph(context.Context, string, SoftwareRepository, string, codegraph.Snapshot) (CodeAnalysis, error)
	GetCodeEntity(context.Context, string) (CodeEntity, error)
	GetCodeEntitiesMany(context.Context, []string) ([]CodeEntity, error)
	SearchCodeEntitiesLexical(context.Context, string, string, string, int) ([]CodeEntity, error)
	GetCodeGraph(context.Context, string, string, string, int) (CodeGraph, error)
	RequeueCodeEntities(context.Context) (int64, error)
}

type ArtifactStore interface {
	Put(context.Context, []byte, string) (Artifact, error)
}

type Embedder interface {
	Embed(context.Context, []string) ([][]float32, error)
	Ping(context.Context) error
}

type VectorStore interface {
	EnsureCollection(context.Context) error
	Upsert(context.Context, KnowledgeItem, []float32) error
	Search(context.Context, string, []float32, int) ([]VectorHit, error)
	UpsertRelation(context.Context, RepositoryRelation, []float32) error
	SearchRelations(context.Context, string, []float32, int) ([]VectorHit, error)
	UpsertCodeEntity(context.Context, CodeEntity, []float32) error
	SearchCodeEntities(context.Context, string, string, []float32, int) ([]VectorHit, error)
	Ping(context.Context) error
	Close(context.Context) error
}
