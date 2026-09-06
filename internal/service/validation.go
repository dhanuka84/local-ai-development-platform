package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/components/workpacket"
	"github.com/dhanuka84/hybrid-ai-platform/contracts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	"github.com/dhanuka84/hybrid-ai-platform/internal/telemetry"
)

type ValidationInput struct {
	KnowledgeID     string                       `json:"knowledge_id"`
	ExpectedVersion int                          `json:"expected_version"`
	SourceManifest  domain.SourceManifest        `json:"source_manifest"`
	Criteria        []domain.ValidationCriterion `json:"criteria"`
}

type DecisionInput struct {
	KnowledgeID     string `json:"knowledge_id"`
	ExpectedVersion int    `json:"expected_version"`
	ValidationID    string `json:"validation_id,omitempty"`
	Decision        string `json:"decision"`
	Reason          string `json:"reason"`
	IdempotencyKey  string `json:"idempotency_key"`
}

func (s *Service) governance() (domain.KnowledgeGovernanceRepository, error) {
	repository, ok := s.repository.(domain.KnowledgeGovernanceRepository)
	if !ok {
		return nil, fmt.Errorf("%w: governance storage unavailable", domain.ErrEvidenceUnavailable)
	}
	return repository, nil
}

func (s *Service) validationCandidate(ctx context.Context, input ValidationInput) (domain.KnowledgeItem, domain.Principal, error) {
	return s.validationCandidateFor(ctx, input, false)
}

func (s *Service) validationCandidateFor(ctx context.Context, input ValidationInput, executed bool) (domain.KnowledgeItem, domain.Principal, error) {
	principal, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return domain.KnowledgeItem{}, principal, err
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return domain.KnowledgeItem{}, principal, err
	}
	if err := contracts.Validate("knowledge/v1/validation-input.schema.json", payload); err != nil {
		return domain.KnowledgeItem{}, principal, fmt.Errorf("%w: %v", domain.ErrValidationRequired, err)
	}
	item, err := s.repository.GetKnowledge(ctx, input.KnowledgeID, true)
	if err != nil {
		return item, principal, err
	}
	localExecutor := executed && !principal.Human && principal.HasRole(item.ProjectID, "validation_executor")
	if (!principal.Human || !principal.HasRole(item.ProjectID, "qa")) && !localExecutor {
		return item, principal, ErrForbidden
	}
	action := "validate"
	if localExecutor {
		action = "validate_local"
	}
	if _, err := s.AuthorizeProjectAction(ctx, item.ProjectID, "knowledge_candidate", item.ID, action, map[string]any{"status": item.Status}); err != nil {
		return item, principal, err
	}
	if input.ExpectedVersion < 1 || item.Version != input.ExpectedVersion || (item.Status != domain.CandidatePending && item.Status != domain.CandidateApproved) {
		return item, principal, domain.ErrVersionConflict
	}
	if err := input.SourceManifest.Validate(); err != nil {
		return item, principal, err
	}
	return item, principal, nil
}

func (s *Service) RecordManualValidation(ctx context.Context, input ValidationInput) (domain.KnowledgeValidation, error) {
	item, principal, err := s.validationCandidate(ctx, input)
	if err != nil {
		return domain.KnowledgeValidation{}, err
	}
	return s.recordValidation(ctx, item, principal, input, "manual", nil, time.Now().UTC())
}

// VerifyKnowledgePatch is intentionally not an MCP report-upload endpoint.
// The local QA CLI executes the bounded verifier and records its actual outputs.
func (s *Service) VerifyKnowledgePatch(ctx context.Context, input ValidationInput, packet workpacket.Packet, patch []byte) (report domain.KnowledgeValidation, err error) {
	item, _, err := s.validationCandidateFor(ctx, input, true)
	if err != nil {
		return report, err
	}
	scope := domain.OperationScope{ProjectID: item.ProjectID, WorkflowID: item.WorkflowID}
	if item.WorkflowID != "" {
		if task, lookupErr := s.repository.GetWorkflowTaskByCandidate(ctx, item.ID); lookupErr == nil {
			scope.TaskID = task.ID
		}
	}
	ctx, finish, err := s.BeginOperation(ctx, "local.verifier", scope, domain.EvidenceReference{Kind: "knowledge", ID: item.ID, Version: item.Version, SHA256: domain.Digest([]byte(item.Content))})
	if err != nil {
		return report, err
	}
	defer func() {
		refs := []domain.EvidenceReference{}
		if report.ID != "" {
			refs = append(refs, domain.EvidenceReference{Kind: "validation", ID: report.ID, Version: report.CandidateVersion}, domain.EvidenceReference{Kind: "artifact", ID: report.ReportArtifact.SHA256, SHA256: report.ReportArtifact.SHA256})
		}
		err = errors.Join(err, finish(err, refs...))
	}()
	return s.verifyKnowledgePatch(ctx, input, packet, patch)
}

