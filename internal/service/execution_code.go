package service

import (
	"context"
	"encoding/json"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
	"path"
	"slices"
	"strings"
)

// Accepted code bindings automatically bridge product/BRS context to the
// active PostgreSQL code graph. Similarity cannot select another revision.
func (s *Service) executionCode(ctx context.Context, run domain.ExecutionRun) ([]domain.ExecutionCodeBridge, error) {
	out := []domain.ExecutionCodeBridge{}
	for _, bound := range run.Bindings {
		if bound.Kind != "code" {
			continue
		}
		record, err := s.GetProductRecord(ctx, run.ProjectID, bound.RecordID, true)
		if err != nil {
			return nil, err
		}
		if record.SHA256 != bound.SHA256 {
			return nil, domain.ErrVersionConflict
		}
		var b domain.ProductCodeBinding
		if execution.DecodeProposal([]byte(record.Content), &b) != nil || b.Schema != "hybrid-ai/product-code/v1" || b.RepositoryID != run.Target.RepositoryID || len(b.Revision) != 40 || b.File == "" || path.IsAbs(b.File) || path.Clean(b.File) != b.File || strings.HasPrefix(b.File, "../") || len(b.Symbols) < 1 || len(b.Symbols) > 5 {
			return nil, domain.ErrValidationRequired
		}
		if run.Kind == "feature" && b.Revision != run.RepositoryRevision {
			return nil, domain.ErrVersionConflict
		}
		for _, pattern := range run.Target.ProtectedPaths {
			match, _ := path.Match(pattern, b.File)
			if match || b.File == pattern || strings.HasPrefix(b.File, strings.TrimSuffix(pattern, "/")+"/") {
				return nil, ErrForbidden
			}
		}
		for _, symbol := range b.Symbols {
			if len(symbol) < 1 || len(symbol) > 512 {
				return nil, ErrInvalidInput
			}
			graph, err := s.repository.GetCodeGraph(ctx, run.ProjectID, b.RepositoryID, symbol, 1)
			if err != nil {
				return nil, domain.ErrEvidenceUnavailable
			}
			if graph.Analysis.Revision != b.Revision || len(graph.Entities) > 20 || len(graph.Relations) > 50 {
				return nil, domain.ErrVersionConflict
			}
			found := false
			allowed := map[string]bool{}
			entities := []domain.CodeEntity{}
			for _, entity := range graph.Entities {
				if entity.ProjectID != run.ProjectID || entity.Revision != b.Revision || entity.RepositoryID != graph.Analysis.Repository.ID {
					return nil, domain.ErrVersionConflict
				}
				if entity.Location.FilePath != b.File {
					continue
				}
				if entity.Name == symbol || entity.QualifiedName == symbol || entity.StableKey == symbol || entity.ID == symbol {
					found = true
				}
				allowed[entity.ID] = true
				entities = append(entities, entity)
			}
			if !found {
				return nil, domain.ErrEvidenceUnavailable
			}
			edges := []domain.CodeRelation{}
			for _, edge := range graph.Relations {
				if allowed[edge.SourceID] && allowed[edge.TargetID] {
					edges = append(edges, edge)
				}
			}
			graph.Entities = entities
			graph.Relations = edges
			raw, _ := json.Marshal(graph)
			if len(raw) > 16384 {
				return nil, domain.ErrBudgetExhausted
			}
			out = append(out, domain.ExecutionCodeBridge{Record: bound, Binding: b, Graph: graph})
		}
	}
	return out, nil
}
func executionLinks(c ExecutionContext) []domain.ExecutionLink {
	links := []domain.ExecutionLink{}
	for _, record := range c.Intent.Specification.Bindings {
		links = append(links, domain.ExecutionLink{From: c.Intent.Intent.RecordID, Relation: "requires", To: record.RecordID, EvidenceSHA256: record.SHA256})
	}
	for _, criterion := range c.Intent.Specification.Criteria {
		links = append(links, domain.ExecutionLink{From: c.Intent.Intent.RecordID, Relation: "acceptance_criterion", To: criterion.ID, EvidenceSHA256: c.Intent.Intent.SHA256})
	}
	for _, bridge := range c.Code {
		for _, entity := range bridge.Graph.Entities {
			links = append(links, domain.ExecutionLink{From: bridge.Record.RecordID, Relation: "implemented_by", To: entity.ID, EvidenceSHA256: bridge.Record.SHA256})
		}
	}
	for _, step := range c.PriorSteps {
		if step.Result == nil {
			continue
		}
		for _, criterion := range step.Result.Criteria {
			links = append(links, domain.ExecutionLink{From: step.ID, Relation: "evaluates", To: criterion, EvidenceSHA256: step.Evidence.SHA256})
		}
		for _, effect := range step.Result.Effects {
			links = append(links, domain.ExecutionLink{From: step.ID, Relation: effect.Kind, To: effect.ExternalID, EvidenceSHA256: effect.Evidence.SHA256})
		}
	}
	slices.SortFunc(links, func(a, b domain.ExecutionLink) int {
		rawA, _ := json.Marshal(a)
		rawB, _ := json.Marshal(b)
		return strings.Compare(string(rawA), string(rawB))
	})
	return links
}
