package domain

import "context"

const QualityTextLimit = 64 << 10

type SimilarKnowledge struct {
	KnowledgeID       string  `json:"knowledge_id"`
	Version           int     `json:"version"`
	ContentSHA256     string  `json:"content_sha256"`
	LexicalSimilarity float64 `json:"lexical_similarity"`
}

type CandidateQualityAssessment struct {
	KnowledgeID        string             `json:"knowledge_id"`
	Version            int                `json:"version"`
	ContentSHA256      string             `json:"content_sha256"`
	Flags              []string           `json:"flags"`
	PossibleDuplicates []SimilarKnowledge `json:"possible_duplicates"`
	Scanned            int                `json:"scanned"`
	Limit              int                `json:"candidate_limit"`
	Algorithm          string             `json:"algorithm"`
	Coverage           string             `json:"coverage"`
}

type TextQualityRepository interface {
	FindQualityCandidates(context.Context, string, string, int) ([]KnowledgeItem, error)
}
