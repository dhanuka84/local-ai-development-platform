package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhanuka84/hybrid-ai-platform/components/codegraph"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

var ErrInvalidInput = errors.New("invalid input")

type Service struct {
	repository      domain.Repository
	artifacts       domain.ArtifactStore
	embedder        domain.Embedder
	vectors         domain.VectorStore
	lexicalFallback bool
	autoApprove     bool
	codeAnalyzer    codegraph.Analyzer
	codeRoots       []string
	codeLimits      CodeGraphLimits
}

type CodeGraphLimits struct {
	MaxFiles     int
	MaxEntities  int
	MaxRelations int
}

func New(repository domain.Repository, artifacts domain.ArtifactStore, embedder domain.Embedder, vectors domain.VectorStore, lexicalFallback, autoApprove bool) *Service {
	return &Service{
		repository: repository, artifacts: artifacts, embedder: embedder, vectors: vectors,
		lexicalFallback: lexicalFallback, autoApprove: autoApprove,
	}
}

func (s *Service) CodeGraphAnalysisEnabled() bool {
	return s != nil && s.codeAnalyzer != nil
}

// ConfigureCodeGraph enables repository analysis and resolves the allowlist up
// front. Symlink resolution is repeated for each requested repository so a
// later symlink swap cannot escape an allowed root.
func (s *Service) ConfigureCodeGraph(analyzer codegraph.Analyzer, allowedRoots []string, limits CodeGraphLimits) error {
	if analyzer == nil {
		return errors.New("code graph analyzer is required")
	}
	if limits.MaxFiles < 1 || limits.MaxEntities < 1 || limits.MaxRelations < 1 {
		return errors.New("code graph limits must be positive")
	}
	resolved := make([]string, 0, len(allowedRoots))
	for _, root := range allowedRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		absolute, err := filepath.Abs(root)
		if err != nil {
			return fmt.Errorf("resolve allowed code root %q: %w", root, err)
		}
		absolute, err = filepath.EvalSymlinks(absolute)
		if err != nil {
			return fmt.Errorf("resolve allowed code root %q: %w", root, err)
		}
		info, err := os.Stat(absolute)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("allowed code root %q is not a directory", root)
		}
		resolved = append(resolved, filepath.Clean(absolute))
	}
	if len(resolved) == 0 {
		return errors.New("at least one allowed code root is required")
	}
	s.codeAnalyzer = analyzer
	s.codeRoots = resolved
	s.codeLimits = limits
	return nil
}

type CodeIndexInput struct {
	ProjectID      string
	Repository     domain.SoftwareRepository
	RepositoryPath string
	Revision       string
	AllowDirty     bool
	RequestedBy    string
}

func (s *Service) IndexCodeRepository(ctx context.Context, input CodeIndexInput) (domain.CodeAnalysis, error) {
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.Repository.Name = strings.TrimSpace(input.Repository.Name)
	input.Repository.CanonicalURL = strings.TrimSpace(input.Repository.CanonicalURL)
	input.Repository.DefaultBranch = strings.TrimSpace(input.Repository.DefaultBranch)
	input.Revision = strings.TrimSpace(input.Revision)
	input.RequestedBy = strings.TrimSpace(input.RequestedBy)
	if input.ProjectID == "" || input.Repository.Name == "" || input.Repository.CanonicalURL == "" ||
		strings.TrimSpace(input.RepositoryPath) == "" || input.RequestedBy == "" {
		return domain.CodeAnalysis{}, fmt.Errorf("%w: project_id, repository name and canonical_url, repository_path, and requested_by are required", ErrInvalidInput)
	}
	if s.codeAnalyzer == nil {
		return domain.CodeAnalysis{}, errors.New("code graph analyzer is not configured")
	}
	path, err := s.allowedRepositoryPath(input.RepositoryPath)
	if err != nil {
		return domain.CodeAnalysis{}, err
	}
	snapshot, err := s.codeAnalyzer.Analyze(ctx, codegraph.Request{
		RepositoryPath: path,
		Revision:       input.Revision,
		AllowDirty:     input.AllowDirty,
		MaxFiles:       s.codeLimits.MaxFiles,
		MaxEntities:    s.codeLimits.MaxEntities,
		MaxRelations:   s.codeLimits.MaxRelations,
	})
	if err != nil {
		return domain.CodeAnalysis{}, fmt.Errorf("analyze repository: %w", err)
	}
	return s.repository.StoreCodeGraph(ctx, input.ProjectID, input.Repository, input.RequestedBy, snapshot)
}

