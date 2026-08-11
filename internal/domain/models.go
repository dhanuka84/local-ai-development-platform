package domain

import (
	"strings"
	"time"
)

const (
	CandidatePending  = "pending"
	CandidateApproved = "approved"
	CandidateRejected = "rejected"
)

type GenerationCapture struct {
	ID                 string
	ProjectID          string
	WorkflowID         string
	WorkflowStepID     string
	SessionID          string
	TaskType           string
	Prompt             string
	Response           string
	Summary            string
	Language           string
	Tags               []string
	Provider           string
	Model              string
	RepositoryRevision string
	Outcome            string
	Procedure          []string
	ValidationEvidence []string
	PromptArtifact     Artifact
	OutputArtifact     Artifact
	AutoApprove        bool
}

type Artifact struct {
	SHA256    string `json:"sha256"`
	URI       string `json:"uri"`
	MediaType string `json:"media_type"`
	SizeBytes int64  `json:"size_bytes"`
}

type KnowledgeItem struct {
	ID                 string    `json:"id"`
	ProjectID          string    `json:"project_id"`
	WorkflowID         string    `json:"workflow_id,omitempty"`
	WorkflowStepID     string    `json:"workflow_step_id,omitempty"`
	Title              string    `json:"title"`
	Problem            string    `json:"problem"`
	Summary            string    `json:"summary"`
	Content            string    `json:"content"`
	Procedure          []string  `json:"procedure,omitempty"`
	ValidationEvidence []string  `json:"validation_evidence,omitempty"`
	TaskType           string    `json:"task_type"`
	Language           string    `json:"language,omitempty"`
	Tags               []string  `json:"tags,omitempty"`
	Status             string    `json:"status"`
	SourceGenerationID string    `json:"source_generation_id,omitempty"`
	Version            int       `json:"version"`
	CreatedAt          time.Time `json:"created_at"`
	ApprovedAt         time.Time `json:"approved_at,omitempty"`
}

func (k KnowledgeItem) RetrievalText() string {
	return k.Title + "\nProblem:\n" + k.Problem + "\nSummary:\n" + k.Summary +
		"\nProcedure:\n" + joinLines(k.Procedure) + "\nSolution:\n" + k.Content +
		"\nValidation:\n" + joinLines(k.ValidationEvidence)
}

func joinLines(values []string) string {
	result := ""
	for i, value := range values {
		if i > 0 {
			result += "\n"
		}
		result += value
	}
	return result
}

type SearchHit struct {
	KnowledgeItem
	Score float32 `json:"score"`
}

type VectorHit struct {
	ID    string
	Score float32
}

type ReviewRecord struct {
	ID                      string
	KnowledgeID             string
	WorkflowID              string
	WorkflowStepID          string
	Reviewer                string
	Provider                string
	Model                   string
	Verdict                 string
	Comments                string
	ImprovedContent         string
	ValidationEvidence      []string
	RawOutput               string
	ContextManifest         string
	ReviewArtifact          Artifact
	ContextManifestArtifact Artifact
	CreatedAt               time.Time
}

type OutboxEvent struct {
	ID          int64
	AggregateID string
	Topic       string
	Attempts    int
}

type SoftwareRepository struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	Name          string    `json:"name"`
	CanonicalURL  string    `json:"canonical_url"`
	DefaultBranch string    `json:"default_branch,omitempty"`
	Revision      string    `json:"revision,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type RepositoryRelation struct {
	ID           string             `json:"id"`
	ProjectID    string             `json:"project_id"`
	From         SoftwareRepository `json:"from"`
	To           SoftwareRepository `json:"to"`
	RelationType string             `json:"relation_type"`
	Evidence     string             `json:"evidence"`
	Confidence   float32            `json:"confidence"`
	ApprovedBy   string             `json:"approved_by"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
	Score        float32            `json:"score,omitempty"`
}

type KnowledgeRelation struct {
	ID           string  `json:"id"`
	ProjectID    string  `json:"project_id"`
	FromID       string  `json:"from_id"`
	ToID         string  `json:"to_id"`
	RelationType string  `json:"relation_type"`
	Confidence   float32 `json:"confidence"`
}

type KnowledgeCodeReference struct {
	ID            string `json:"id"`
	ProjectID     string `json:"project_id"`
	KnowledgeID   string `json:"knowledge_id"`
	EntityID      string `json:"entity_id"`
	AnalysisRunID string `json:"analysis_run_id"`
	Role          string `json:"role"`
	Evidence      string `json:"evidence,omitempty"`
}

func (r RepositoryRelation) RetrievalText() string {
	return "Repository relationship: " + r.From.Name + " (" + r.From.CanonicalURL + ") " +
		r.RelationType + " " + r.To.Name + " (" + r.To.CanonicalURL + "). Evidence: " + r.Evidence
}

