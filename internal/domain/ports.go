package domain

import "context"

import "github.com/dhanuka84/hybrid-ai-platform/components/codegraph"

type Repository interface {
	Ping(context.Context) error
	BootstrapPrincipals(context.Context, []PrincipalBootstrap) error
	AuthenticatePrincipal(context.Context, []byte) (Principal, error)
	CreateWorkflow(context.Context, WorkflowRun, WorkflowEvent) (WorkflowRun, error)
	GetWorkflow(context.Context, string) (WorkflowRun, error)
	TransitionWorkflow(context.Context, WorkflowTransition) (WorkflowRun, WorkflowEvent, error)
	CreateWorkflowTask(context.Context, WorkflowTaskCheckpoint, WorkflowTaskEvent) (WorkflowTaskCheckpoint, WorkflowTaskEvent, error)
	GetWorkflowTask(context.Context, string) (WorkflowTaskCheckpoint, error)
	GetWorkflowTaskByCandidate(context.Context, string) (WorkflowTaskCheckpoint, error)
	GetActivatableWorkflowTask(context.Context, string) (WorkflowTaskCheckpoint, bool, error)
	TransitionWorkflowTask(context.Context, WorkflowTaskTransition) (WorkflowTaskCheckpoint, WorkflowTaskEvent, error)
	RecordGeneration(context.Context, GenerationCapture) (KnowledgeItem, error)
	GetKnowledge(context.Context, string, bool) (KnowledgeItem, error)
	GetKnowledgeMany(context.Context, []string) ([]KnowledgeItem, error)
	SearchApprovedLexical(context.Context, string, string, int) ([]SearchHit, error)
	ListCandidates(context.Context, string, int) ([]KnowledgeItem, error)
	ApproveCandidate(context.Context, string, string) (KnowledgeItem, error)
	RejectCandidate(context.Context, string, string) (KnowledgeItem, error)
	RecordReview(context.Context, ReviewRecord) error
	ReviewEvidenceExists(context.Context, string, string, string, string, string, string) (bool, error)
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
	GetSemanticGraphEdge(context.Context, string) (SemanticGraphEdge, bool, error)
	GetSemanticGraphEdgesMany(context.Context, []string) ([]SemanticGraphEdge, error)
	GetSemanticGraphEdgesForKnowledge(context.Context, string) ([]SemanticGraphEdge, error)
	RequeueSemanticGraphEdges(context.Context) (int64, error)
}

type RepositoryGraphRequest struct {
	ProjectID string
	Root      string
	MaxHops   int
}

type CodeGraphRequest struct {
	ProjectID      string
	RepositoryRoot string
	SymbolRoot     string
	MaxHops        int
}

type KnowledgeGraphRequest struct {
	ProjectID         string
	KnowledgeSeedIDs  []string
	CodeSeedIDs       []string
	RepositorySeedIDs []string
	MaxHops           int
	MaxNodes          int
	MaxEdges          int
}

type GraphStore interface {
	ExpandRepositoryGraph(context.Context, RepositoryGraphRequest) ([]RepositoryRelation, error)
	ExpandCodeGraph(context.Context, CodeGraphRequest) (CodeGraph, error)
	ExpandKnowledgeGraph(context.Context, KnowledgeGraphRequest) (KnowledgeSubgraph, error)
}

type GraphProjector interface {
	ProjectRepositoryRelation(context.Context, RepositoryRelation) error
	ProjectCodeGraph(context.Context, string) error
	ProjectKnowledge(context.Context, string) error
}

type HealthChecker interface {
	Ping(context.Context) error
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
	UpsertGraphEdge(context.Context, SemanticGraphEdge, []float32) error
	SearchGraphEdges(context.Context, string, string, []float32, int) ([]VectorHit, error)
	Ping(context.Context) error
	Close(context.Context) error
}