func (s *Service) allowedRepositoryPath(input string) (string, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(input))
	if err != nil {
		return "", fmt.Errorf("%w: resolve repository path: %v", ErrInvalidInput, err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("%w: resolve repository path: %v", ErrInvalidInput, err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%w: repository_path is not a directory", ErrInvalidInput)
	}
	for _, root := range s.codeRoots {
		relative, err := filepath.Rel(root, resolved)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return filepath.Clean(resolved), nil
		}
	}
	return "", fmt.Errorf("%w: repository_path is outside CODEGRAPH_ALLOWED_ROOTS", ErrInvalidInput)
}

func (s *Service) SearchCodeEntities(ctx context.Context, projectID, repositoryID, query string, limit int) ([]domain.CodeEntity, string, error) {
	projectID, repositoryID, query = strings.TrimSpace(projectID), strings.TrimSpace(repositoryID), strings.TrimSpace(query)
	if projectID == "" || query == "" {
		return nil, "", fmt.Errorf("%w: project_id and query are required", ErrInvalidInput)
	}
	if limit < 1 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	embeddings, embedErr := s.embedder.Embed(ctx, []string{query})
	if embedErr == nil && len(embeddings) == 1 {
		candidateLimit := limit * 5
		if candidateLimit > 100 {
			candidateLimit = 100
		}
		vectorHits, searchErr := s.vectors.SearchCodeEntities(ctx, projectID, repositoryID, embeddings[0], candidateLimit)
		if searchErr == nil {
			ids := make([]string, 0, len(vectorHits))
			for _, hit := range vectorHits {
				ids = append(ids, hit.ID)
			}
			entities, hydrateErr := s.repository.GetCodeEntitiesMany(ctx, ids)
			if hydrateErr == nil {
				byID := make(map[string]domain.CodeEntity, len(entities))
				for _, entity := range entities {
					byID[entity.ID] = entity
				}
				result := make([]domain.CodeEntity, 0, limit)
				for _, hit := range vectorHits {
					entity, ok := byID[hit.ID]
					if !ok || (repositoryID != "" && entity.RepositoryID != repositoryID) {
						continue
					}
					entity.Score = hit.Score
					result = append(result, entity)
					if len(result) == limit {
						break
					}
				}
				if len(result) > 0 {
					return result, "milvus", nil
				}
			}
		}
	}
	if !s.lexicalFallback {
		if embedErr != nil {
			return nil, "", fmt.Errorf("vector search unavailable: %w", embedErr)
		}
		return nil, "", errors.New("vector search unavailable")
	}
	entities, err := s.repository.SearchCodeEntitiesLexical(ctx, projectID, repositoryID, query, limit)
	return entities, "postgres-lexical-fallback", err
}

func (s *Service) CodeGraph(ctx context.Context, projectID, repositoryRoot, symbolRoot string, depth int) (domain.CodeGraph, error) {
	projectID, repositoryRoot, symbolRoot = strings.TrimSpace(projectID), strings.TrimSpace(repositoryRoot), strings.TrimSpace(symbolRoot)
	if projectID == "" || repositoryRoot == "" || symbolRoot == "" {
		return domain.CodeGraph{}, fmt.Errorf("%w: project_id, repository, and symbol are required", ErrInvalidInput)
	}
	if depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}
	return s.repository.GetCodeGraph(ctx, projectID, repositoryRoot, symbolRoot, depth)
}

type CaptureInput struct {
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
}

