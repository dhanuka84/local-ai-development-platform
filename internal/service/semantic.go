package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dhanuka84/hybrid-ai-platform/contracts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/contextregistry"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	"go.opentelemetry.io/otel/trace"
	"strings"
	"time"
)

func (s *Service) semantic() (domain.SemanticRepository, error) {
	r, ok := s.repository.(domain.SemanticRepository)
	if !ok {
		return nil, domain.ErrEvidenceUnavailable
	}
	return r, nil
}
func (s *Service) ValidateContextRegistry(ctx context.Context, project string) (domain.RegistryValidation, error) {
	v := domain.RegistryValidation{ProjectID: project}
	p, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return v, err
	}
	if !p.Human || !p.HasRole(project, "qa") {
		return v, ErrForbidden
	}
	if _, err = s.AuthorizeProjectAction(ctx, project, "knowledge_candidate", "context-registry", "validate", nil); err != nil {
		return v, err
	}
	r, err := s.semantic()
	if err != nil {
		return v, err
	}
	if err = contracts.Check(); err != nil {
		return v, err
	}
	defs, sha, err := contextregistry.Load()
	if err != nil {
		return v, err
	}
	v.ID, err = domain.NewID()
	if err != nil {
		return v, err
	}
	v.RegistrySHA256 = sha
	v.Actor = p.ID
	v.CompletedAt = time.Now().UTC()
	raw, _ := json.Marshal(map[string]any{"schema": "hybrid-ai/context-registry-validation/v1", "registry_sha256": sha, "actor": p.ID, "checks": []string{"embedded JSON schemas and positive/negative fixtures passed", "metric IDs bound to compiled SQL allowlist"}, "scope": "contract and SQL preparation validation; not an integration test report", "completed_at": v.CompletedAt})
	v.Evidence, err = s.artifacts.Put(ctx, raw, "application/json")
	if err != nil {
		return v, err
	}
	// PostgreSQL EXPLAINs every allowlisted statement and commits the validation
	// only if query preparation succeeds. Nothing here approves definitions.
	return v, r.ValidateContextRegistry(ctx, v, defs)
}
func (s *Service) DecideContextDefinition(ctx context.Context, in domain.DefinitionDecision) (domain.GovernedDefinition, error) {
	p, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return domain.GovernedDefinition{}, err
	}
	if !p.Human || !p.HasRole(in.ProjectID, "product_owner") {
		return domain.GovernedDefinition{}, ErrForbidden
	}
	if _, err = s.AuthorizeProjectAction(ctx, in.ProjectID, "knowledge_candidate", in.DefinitionID, "decide_validated", nil); err != nil {
		return domain.GovernedDefinition{}, err
	}
	r, err := s.semantic()
	if err != nil {
		return domain.GovernedDefinition{}, err
	}
	in.Actor = p.ID
	return r.DecideContextDefinition(ctx, in)
}
func (s *Service) PlatformMetric(ctx context.Context, in domain.MetricRequest) (out domain.MetricResult, err error) {
	if err = contextregistry.ValidateRequest(in); err != nil {
		return out, err
	}
	p, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return out, err
	}
	if _, ok := selectRole(p, in.ProjectID, []string{"operations", "product_owner", "qa", "controller", "development"}); !ok {
		return out, ErrForbidden
	}
	if _, err = s.AuthorizeProjectAction(ctx, in.ProjectID, "knowledge_candidate", in.MetricID, "read", nil); err != nil {
		return out, err
	}
	r, err := s.semantic()
	if err != nil {
		return out, err
	}
	_, sha, err := contextregistry.Load()
	if err != nil {
		return out, err
	}
	ctx, finish, err := s.BeginOperation(ctx, "platform.metric.query", domain.OperationScope{ProjectID: in.ProjectID}, domain.EvidenceReference{Kind: "definition", ID: in.MetricID, Version: in.Version})
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, finish(err)) }()
	out, err = r.QueryPlatformMetric(ctx, in, sha)
	out.TraceID = trace.SpanContextFromContext(ctx).TraceID().String()
	return out, err
}
func (s *Service) SearchContextDefinitions(ctx context.Context, project, query string, limit int) ([]domain.GovernedDefinition, error) {
	if len(strings.TrimSpace(query)) == 0 || len(query) > 4096 || limit < 1 || limit > 50 {
		return nil, ErrInvalidInput
	}
	p, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := selectRole(p, project, []string{"operations", "product_owner", "qa", "controller", "development"}); !ok {
		return nil, ErrForbidden
	}
	if _, err = s.AuthorizeProjectAction(ctx, project, "knowledge_candidate", "context-definitions", "read", nil); err != nil {
		return nil, err
	}
	r, err := s.semantic()
	if err != nil {
		return nil, err
	}
	vectors, ok := s.vectors.(domain.SemanticVectorStore)
	if !ok {
		return nil, domain.ErrEvidenceUnavailable
	}
	model, ok := s.embedder.(interface{ EmbeddingIdentity() (string, string, int) })
	if !ok {
		return nil, domain.ErrQualityBlocked
	}
	provider, modelName, _ := model.EmbeddingIdentity()
	if provider != "ollama" {
		return nil, ErrForbidden
	}
	ctx = domain.WithOperationScope(ctx, domain.OperationScope{ProjectID: project})
	embeddings, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(embeddings) != 1 {
		return nil, domain.ErrQualityBlocked
	}
	hits, err := vectors.SearchDefinitions(ctx, project, embeddings[0], limit)
	if err != nil {
		return nil, err
	}
	// Milvus IDs and metadata are untrusted. Hydrate the project registry first.
	defs, err := r.ApprovedContextDefinitions(ctx, project)
	if err != nil {
		return nil, err
	}
	_, registrySHA, err := contextregistry.Load()
	if err != nil {
		return nil, err
	}
	byID := map[string]domain.GovernedDefinition{}
	for _, d := range defs {
		byID[d.VectorID()] = d
	}
	result := []domain.GovernedDefinition{}
	for _, h := range hits {
		d, ok := byID[h.ID]
		if !ok || !domain.FiniteScore(h.Score) || d.Version != h.Version || d.SHA256 != h.ContentSHA256 || d.RegistrySHA256 != registrySHA || time.Since(d.ValidatedAt) > 30*24*time.Hour || d.ProjectionVerifiedAt == nil || time.Since(*d.ProjectionVerifiedAt) > 24*time.Hour || d.ProjectionModel != modelName || d.ProjectionDimension != len(embeddings[0]) || h.ProjectionSHA256 != d.ProjectionDigest() {
			continue
		}
		result = append(result, d)
	}
	return result, nil
}

