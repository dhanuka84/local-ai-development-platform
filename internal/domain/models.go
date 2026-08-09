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

func (r RepositoryRelation) RetrievalText() string {
	return "Repository relationship: " + r.From.Name + " (" + r.From.CanonicalURL + ") " +
		r.RelationType + " " + r.To.Name + " (" + r.To.CanonicalURL + "). Evidence: " + r.Evidence
}

type CodeAnalysis struct {
	ID              string             `json:"id"`
	ProjectID       string             `json:"project_id"`
	Repository      SoftwareRepository `json:"repository"`
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
	ID            string            `json:"id"`
	ProjectID     string            `json:"project_id"`
	RepositoryID  string            `json:"repository_id"`
	AnalysisRunID string            `json:"analysis_run_id"`
	Revision      string            `json:"revision"`
	StableKey     string            `json:"stable_key"`
	Language      string            `json:"language"`
	Kind          string            `json:"kind"`
	Name          string            `json:"name"`
	QualifiedName string            `json:"qualified_name"`
	Signature     string            `json:"signature,omitempty"`
	ContentHash   string            `json:"content_hash,omitempty"`
	Location      CodeLocation      `json:"location"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	Score         float32           `json:"score,omitempty"`
}

func (e CodeEntity) RetrievalText() string {
	text := "Code entity: " + e.QualifiedName + "\nLanguage: " + e.Language + "\nKind: " + e.Kind
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