func (s *Service) verifyKnowledgePatch(ctx context.Context, input ValidationInput, packet workpacket.Packet, patch []byte) (domain.KnowledgeValidation, error) {
	item, principal, err := s.validationCandidateFor(ctx, input, true)
	if err != nil {
		return domain.KnowledgeValidation{}, err
	}
	if !packet.LocalOnly || packet.CloudReview {
		return domain.KnowledgeValidation{}, fmt.Errorf("%w: validation must be local-only", ErrForbidden)
	}
	if len(packet.Checks) == 0 {
		return domain.KnowledgeValidation{}, domain.ErrValidationRequired
	}
	if err := s.verifySources(ctx, input.SourceManifest); err != nil {
		return domain.KnowledgeValidation{}, err
	}
	bound := false
	for _, source := range input.SourceManifest.Sources {
		if source.Kind == "repository" && source.Reference == packet.Workspace && source.Revision == packet.BaseRevision {
			bound = true
		}
	}
	if !bound {
		return domain.KnowledgeValidation{}, fmt.Errorf("%w: work packet requires an allowlisted, exact-revision source", domain.ErrValidationRequired)
	}
	// The actually executed patch target must match the target recorded at use
	// time. A task label cannot turn context from repository A into validation
	// evidence for an unrelated patch in repository B.
	if taskID := domain.ScopeFromContext(ctx).TaskID; taskID != "" {
		repository, ok := s.repository.(domain.SemanticRepository)
		if !ok {
			return domain.KnowledgeValidation{}, domain.ErrEvidenceUnavailable
		}
		uses, err := repository.TaskUsedContexts(ctx, taskID)
		if err != nil {
			return domain.KnowledgeValidation{}, err
		}
		for _, use := range uses {
			if use.TargetRepository != packet.Workspace || use.TargetRevision != packet.BaseRevision {
				return domain.KnowledgeValidation{}, domain.ErrQualityBlocked
			}
			matched := false
			for _, source := range input.SourceManifest.Sources {
				if source.Kind == "repository" && source.Reference == use.TargetRepository && source.Revision == use.TargetRevision && source.Branch == use.TargetBranch {
					matched = true
				}
			}
			if !matched {
				return domain.KnowledgeValidation{}, domain.ErrQualityBlocked
			}
		}
	}
	started := time.Now().UTC()
	result := workpacket.VerifyPatch(ctx, packet, patch)
	// Preserve failed checks and policy failures as exact immutable evidence,
	// without turning that evidence into an eligible validation report.
	resultJSON, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return domain.KnowledgeValidation{}, marshalErr
	}
	resultArtifact, storeErr := s.artifacts.Put(ctx, resultJSON, "application/json")
	if storeErr != nil {
		return domain.KnowledgeValidation{}, storeErr
	}
	if err := s.RecordOperationEvidence(ctx, "local.verifier.result", resultArtifact); err != nil {
		return domain.KnowledgeValidation{}, err
	}
	if !result.Accepted {
		return domain.KnowledgeValidation{}, fmt.Errorf("%w: work-packet verification failed: %v", domain.ErrValidationRequired, result.Errors)
	}
	packetJSON, err := json.Marshal(packet)
	if err != nil {
		return domain.KnowledgeValidation{}, err
	}
	packetArtifact, err := s.artifacts.Put(ctx, packetJSON, "application/json")
	if err != nil {
		return domain.KnowledgeValidation{}, err
	}
	patchArtifact, err := s.artifacts.Put(ctx, patch, "text/x-diff")
	if err != nil {
		return domain.KnowledgeValidation{}, err
	}
	matched := false
	for _, source := range input.SourceManifest.Sources {
		if source.Kind == "repository" && source.Revision == result.BaseRevision && source.Reference == packet.Workspace {
			matched = true
		}
	}
	if !matched {
		return domain.KnowledgeValidation{}, fmt.Errorf("%w: source must match verifier workspace and full base revision", domain.ErrValidationRequired)
	}
	input.SourceManifest.Sources = append(input.SourceManifest.Sources,
		domain.KnowledgeSource{Kind: "work_packet", Reference: packet.ID, ArtifactSHA256: packetArtifact.SHA256},
		domain.KnowledgeSource{Kind: "patch", Reference: packet.ID, ArtifactSHA256: patchArtifact.SHA256})
	commands := make([]domain.ValidationCommand, 0, len(result.Checks))
	for _, check := range result.Checks {
		artifact, err := s.artifacts.Put(ctx, []byte(check.Output), "text/plain")
		if err != nil {
			return domain.KnowledgeValidation{}, err
		}
		commands = append(commands, domain.ValidationCommand{Argv: check.Argv, ExitCode: check.ExitCode, OutputSHA256: artifact.SHA256})
	}
	return s.recordValidation(ctx, item, principal, input, "workpacket", commands, started)
}