func (s *Service) Capture(ctx context.Context, input CaptureInput) (domain.KnowledgeItem, error) {
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.Response = strings.TrimSpace(input.Response)
	if input.ProjectID == "" || input.Prompt == "" || input.Response == "" {
		return domain.KnowledgeItem{}, fmt.Errorf("%w: project_id, prompt, and response are required", ErrInvalidInput)
	}
	generationID, err := domain.NewID()
	if err != nil {
		return domain.KnowledgeItem{}, err
	}
	promptArtifact, err := s.artifacts.Put(ctx, []byte(input.Prompt), "text/plain; charset=utf-8")
	if err != nil {
		return domain.KnowledgeItem{}, err
	}
	outputArtifact, err := s.artifacts.Put(ctx, []byte(input.Response), "text/plain; charset=utf-8")
	if err != nil {
		return domain.KnowledgeItem{}, err
	}
	return s.repository.RecordGeneration(ctx, domain.GenerationCapture{
		ID: generationID, ProjectID: input.ProjectID, SessionID: strings.TrimSpace(input.SessionID),
		TaskType: strings.TrimSpace(input.TaskType), Prompt: input.Prompt, Response: input.Response,
		Summary: strings.TrimSpace(input.Summary), Language: strings.TrimSpace(input.Language),
		Tags: cleanTags(input.Tags), Provider: strings.TrimSpace(input.Provider), Model: strings.TrimSpace(input.Model),
		RepositoryRevision: strings.TrimSpace(input.RepositoryRevision), Outcome: strings.TrimSpace(input.Outcome),
		Procedure: cleanList(input.Procedure), ValidationEvidence: cleanList(input.ValidationEvidence),
		PromptArtifact: promptArtifact, OutputArtifact: outputArtifact, AutoApprove: s.autoApprove,
	})
}

func (s *Service) Search(ctx context.Context, projectID, query string, limit int) ([]domain.SearchHit, string, error) {
	projectID, query = strings.TrimSpace(projectID), strings.TrimSpace(query)
	if projectID == "" || query == "" {
		return nil, "", fmt.Errorf("%w: project_id and query are required", ErrInvalidInput)
	}
	if limit < 1 {
		limit = 5
	}
	if limit > 25 {
		limit = 25
	}

	embeddings, embedErr := s.embedder.Embed(ctx, []string{query})
	if embedErr == nil && len(embeddings) == 1 {
		vectorHits, searchErr := s.vectors.Search(ctx, projectID, embeddings[0], limit)
		if searchErr == nil {
			ids := make([]string, 0, len(vectorHits))
			for _, hit := range vectorHits {
				ids = append(ids, hit.ID)
			}
			items, err := s.repository.GetKnowledgeMany(ctx, ids)
			if err == nil {
				byID := make(map[string]domain.KnowledgeItem, len(items))
				for _, item := range items {
					byID[item.ID] = item
				}
				result := make([]domain.SearchHit, 0, len(vectorHits))
				for _, hit := range vectorHits {
					if item, ok := byID[hit.ID]; ok && item.Status == domain.CandidateApproved {
						result = append(result, domain.SearchHit{KnowledgeItem: item, Score: hit.Score})
					}
				}
				return result, "milvus", nil
			}
		}
	}
	if !s.lexicalFallback {
		if embedErr != nil {
			return nil, "", fmt.Errorf("vector search unavailable: %w", embedErr)
		}
		return nil, "", errors.New("vector search unavailable")
	}
	hits, err := s.repository.SearchApprovedLexical(ctx, projectID, query, limit)
	return hits, "postgres-lexical-fallback", err
}

func (s *Service) Get(ctx context.Context, id string, includePending bool) (domain.KnowledgeItem, error) {
	return s.repository.GetKnowledge(ctx, strings.TrimSpace(id), includePending)
}

func (s *Service) ListCandidates(ctx context.Context, projectID string, limit int) ([]domain.KnowledgeItem, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	return s.repository.ListCandidates(ctx, strings.TrimSpace(projectID), limit)
}

func (s *Service) Approve(ctx context.Context, id, actor string) (domain.KnowledgeItem, error) {
	if strings.TrimSpace(actor) == "" {
		return domain.KnowledgeItem{}, fmt.Errorf("%w: actor is required", ErrInvalidInput)
	}
	return s.repository.ApproveCandidate(ctx, strings.TrimSpace(id), strings.TrimSpace(actor))
}

func (s *Service) Reject(ctx context.Context, id, actor string) (domain.KnowledgeItem, error) {
	if strings.TrimSpace(actor) == "" {
		return domain.KnowledgeItem{}, fmt.Errorf("%w: actor is required", ErrInvalidInput)
	}
	return s.repository.RejectCandidate(ctx, strings.TrimSpace(id), strings.TrimSpace(actor))
}