type CodeAnalysis struct {
	ID              string             `json:"id"`
	ProjectID       string             `json:"project_id"`
	Repository      SoftwareRepository `json:"repository"`
	Branch          string             `json:"branch"`
	Revision        string             `json:"revision"`
	Analyzer        string             `json:"analyzer"`
	AnalyzerVersion string             `json:"analyzer_version"`
	RequestedBy     string             `json:"requested_by"`
	EntityCount     int                `json:"entity_count"`
	RelationCount   int                `json:"relation_count"`
	StartedAt       time.Time          `json:"started_at"`
	CompletedAt     time.Time          `json:"completed_at"`
}

type CodeLocation struct {
	FilePath    string `json:"file_path,omitempty"`
	StartLine   int    `json:"start_line,omitempty"`
	StartColumn int    `json:"start_column,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`
	EndColumn   int    `json:"end_column,omitempty"`
}

type CodeEntity struct {
	ID             string            `json:"id"`
	ProjectID      string            `json:"project_id"`
	RepositoryID   string            `json:"repository_id"`
	RepositoryName string            `json:"repository_name"`
	AnalysisRunID  string            `json:"analysis_run_id"`
	Branch         string            `json:"branch"`
	Revision       string            `json:"revision"`
	StableKey      string            `json:"stable_key"`
	Language       string            `json:"language"`
	Kind           string            `json:"kind"`
	Name           string            `json:"name"`
	QualifiedName  string            `json:"qualified_name"`
	Signature      string            `json:"signature,omitempty"`
	ContentHash    string            `json:"content_hash,omitempty"`
	Location       CodeLocation      `json:"location"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	Score          float32           `json:"score,omitempty"`
}

func (e CodeEntity) RetrievalText() string {
	text := "Code entity: " + e.QualifiedName + "\nRepository: " + e.RepositoryName +
		"\nBranch: " + e.Branch + "\nRevision: " + e.Revision +
		"\nLanguage: " + e.Language + "\nKind: " + e.Kind
	if e.Signature != "" {
		text += "\nSignature: " + e.Signature
	}
	if e.Location.FilePath != "" {
		text += "\nSource: " + e.Location.FilePath
	}
	if documentation := strings.TrimSpace(e.Metadata["documentation"]); documentation != "" {
		text += "\nDocumentation: " + documentation
	}
	return text
}

type CodeRelation struct {
	ID            string            `json:"id"`
	AnalysisRunID string            `json:"analysis_run_id"`
	SourceID      string            `json:"source_id"`
	TargetID      string            `json:"target_id"`
	RelationType  string            `json:"relation_type"`
	Evidence      string            `json:"evidence,omitempty"`
	Confidence    float32           `json:"confidence"`
	Location      CodeLocation      `json:"location"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type CodeGraph struct {
	Analysis  CodeAnalysis   `json:"analysis"`
	Entities  []CodeEntity   `json:"entities"`
	Relations []CodeRelation `json:"relations"`
}

const (
	GraphNodeRepository    = "repository"
	GraphNodeCodeEntity    = "code_entity"
	GraphNodeKnowledgeItem = "knowledge_item"
)

type GraphNode struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"`
	Distance int     `json:"distance"`
	Score    float32 `json:"score,omitempty"`
}

type GraphEdge struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	SourceID   string  `json:"source_id"`
	SourceType string  `json:"source_type"`
	TargetID   string  `json:"target_id"`
	TargetType string  `json:"target_type"`
	Evidence   string  `json:"evidence,omitempty"`
	Confidence float32 `json:"confidence"`
}

type KnowledgeSubgraph struct {
	Backend      string               `json:"backend"`
	Nodes        []GraphNode          `json:"nodes"`
	Edges        []GraphEdge          `json:"edges"`
	Repositories []SoftwareRepository `json:"repositories,omitempty"`
	CodeEntities []CodeEntity         `json:"code_entities,omitempty"`
	Knowledge    []KnowledgeItem      `json:"knowledge_items,omitempty"`
	Truncated    bool                 `json:"truncated"`
}

type SemanticGraphEdge struct {
	GraphEdge
	ProjectID    string `json:"project_id"`
	RepositoryID string `json:"repository_id,omitempty"`
	SourceLabel  string `json:"source_label,omitempty"`
	TargetLabel  string `json:"target_label,omitempty"`
}

func (e SemanticGraphEdge) RetrievalText() string {
	source, target := e.SourceLabel, e.TargetLabel
	if source == "" {
		source = e.SourceID
	}
	if target == "" {
		target = e.TargetID
	}
	text := "Graph relationship: " + e.SourceType + " " + source + " " + e.Type + " " + e.TargetType + " " + target
	if e.Evidence != "" {
		text += ". Evidence: " + e.Evidence
	}
	return text
}

type GraphProjectionSnapshot struct {
	Repositories            []SoftwareRepository
	RepositoryRelations     []RepositoryRelation
	CodeGraphs              []CodeGraph
	Knowledge               []KnowledgeItem
	KnowledgeRelations      []KnowledgeRelation
	KnowledgeCodeReferences []KnowledgeCodeReference
}
