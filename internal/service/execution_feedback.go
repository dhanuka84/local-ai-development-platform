package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type ExecutionImprovements struct {
	Schema    string                 `json:"schema"`
	RunID     string                 `json:"run_id"`
	Status    string                 `json:"status"`
	Knowledge domain.KnowledgeItem   `json:"knowledge"`
	Proposals []domain.ProductRecord `json:"proposals"`
	Evidence  domain.Artifact        `json:"evidence"`
}
type executionProposalRepository interface {
	FindExecutionProposal(context.Context, string, string, string) (domain.ProductRecord, error)
}

func (s *Service) ProposeExecutionImprovements(ctx context.Context, in ExecutionIDInput) (out ExecutionImprovements, err error) {
	view, err := s.GetExecution(ctx, in)
	if err != nil {
		return out, err
	}
	p, err := s.AuthorizeProjectAction(ctx, in.ProjectID, "sdlc_execution", in.RunID, "control", map[string]any{"owner": view.Run.Owner})
	if err != nil {
		return out, err
	}
	if !p.Human || p.ID != view.Run.Owner || view.Run.Status != "completed" {
		return out, domain.ErrValidationRequired
	}
	if _, err = s.AuthorizeProjectAction(ctx, in.ProjectID, "knowledge_candidate", "new", "capture", nil); err != nil {
		return out, err
	}
	intent, err := s.IntentContext(ctx, IntentContextInput{ProjectID: in.ProjectID, IntentID: view.Run.Intent.RecordID, ExpectedSHA256: view.Run.Intent.SHA256})
	if err != nil {
		return out, err
	}
	if !intent.Ready {
		return out, domain.ErrQualityBlocked
	}
	r, ok := s.repository.(executionProposalRepository)
	if !ok {
		return out, domain.ErrEvidenceUnavailable
	}
	raw, _ := json.Marshal(view)
	evidence, err := s.artifacts.Put(ctx, raw, "application/json")
	if err != nil {
		return out, err
	}
	out = ExecutionImprovements{Schema: "hybrid-ai/execution-improvements/v1", RunID: in.RunID, Status: "pending", Evidence: evidence, Proposals: []domain.ProductRecord{}}
	procedure := []string{}
	validation := []string{}
	provider, model, revision := "deterministic", "sdlc-outcome/v1", ""
	for _, step := range view.Steps {
		if step.Result == nil {
			continue
		}
		procedure = append(procedure, step.Stage+": "+step.Result.Summary)
		validation = append(validation, fmt.Sprintf("%s actor=%s outcome=%s evidence=%s", step.ID, step.Actor, step.Result.Outcome, step.Evidence.SHA256))
		if step.Result.Model != nil {
			validation = append(validation, "Underlying local model: "+step.Result.Model.Provider+":"+step.Result.Model.Model+"@"+step.Result.Model.ModelSHA256)
		}
		if step.Result.BaseRevision != "" {
			revision = step.Result.BaseRevision
		}
	}
	specifications := []struct{ key, kind, title, proposal string }{
		{"requirements", "brs", "Requirement refinement", "Review whether observed edge cases require a new business criterion. Preserve accepted intent until an exact replacement is validated and accepted."},
		{"tests", "test", "Regression test proposal", "Retain the failed and successful candidates and independent checks as regression cases. Add a separately reviewed held-out case before activating a package revision."},
		{"runbook", "procedure", "Operational procedure proposal", "Generalize the ordered validated procedure. Keep source completeness, competing explanations, action preconditions, technical probes and business reconciliation explicit."},
		{"package", "procedure", "Agent package improvement proposal", "Evaluate prompt, role tool and routing changes as a new version against success and failure cases. Activate after a package campaign passes; retain the prior qualified package for rollback."},
	}
	for _, spec := range specifications {
		content, _ := json.Marshal(map[string]any{"schema": "hybrid-ai/improvement-proposal/v1", "run_id": in.RunID, "intent": view.Run.Intent, "outcome_evidence": evidence.SHA256, "proposal": spec.proposal, "procedure": procedure, "validation_evidence": validation, "status": "pending"})
		key := "improve:" + in.RunID + ":" + spec.key
		prior, e := r.FindExecutionProposal(ctx, in.ProjectID, view.Run.ProductID, key)
		if e != nil {
			return out, e
		}
		if prior.ID != "" {
			if prior.Content != string(content) || prior.Source.SourceID != in.RunID {
				return out, domain.ErrVersionConflict
			}
			out.Proposals = append(out.Proposals, prior)
			continue
		}
		record, e := s.PutProductRecord(ctx, domain.ProductRecordInput{ProjectID: in.ProjectID, ProductID: view.Run.ProductID, Key: key, Kind: spec.kind, Title: spec.title, Content: string(content), Classification: "internal", SourceID: in.RunID, SourceRevision: evidence.SHA256})
		if e != nil {
			return out, e
		}
		out.Proposals = append(out.Proposals, record)
	}
	prompt, _ := json.Marshal(map[string]any{"schema": "hybrid-ai/outcome-learning/v1", "run_id": in.RunID, "intent": view.Run.Intent, "evidence_sha256": evidence.SHA256})
	response, _ := json.Marshal(map[string]any{"status": "pending", "scope": "Generalization requires exact candidate review. Fixture outcomes do not establish model quality or production readiness.", "procedure": procedure, "validation_evidence": validation})
	promptArtifact, err := s.artifacts.Put(ctx, prompt, "application/json")
	if err != nil {
		return out, err
	}
	responseArtifact, err := s.artifacts.Put(ctx, response, "application/json")
	if err != nil {
		return out, err
	}
	hash := domain.Digest([]byte("sdlc-learning/v1:" + in.ProjectID + ":" + in.RunID))
	generationID := hash[:8] + "-" + hash[8:12] + "-5" + hash[13:16] + "-a" + hash[17:20] + "-" + hash[20:32]
	out.Knowledge, err = s.repository.RecordGeneration(ctx, domain.GenerationCapture{ID: generationID, ProjectID: in.ProjectID, SessionID: in.RunID, TaskType: "maintenance", Prompt: string(prompt), Response: string(response), Summary: "Validated SDLC outcome awaiting generalized knowledge review", Provider: provider, Model: model, RepositoryRevision: revision, Outcome: "success", Procedure: procedure, ValidationEvidence: validation, Tags: []string{"sdlc", "pending-improvement"}, PromptArtifact: promptArtifact, OutputArtifact: responseArtifact, AutoApprove: false})
	return out, err
}