func (s *Service) RecordReview(ctx context.Context, review domain.ReviewRecord) error {
	if strings.TrimSpace(review.KnowledgeID) == "" || strings.TrimSpace(review.Reviewer) == "" || strings.TrimSpace(review.Verdict) == "" {
		return fmt.Errorf("%w: knowledge_id, reviewer, and verdict are required", ErrInvalidInput)
	}
	var err error
	review.ID, err = domain.NewID()
	if err != nil {
		return err
	}
	return s.repository.RecordReview(ctx, review)
}

func (s *Service) Dependencies(ctx context.Context) map[string]string {
	status := map[string]string{"postgres": "ok", "ollama": "ok", "milvus": "ok"}
	if err := s.repository.Ping(ctx); err != nil {
		status["postgres"] = "unavailable"
	}
	if err := s.embedder.Ping(ctx); err != nil {
		status["ollama"] = "unavailable"
	}
	if err := s.vectors.Ping(ctx); err != nil {
		status["milvus"] = "unavailable"
	}
	return status
}

func (s *Service) UpsertRepositoryRelation(ctx context.Context, relation domain.RepositoryRelation) (domain.RepositoryRelation, error) {
	relation.ProjectID = strings.TrimSpace(relation.ProjectID)
	relation.From.Name = strings.TrimSpace(relation.From.Name)
	relation.From.CanonicalURL = strings.TrimSpace(relation.From.CanonicalURL)
	relation.To.Name = strings.TrimSpace(relation.To.Name)
	relation.To.CanonicalURL = strings.TrimSpace(relation.To.CanonicalURL)
	relation.RelationType = strings.ToLower(strings.TrimSpace(relation.RelationType))
	relation.Evidence = strings.TrimSpace(relation.Evidence)
	relation.ApprovedBy = strings.TrimSpace(relation.ApprovedBy)
	if relation.ProjectID == "" || relation.From.Name == "" || relation.From.CanonicalURL == "" ||
		relation.To.Name == "" || relation.To.CanonicalURL == "" || relation.Evidence == "" || relation.ApprovedBy == "" {
		return domain.RepositoryRelation{}, fmt.Errorf("%w: project, both repositories, evidence, and approved_by are required", ErrInvalidInput)
	}
	if !validRelationType(relation.RelationType) {
		return domain.RepositoryRelation{}, fmt.Errorf("%w: unsupported repository relation type %q", ErrInvalidInput, relation.RelationType)
	}
	if relation.Confidence < 0 || relation.Confidence > 1 {
		return domain.RepositoryRelation{}, fmt.Errorf("%w: confidence must be between 0 and 1", ErrInvalidInput)
	}
	return s.repository.UpsertRepositoryRelation(ctx, relation)
}

func (s *Service) RepositoryGraph(ctx context.Context, projectID, root string, depth int) ([]domain.RepositoryRelation, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("%w: project_id and root are required", ErrInvalidInput)
	}
	if depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}
	return s.repository.GetRepositoryGraph(ctx, strings.TrimSpace(projectID), strings.TrimSpace(root), depth)
}

func (s *Service) SearchRepositoryRelations(ctx context.Context, projectID, query string, limit int) ([]domain.RepositoryRelation, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("%w: project_id and query are required", ErrInvalidInput)
	}
	if limit < 1 {
		limit = 5
	}
	if limit > 25 {
		limit = 25
	}
	embeddings, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	hits, err := s.vectors.SearchRelations(ctx, projectID, embeddings[0], limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(hits))
	for _, hit := range hits {
		ids = append(ids, hit.ID)
	}
	relations, err := s.repository.GetRepositoryRelationsMany(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.RepositoryRelation, len(relations))
	for _, relation := range relations {
		byID[relation.ID] = relation
	}
	result := make([]domain.RepositoryRelation, 0, len(hits))
	for _, hit := range hits {
		if relation, ok := byID[hit.ID]; ok {
			relation.Score = hit.Score
			result = append(result, relation)
		}
	}
	return result, nil
}

func validRelationType(value string) bool {
	switch value {
	case "depends_on", "provides_api_to", "deploys_with", "shares_contract", "fork_of", "upstream_of", "successor_of", "contains", "related_to":
		return true
	default:
		return false
	}
}

func cleanTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	return result
}

func cleanList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}