func (s *Service) recordValidation(ctx context.Context, item domain.KnowledgeItem, principal domain.Principal, input ValidationInput, method string, commands []domain.ValidationCommand, started time.Time) (domain.KnowledgeValidation, error) {
	role := "qa"
	if !principal.Human {
		role = "validation_executor"
	}
	ctx = telemetry.WithRole(ctx, role)
	if err := s.verifySources(ctx, input.SourceManifest); err != nil {
		return domain.KnowledgeValidation{}, err
	}
	repository, err := s.governance()
	if err != nil {
		return domain.KnowledgeValidation{}, err
	}
	reader, ok := s.artifacts.(interface {
		Read(context.Context, string) ([]byte, error)
	})
	if !ok {
		return domain.KnowledgeValidation{}, domain.ErrEvidenceUnavailable
	}
	for _, source := range input.SourceManifest.Sources {
		if source.ArtifactSHA256 != "" {
			if _, err := reader.Read(ctx, source.ArtifactSHA256); err != nil {
				return domain.KnowledgeValidation{}, err
			}
		}
	}
	for _, command := range commands {
		if _, err := reader.Read(ctx, command.OutputSHA256); err != nil {
			return domain.KnowledgeValidation{}, err
		}
	}
	manifest, err := json.Marshal(input.SourceManifest)
	if err != nil {
		return domain.KnowledgeValidation{}, err
	}
	sourceArtifact, err := s.artifacts.Put(ctx, manifest, "application/json")
	if err != nil {
		return domain.KnowledgeValidation{}, err
	}
	id, err := domain.NewID()
	if err != nil {
		return domain.KnowledgeValidation{}, err
	}
	now := time.Now().UTC()
	verdict := "pass"
	for _, command := range commands {
		if command.ExitCode != 0 {
			verdict = "fail"
		}
	}
	for _, criterion := range input.Criteria {
		if !criterion.Passed {
			verdict = "fail"
		}
	}
	validFor := 30 * 24 * time.Hour
	if method == "workpacket" {
		validFor = 24 * time.Hour
	}
	report := domain.KnowledgeValidation{ID: id, SchemaVersion: domain.ValidationSchema, KnowledgeID: item.ID, ProjectID: item.ProjectID,
		CandidateVersion: item.Version, ContentSHA256: domain.Digest([]byte(item.Content)), SourceManifest: input.SourceManifest,
		SourceManifestSHA256: sourceArtifact.SHA256, SourceArtifact: sourceArtifact, Method: method, Criteria: input.Criteria,
		Commands: commands, Verdict: verdict, ValidatedBy: principal.ID, StartedAt: started, CompletedAt: now, ValidUntil: now.Add(validFor)}
	if err := report.Check(); err != nil {
		return report, err
	}
	payload, err := json.Marshal(report)
	if err != nil {
		return report, err
	}
	report.ReportArtifact, err = s.artifacts.Put(ctx, payload, "application/json")
	if err != nil {
		return report, err
	}
	return repository.RecordKnowledgeValidation(ctx, report)
}

