package service

import (
	"context"
	"encoding/json"

	"github.com/dhanuka84/hybrid-ai-platform/components/workpacket"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
)

func (s *Service) validateModelSubmission(ctx context.Context, view domain.ExecutionView, result domain.ExecutionResult) error {
	m := result.Model
	pkg := view.Run.Package()
	requestRaw, err := s.readExecutionArtifact(ctx, view.Run, m.Prompt.SHA256)
	if err != nil {
		return err
	}
	responseRaw, err := s.readExecutionArtifact(ctx, view.Run, m.Response.SHA256)
	if err != nil {
		return err
	}
	manifestRaw, err := s.readExecutionArtifact(ctx, view.Run, m.Manifest.SHA256)
	if err != nil {
		return err
	}
	var request execution.ModelRequest
	var response execution.ModelResponse
	var manifest execution.DisclosureManifest
	if execution.DecodeProposal(requestRaw, &request) != nil || json.Unmarshal(responseRaw, &response) != nil || execution.DecodeProposal(manifestRaw, &manifest) != nil {
		return domain.ErrValidationRequired
	}
	if request.Model != pkg.Model || request.System != pkg.Instructions || !execution.ValidProposalFormat(pkg.Role, request.Format) || request.Stream || request.Think || request.Options.NumPredict != pkg.MaxOutputTokens || request.Options.Temperature != 0 || len(request.Prompt) > pkg.MaxInputBytes || !response.Done || response.DoneReason == "length" || response.Model != m.Model || response.PromptEvalCount != m.InputTokens || response.EvalCount != m.OutputTokens {
		return domain.ErrValidationRequired
	}
	var contextSHA, stepID string
	for _, step := range view.Steps {
		if step.Number == view.Run.StepNumber {
			contextSHA = step.ContextSHA
			stepID = step.ID
		}
	}
	if manifest.Schema != "hybrid-ai/disclosed-context/v1" || manifest.RunID != view.Run.ID || manifest.StepID != stepID || manifest.ContextSHA256 != contextSHA || manifest.PackageSHA256 != pkg.Digest() || manifest.Provider != "ollama" || manifest.Model != pkg.Model || manifest.ModelSHA256 != pkg.ModelSHA256 || manifest.RepositoryID != view.Run.Target.RepositoryID {
		return domain.ErrValidationRequired
	}
	var prompt struct {
		Context      json.RawMessage                `json:"context"`
		Files        map[string]string              `json:"files"`
		Observations []domain.ProductRecord         `json:"observations"`
		Evaluations  []domain.ObservationEvaluation `json:"evaluations"`
	}
	if json.Unmarshal([]byte(request.Prompt), &prompt) != nil || domain.Digest(prompt.Context) != contextSHA {
		return domain.ErrValidationRequired
	}
	if len(prompt.Files) != len(manifest.Files) {
		return domain.ErrValidationRequired
	}
	for name, content := range prompt.Files {
		if domain.Digest([]byte(content)) != manifest.Files[name] {
			return domain.ErrValidationRequired
		}
	}
	if result.Stage == "build" {
		var proposal execution.BuilderProposal
		if execution.DecodeProposal([]byte(response.Response), &proposal) != nil {
			return domain.ErrValidationRequired
		}
		if len(proposal.Files) > 0 {
			if proposal.Patch != "" {
				return domain.ErrValidationRequired
			}
			proposal.Patch, err = execution.FilePatch(prompt.Files, proposal.Files)
			if err != nil {
				return err
			}
		}
		if proposal.Schema != "hybrid-ai/builder-proposal/v1" || domain.Digest([]byte(proposal.Patch)) != result.Patch.SHA256 || proposal.BaseRevision != result.BaseRevision || manifest.RepositoryRevision != result.BaseRevision || !sameStrings(proposal.Criteria, result.Criteria) {
			return domain.ErrValidationRequired
		}
		a, _ := json.Marshal(proposal.Plan)
		b, _ := json.Marshal(result.Plan)
		if string(a) != string(b) {
			return domain.ErrValidationRequired
		}
	} else {
		if len(prompt.Observations) != len(manifest.Observations) || len(prompt.Observations) != len(result.ObservationIDs) || len(prompt.Evaluations) != len(result.EvaluationIDs) {
			return domain.ErrValidationRequired
		}
		for i, observed := range prompt.Observations {
			record, err := s.GetProductRecord(ctx, view.Run.ProjectID, observed.ID, true)
			if err != nil {
				return err
			}
			a, _ := json.Marshal(observed)
			b, _ := json.Marshal(record)
			if string(a) != string(b) || observed.ID != result.ObservationIDs[i] || manifest.Observations[i] != (domain.ProductBinding{RecordID: observed.ID, SHA256: observed.SHA256, Kind: observed.Kind}) {
				return domain.ErrValidationRequired
			}
		}
		for i, submitted := range prompt.Evaluations {
			evaluation, err := s.GetProductEvaluation(ctx, view.Run.ProjectID, submitted.ID)
			if err != nil {
				return err
			}
			a, _ := json.Marshal(submitted)
			b, _ := json.Marshal(evaluation)
			if string(a) != string(b) || submitted.ID != result.EvaluationIDs[i] {
				return domain.ErrValidationRequired
			}
		}
		var proposal execution.DiagnosisProposal
		if execution.DecodeProposal([]byte(response.Response), &proposal) != nil || proposal.Schema != "hybrid-ai/diagnosis-proposal/v1" || !sameStrings(proposal.Criteria, result.Criteria) {
			return domain.ErrValidationRequired
		}
		a, _ := json.Marshal(proposal.Hypotheses)
		b, _ := json.Marshal(result.Hypotheses)
		if string(a) != string(b) {
			return domain.ErrValidationRequired
		}
	}
	return nil
}

func (s *Service) validateVerifierReceipt(ctx context.Context, view domain.ExecutionView, packet workpacket.Packet, result domain.ExecutionResult) error {
	for _, a := range result.Artifacts {
		raw, err := s.readExecutionArtifact(ctx, view.Run, a.SHA256)
		if err != nil {
			return err
		}
		var receipt workpacket.VerificationResult
		if execution.DecodeProposal(raw, &receipt) != nil {
			continue
		}
		if !receipt.Accepted || !receipt.Evaluation.Allowed || receipt.BaseRevision != packet.BaseRevision || len(receipt.Checks) != len(result.Checks) || len(receipt.Checks) != len(packet.Checks) || len(receipt.Errors) > 0 || len(receipt.ChangedFiles) == 0 || len(receipt.ChangedFiles) > packet.Limits.MaxChangedFiles || receipt.TotalDiffLines > packet.Limits.MaxDiffLines {
			return domain.ErrValidationRequired
		}
		for i, check := range receipt.Checks {
			argv, _ := json.Marshal(check.Argv)
			expected, _ := json.Marshal(packet.Checks[i].Argv)
			if check.Name != packet.Checks[i].Name || string(argv) != string(expected) {
				return domain.ErrValidationRequired
			}
			output := check.Output
			if output == "" {
				output = "No output was produced."
			}
			if !check.Passed || check.ExitCode != 0 || check.Name != result.Checks[i].Name || domain.Digest(argv) != result.Checks[i].ArgvSHA256 || domain.Digest([]byte(output)) != result.Checks[i].Output.SHA256 {
				return domain.ErrValidationRequired
			}
		}
		return nil
	}
	return domain.ErrEvidenceUnavailable
}
