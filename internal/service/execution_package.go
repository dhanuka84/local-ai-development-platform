package service

import (
	"context"
	"encoding/json"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"time"
)

type PackageEvaluateInput struct {
	ProjectID      string   `json:"project_id"`
	PackageID      string   `json:"package_id"`
	ExpectedSHA256 string   `json:"expected_sha256"`
	RunIDs         []string `json:"run_ids"`
}
type PackageActivateInput struct {
	ProjectID            string `json:"project_id"`
	TargetID             string `json:"target_id"`
	PackageID            string `json:"package_id"`
	EvaluationID         string `json:"evaluation_id"`
	ExpectedActiveSHA256 string `json:"expected_active_sha256"`
	Action               string `json:"action"`
	Reason               string `json:"reason"`
}

type PackageGetInput struct {
	ProjectID string `json:"project_id"`
	TargetID  string `json:"target_id"`
	Role      string `json:"role"`
}
type PackageStatus struct {
	Configured    domain.AgentPackage      `json:"configured"`
	Qualification bool                     `json:"qualification"`
	Active        domain.PackageActivation `json:"active"`
}

func (s *Service) GetPackageStatus(ctx context.Context, in PackageGetInput) (out PackageStatus, err error) {
	if _, err = s.packageAuthority(ctx, in.ProjectID); err != nil {
		return out, err
	}
	target, packages, err := s.executions.Target(in.ProjectID, in.TargetID)
	if err != nil {
		return out, err
	}
	pkg, ok := packages[in.Role]
	if !ok {
		return out, ErrInvalidInput
	}
	r, ok := s.repository.(domain.PackageRepository)
	if !ok {
		return out, domain.ErrEvidenceUnavailable
	}
	out.Configured = pkg
	out.Qualification = target.Qualification
	out.Active, err = r.GetPackageActivation(ctx, in.ProjectID, in.TargetID, in.Role)
	return out, err
}

func (s *Service) packageAuthority(ctx context.Context, project string) (domain.Principal, error) {
	p, err := s.AuthorizeProjectAction(ctx, project, "sdlc_execution", "packages", "create", nil)
	if err != nil {
		return p, err
	}
	if !p.Human || !p.HasRole(project, "qa") || !p.HasRole(project, "operations") {
		return p, ErrForbidden
	}
	return p, nil
}
func (s *Service) EvaluatePackage(ctx context.Context, in PackageEvaluateInput) (out domain.PackageEvaluation, err error) {
	p, err := s.packageAuthority(ctx, in.ProjectID)
	if err != nil {
		return out, err
	}
	pkg, ok := s.executions.Package(in.PackageID)
	if !ok || pkg.Digest() != in.ExpectedSHA256 || len(in.RunIDs) < 1 || len(in.RunIDs) > 20 {
		return out, domain.ErrVersionConflict
	}
	r, ok := s.repository.(domain.PackageRepository)
	if !ok {
		return out, domain.ErrEvidenceUnavailable
	}
	out = domain.PackageEvaluation{ProjectID: in.ProjectID, PackageID: pkg.ID, PackageSHA256: pkg.Digest(), Role: pkg.Role, RegressionID: pkg.RegressionID, RunIDs: in.RunIDs, Actor: p.ID, Outcome: "failed", TrialKind: "deterministic", CreatedAt: time.Now().UTC()}
	out.ID, err = domain.NewID()
	if err != nil {
		return out, err
	}
	proofs := []domain.ExecutionView{}
	seen := map[string]bool{}
	for _, id := range in.RunIDs {
		if seen[id] {
			return out, ErrInvalidInput
		}
		seen[id] = true
		view, err := s.GetExecution(ctx, ExecutionIDInput{ProjectID: in.ProjectID, RunID: id})
		if err != nil {
			return out, err
		}
		if view.Run.Packages[pkg.Role].Digest() != pkg.Digest() || !view.Run.Target.Qualification {
			return out, domain.ErrVersionConflict
		}
		proofs = append(proofs, view)
		for _, step := range view.Steps {
			if step.Result == nil {
				continue
			}
			result := step.Result
			role := domain.ExecutionRole(step.Stage)
			if role == pkg.Role && step.PackageSHA == pkg.Digest() {
				if result.Model != nil {
					if result.Model.TrialKind == "fixture" {
						out.TrialKind = "fixture"
					} else if out.TrialKind != "fixture" {
						out.TrialKind = "local_model"
					}
				}
				if result.Outcome == "succeeded" && view.Run.Status == "completed" {
					out.Positive++
				}
				if result.Outcome == "failed" || result.Outcome == "blocked" {
					out.Negative++
				}
			}
			// A failed independent candidate evaluation also exercises the builder
			// and delivery gate: the candidate cannot transition to delivery.
			if result.Stage == "evaluate" && result.Outcome == "failed" && (pkg.Role == "sdlc_builder" || pkg.Role == "sdlc_delivery") {
				out.Negative++
			}
		}
	}
	if out.Positive > 0 && out.Negative > 0 {
		out.Outcome = "passed"
	}
	raw, _ := json.Marshal(struct {
		Evaluation domain.PackageEvaluation `json:"evaluation"`
		Runs       []domain.ExecutionView   `json:"runs"`
	}{out, proofs})
	out.Evidence, err = s.artifacts.Put(ctx, raw, "application/json")
	if err != nil {
		return out, err
	}
	return r.RecordPackageEvaluation(ctx, out)
}
func (s *Service) ActivatePackage(ctx context.Context, in PackageActivateInput) (out domain.PackageActivation, err error) {
	p, err := s.packageAuthority(ctx, in.ProjectID)
	if err != nil {
		return out, err
	}
	if len(in.Reason) < 1 || len(in.Reason) > 2048 || (in.Action != "activate" && in.Action != "rollback") {
		return out, ErrInvalidInput
	}
	pkg, ok := s.executions.Package(in.PackageID)
	if !ok {
		return out, ErrForbidden
	}
	target, _, err := s.executions.Target(in.ProjectID, in.TargetID)
	if err != nil {
		return out, err
	}
	if target.Packages[pkg.Role] != pkg.ID {
		return out, domain.ErrVersionConflict
	}
	r, ok := s.repository.(domain.PackageRepository)
	if !ok {
		return out, domain.ErrEvidenceUnavailable
	}
	evaluation, err := r.GetPackageEvaluation(ctx, in.ProjectID, in.EvaluationID)
	if err != nil {
		return out, err
	}
	if evaluation.PackageSHA256 != pkg.Digest() || evaluation.RegressionID != pkg.RegressionID || evaluation.Outcome != "passed" || target.Environment != "disposable" && pkg.Model != "" && evaluation.TrialKind != "local_model" {
		return out, domain.ErrValidationRequired
	}
	out = domain.PackageActivation{ProjectID: in.ProjectID, TargetID: target.ID, Role: pkg.Role, PackageID: pkg.ID, PackageSHA256: pkg.Digest(), EvaluationID: evaluation.ID, Actor: p.ID, Action: in.Action, Reason: in.Reason, PreviousSHA256: in.ExpectedActiveSHA256}
	return r.ActivatePackage(ctx, out)
}
