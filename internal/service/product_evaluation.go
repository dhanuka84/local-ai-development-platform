package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/telemetry"
)

type CompareObservationsInput struct {
	ProjectID      string `json:"project_id"`
	IntentID       string `json:"intent_id"`
	ExpectedSHA256 string `json:"expected_sha256"`
	CriterionID    string `json:"criterion_id"`
	LeftRecordID   string `json:"left_record_id"`
	RightRecordID  string `json:"right_record_id"`
}

func (s *Service) CompareProductObservations(ctx context.Context, in CompareObservationsInput) (out domain.ObservationEvaluation, err error) {
	ctx, p, err := s.productAccess(ctx, in.ProjectID, in.IntentID, "read", "internal")
	if err != nil {
		return out, err
	}
	prepared, err := s.IntentContext(ctx, IntentContextInput{ProjectID: in.ProjectID, IntentID: in.IntentID, ExpectedSHA256: in.ExpectedSHA256})
	if err != nil {
		return out, err
	}
	if !prepared.Ready {
		return out, domain.ErrQualityBlocked
	}
	var rule *domain.ReconciliationOracle
	for _, criterion := range prepared.Specification.Criteria {
		if criterion.ID == in.CriterionID && criterion.Oracle == "observation_reconciliation" {
			rule = criterion.Reconciliation
		}
	}
	if rule == nil {
		return out, ErrInvalidInput
	}
	left, err := s.GetProductRecord(ctx, in.ProjectID, in.LeftRecordID, true)
	if err != nil {
		return out, err
	}
	right, err := s.GetProductRecord(ctx, in.ProjectID, in.RightRecordID, true)
	if err != nil {
		return out, err
	}
	intent, err := s.GetProductRecord(ctx, in.ProjectID, in.IntentID, true)
	if err != nil {
		return out, err
	}
	if left.Origin != "source_adapter" || right.Origin != "source_adapter" || left.ProductID != intent.ProductID || right.ProductID != intent.ProductID || left.Source.SourceID != rule.LeftSourceID || right.Source.SourceID != rule.RightSourceID {
		return out, ErrForbidden
	}
	// The v1 oracle compares the complete reviewed source views. Filtered
	// populations need an explicitly versioned reconciliation rule of their own.
	if len(left.Source.Filters) != 0 || len(right.Source.Filters) != 0 || left.Source.Purpose != right.Source.Purpose {
		return out, domain.ErrQualityBlocked
	}
	var a, b domain.SourceEnvelope
	if json.Unmarshal([]byte(left.Content), &a) != nil || json.Unmarshal([]byte(right.Content), &b) != nil {
		return out, domain.ErrQualityBlocked
	}
	// Source-level authorization and field permissions have already been checked
	// by GetProductRecord, including reads through retained KB context.
	role, ok := selectRole(p, in.ProjectID, []string{"operations", "incident_diagnosis", "sdlc_diagnosis", "sdlc_evaluator"})
	if !ok {
		return out, ErrForbidden
	}
	ctx = telemetry.WithRole(telemetry.WithAccountability(ctx, intent.ProductID, intent.Owner, ""), role)
	out = domain.CompareObservations(*rule, a, b)
	out.ID, err = domain.NewID()
	if err != nil {
		return out, err
	}
	out.ProjectID, out.ProductID, out.Actor, out.Owner = in.ProjectID, intent.ProductID, p.ID, intent.Owner
	out.Intent, out.CriterionID = prepared.Intent, in.CriterionID
	out.Left = domain.ProductBinding{RecordID: left.ID, SHA256: left.SHA256, Kind: left.Kind}
	out.Right = domain.ProductBinding{RecordID: right.ID, SHA256: right.SHA256, Kind: right.Kind}
	raw, _ := json.Marshal([]any{out.Intent, out.CriterionID, out.Left, out.Right, out.Method})
	out.RequestSHA256 = domain.Digest(raw)
	out.CreatedAt = time.Now().UTC()
	raw, _ = json.Marshal(out)
	out.Evidence, err = s.artifacts.Put(ctx, raw, "application/json")
	if err != nil {
		return out, err
	}
	r, ok := s.repository.(domain.ProductEvaluationRepository)
	if !ok {
		return out, domain.ErrEvidenceUnavailable
	}
	return r.RecordProductEvaluation(ctx, out)
}

func (s *Service) GetProductEvaluation(ctx context.Context, project, id string) (out domain.ObservationEvaluation, err error) {
	ctx, _, err = s.productAccess(ctx, project, id, "read", "internal")
	if err != nil {
		return out, err
	}
	r, ok := s.repository.(domain.ProductEvaluationRepository)
	if !ok {
		return out, domain.ErrEvidenceUnavailable
	}
	out, err = r.GetProductEvaluation(ctx, project, id)
	if err != nil {
		return domain.ObservationEvaluation{}, err
	}
	for _, binding := range []domain.ProductBinding{out.Intent, out.Left, out.Right} {
		record, e := s.GetProductRecord(ctx, project, binding.RecordID, true)
		if e != nil || record.SHA256 != binding.SHA256 {
			return domain.ObservationEvaluation{}, errors.Join(domain.ErrQualityBlocked, e)
		}
	}
	prepared, err := s.IntentContext(ctx, IntentContextInput{ProjectID: project, IntentID: out.Intent.RecordID, ExpectedSHA256: out.Intent.SHA256})
	if err != nil || !prepared.Ready {
		return domain.ObservationEvaluation{}, errors.Join(domain.ErrQualityBlocked, err)
	}
	return out, nil
}