func (s *Service) DecideKnowledge(ctx context.Context, input DecisionInput) (domain.KnowledgeItem, error) {
	principal, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return domain.KnowledgeItem{}, err
	}
	input.Decision = strings.ToLower(strings.TrimSpace(input.Decision))
	input.Reason = strings.TrimSpace(input.Reason)
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return domain.KnowledgeItem{}, err
	}
	if err := contracts.Validate("knowledge/v1/decision.schema.json", inputJSON); err != nil {
		return domain.KnowledgeItem{}, fmt.Errorf("%w: %v", domain.ErrValidationRequired, err)
	}
	if input.ExpectedVersion < 1 || input.IdempotencyKey == "" || input.Reason == "" ||
		(input.Decision != "approve" && input.Decision != "reject") || (input.Decision == "approve" && input.ValidationID == "") {
		return domain.KnowledgeItem{}, fmt.Errorf("%w: expected_version, reason, idempotency_key, and approval validation_id required", domain.ErrValidationRequired)
	}
	item, err := s.repository.GetKnowledge(ctx, input.KnowledgeID, true)
	if err != nil {
		return item, err
	}
	if !principal.Human || !principal.HasRole(item.ProjectID, "product_owner") {
		return item, ErrForbidden
	}
	// Workflow state and validation binding are checked again inside the locked
	// persistence transaction. This policy decision supplies identity and scope.
	decision, err := s.authorize(ctx, domain.AuthorizationRequest{Principal: principal, ResourceKind: "knowledge_candidate", ResourceID: item.ID,
		Action: "decide_validated", Attributes: map[string]any{"project_id": item.ProjectID}})
	if err != nil {
		return item, err
	}
	repository, err := s.governance()
	if err != nil {
		return item, err
	}
	if input.Decision == "approve" {
		report, err := repository.GetKnowledgeValidation(ctx, input.ValidationID)
		if err != nil {
			return item, fmt.Errorf("%w: validation report not found", domain.ErrValidationRequired)
		}
		if report.ProjectID != item.ProjectID || report.KnowledgeID != item.ID || report.CandidateVersion != input.ExpectedVersion {
			return item, domain.ErrValidationRequired
		}
		if err := s.verifySources(ctx, report.SourceManifest); err != nil {
			return item, err
		}
		reader, ok := s.artifacts.(interface {
			Read(context.Context, string) ([]byte, error)
		})
		if !ok {
			return item, domain.ErrEvidenceUnavailable
		}
		for _, digest := range []string{report.ReportArtifact.SHA256, report.SourceManifestSHA256} {
			if _, err := reader.Read(ctx, digest); err != nil {
				return item, err
			}
		}
		for _, source := range report.SourceManifest.Sources {
			if source.ArtifactSHA256 != "" {
				if _, err := reader.Read(ctx, source.ArtifactSHA256); err != nil {
					return item, err
				}
			}
		}
		for _, command := range report.Commands {
			if _, err := reader.Read(ctx, command.OutputSHA256); err != nil {
				return item, err
			}
		}
	}
	id, err := domain.NewID()
	if err != nil {
		return item, err
	}
	payload, err := json.Marshal(struct {
		Input DecisionInput
		Actor string
	}{input, principal.ID})
	if err != nil {
		return item, err
	}
	return repository.DecideKnowledge(telemetry.WithRole(ctx, "product_owner"), domain.KnowledgeDecision{ID: id, KnowledgeID: item.ID, ExpectedVersion: input.ExpectedVersion,
		ValidationID: input.ValidationID, Decision: input.Decision, Reason: input.Reason, IdempotencyKey: input.IdempotencyKey,
		Actor: principal.ID, RequestSHA256: domain.Digest(payload), Authorization: decision})
}