type RecordTaskContextInput struct {
	TaskID          string               `json:"task_id"`
	ExpectedVersion int                  `json:"expected_version"`
	Contexts        []domain.UsedContext `json:"contexts"`
	IdempotencyKey  string               `json:"idempotency_key"`
}

func (s *Service) RecordTaskContext(ctx context.Context, in RecordTaskContextInput) error {
	if in.ExpectedVersion < 1 || len(in.Contexts) == 0 || len(in.Contexts) > 50 || in.IdempotencyKey == "" {
		return ErrInvalidInput
	}
	task, err := s.GetWorkflowTask(ctx, in.TaskID)
	if err != nil {
		return err
	}
	p, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return err
	}
	if _, ok := selectRole(p, task.ProjectID, []string{"development", "controller"}); !ok {
		return ErrForbidden
	}
	ctx = domain.WithPurpose(ctx, domain.TaskPurpose(task.TaskType))
	ctx = domain.WithOperationScope(ctx, domain.OperationScope{ProjectID: task.ProjectID, WorkflowID: task.WorkflowID, TaskID: task.ID})
	if task.State != domain.TaskStateLocalExecution || task.Version != in.ExpectedVersion {
		return domain.ErrVersionConflict
	}
	governance, err := s.governance()
	if err != nil {
		return err
	}
	quality, ok := s.repository.(domain.KnowledgeQualityRepository)
	if !ok {
		return domain.ErrEvidenceUnavailable
	}
	for _, use := range in.Contexts {
		useCtx := ctx
		if use.TargetRepository != "" {
			useCtx = domain.WithPurpose(ctx, domain.PurposeCodeChange)
		}
		item, err := s.repository.GetKnowledge(useCtx, use.KnowledgeID, false)
		if err != nil {
			return err
		}
		if item.ProjectID != task.ProjectID || item.Version != use.Version {
			return domain.ErrQualityBlocked
		}
		q, err := quality.KnowledgeQuality(useCtx, item.ID)
		if err != nil {
			return err
		}
		report, err := governance.GetKnowledgeValidation(ctx, q.ValidationID)
		if err != nil {
			return err
		}
		if err = s.verifyValidationEvidence(ctx, report); err != nil {
			return err
		}
		hasRepository := false
		matched := false
		for _, source := range report.SourceManifest.Sources {
			if source.Kind == "repository" {
				hasRepository = true
				if source.Reference == use.TargetRepository && source.Branch == use.TargetBranch && (source.Revision == use.TargetRevision || source.ApplicableThrough != "") {
					matched = true
				}
			}
		}
		if (hasRepository && !matched) || (domain.TaskPurpose(task.TaskType) == domain.PurposeCodeChange && use.TargetRepository == "") {
			return domain.ErrQualityBlocked
		}
		if use.TargetRepository != "" {
			if err = s.verifySources(ctx, domain.SourceManifest{SchemaVersion: "hybrid-ai/knowledge-source/v1", Sources: []domain.KnowledgeSource{{Kind: "repository", Reference: use.TargetRepository, Branch: use.TargetBranch, Revision: use.TargetRevision}}}); err != nil {
				return err
			}
		}
	}
	r, err := s.semantic()
	if err != nil {
		return err
	}
	return r.RecordTaskContext(ctx, task.ID, in.ExpectedVersion, in.Contexts, p.ID, in.IdempotencyKey)
}
