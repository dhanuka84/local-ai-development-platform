package domain

import "time"

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
	SHA256    string
	URI       string
	MediaType string
	SizeBytes int64
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
	ID              string
	KnowledgeID     string
	Reviewer        string
	Provider        string
	Model           string
	Verdict         string
	Comments        string
	ImprovedContent string
	CreatedAt       time.Time
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
