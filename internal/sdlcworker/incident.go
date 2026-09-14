package sdlcworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
	"github.com/dhanuka84/hybrid-ai-platform/internal/mcpclient"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
)

func (w *Worker) observe(ctx context.Context, claim domain.ExecutionClaim, l domain.ExecutionLease, c service.ExecutionContext) (records []domain.ProductRecord, evaluations []domain.ObservationEvaluation, err error) {
	if c.ObservationStart.IsZero() || !c.ObservationEnd.After(c.ObservationStart) {
		return nil, nil, domain.ErrValidationRequired
	}
	if wait := time.Until(c.ObservationEnd.Add(time.Second)); wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		}
	}
	purpose := "diagnosis"
	if c.Stage == "verify_recovery" {
		purpose = "recovery"
	}
	bySource := map[string]domain.ProductRecord{}
	for _, source := range c.Sources {
		fields := []string{}
		allowed, scoped := source.FieldsByRole[claim.Run.Package().Role]
		if scoped {
			fields = append(fields, allowed...)
		} else {
			for field := range source.Fields {
				fields = append(fields, field)
			}
		}
		sort.Strings(fields)
		q := domain.SourceQuery{ProjectID: l.ProjectID, ProductID: claim.Run.ProductID, SourceID: source.ID, Purpose: purpose, Start: c.ObservationStart, End: c.ObservationEnd, Fields: fields, Limit: min(100, source.MaxRows), IdempotencyKey: "observe-" + source.ID}
		var receipt domain.SourceReceipt
		if err = w.Gateway.Call(ctx, "sdlc_source_query", struct {
			domain.ExecutionLease
			Query domain.SourceQuery `json:"query"`
		}{l, q}, &receipt); err != nil {
			return records, evaluations, err
		}
		if receipt.RecordID == "" || receipt.Status != "complete" || !receipt.Source.Complete {
			return records, evaluations, errors.New("source " + source.ID + " is unavailable or incomplete; causal selection and recovery are blocked")
		}
		var record domain.ProductRecord
		if err = w.Gateway.Call(ctx, "sdlc_record_get", struct {
			domain.ExecutionLease
			RecordID string `json:"record_id"`
		}{l, receipt.RecordID}, &record); err != nil {
			return records, evaluations, err
		}
		records = append(records, record)
		bySource[source.ID] = record
	}
	for _, criterion := range c.Intent.Specification.Criteria {
		rule := criterion.Reconciliation
		if rule == nil {
			return records, evaluations, domain.ErrValidationRequired
		}
		left, lok := bySource[rule.LeftSourceID]
		right, rok := bySource[rule.RightSourceID]
		if !lok || !rok {
			return records, evaluations, domain.ErrEvidenceUnavailable
		}
		var evaluation domain.ObservationEvaluation
		input := service.CompareObservationsInput{ProjectID: l.ProjectID, IntentID: claim.Run.Intent.RecordID, ExpectedSHA256: claim.Run.Intent.SHA256, CriterionID: criterion.ID, LeftRecordID: left.ID, RightRecordID: right.ID}
		if err = w.Gateway.Call(ctx, "sdlc_observations_compare", struct {
			domain.ExecutionLease
			Comparison service.CompareObservationsInput `json:"comparison"`
		}{l, input}, &evaluation); err != nil {
			return records, evaluations, err
		}
		evaluations = append(evaluations, evaluation)
	}
	return records, evaluations, nil
}
func (w *Worker) diagnose(ctx context.Context, claim domain.ExecutionClaim, l domain.ExecutionLease, c service.ExecutionContext) (out domain.ExecutionResult, err error) {
	out.Stage = "diagnose"
	records, evaluations, err := w.observe(ctx, claim, l, c)
	if err != nil {
		return out, err
	}
	violation := false
	for _, e := range evaluations {
		out.EvaluationIDs = append(out.EvaluationIDs, e.ID)
		violation = violation || e.Outcome == "violated"
	}
	if !violation {
		return out, errors.New("no accepted business criterion is demonstrably violated")
	}
	raw, _ := json.Marshal(struct {
		Schema       string                         `json:"schema"`
		Context      service.ExecutionContext       `json:"context"`
		Observations []domain.ProductRecord         `json:"observations"`
		Evaluations  []domain.ObservationEvaluation `json:"evaluations"`
		Remedies     []string                       `json:"allowed_remedies"`
		Contract     execution.DiagnosisProposal    `json:"response_contract"`
	}{"hybrid-ai/diagnosis-request/v1", c, records, evaluations, claim.Run.Target.AllowedRemedies, execution.DiagnosisProposal{Schema: "hybrid-ai/diagnosis-proposal/v1", Criteria: criterionIDs(c), Summary: "Explain the observed violation", Hypotheses: []domain.ExecutionHypothesis{{ID: "candidate-cause", Explanation: "Compare current evidence against competing explanations, including historical incidents. Cite only supplied record/evaluation IDs.", Status: "supported"}, {ID: "alternative", Explanation: "Identify and cite evidence contradicting this alternative.", Status: "contradicted"}}}})
	contextRaw, _ := json.Marshal(c)
	p := claim.Run.Package()
	manifest := execution.DisclosureManifest{Schema: "hybrid-ai/disclosed-context/v1", RunID: claim.Run.ID, StepID: l.StepID, ContextSHA256: domain.Digest(contextRaw), PackageSHA256: p.Digest(), RepositoryID: claim.Run.Target.RepositoryID, Files: map[string]string{}, Observations: []domain.ProductBinding{}, Provider: "ollama", Model: p.Model, ModelSHA256: p.ModelSHA256}
	for _, r := range records {
		out.ObservationIDs = append(out.ObservationIDs, r.ID)
		manifest.Observations = append(manifest.Observations, domain.ProductBinding{RecordID: r.ID, SHA256: r.SHA256, Kind: r.Kind})
	}
	generated, generateErr := w.Model.Generate(ctx, p, string(raw))
	model, err := w.modelEvidence(ctx, claim, l, manifest, generated)
	if err != nil {
		return out, err
	}
	out.Model = &model
	if generateErr != nil {
		return out, generateErr
	}
	var proposal execution.DiagnosisProposal
	if execution.DecodeProposal([]byte(generated.Output.Response), &proposal) != nil || proposal.Schema != "hybrid-ai/diagnosis-proposal/v1" {
		return out, domain.ErrValidationRequired
	}
	out.Outcome = "succeeded"
	out.Criteria = proposal.Criteria
	out.Hypotheses = proposal.Hypotheses
	out.Summary = proposal.Summary
	return out, nil
}

func remedyRead(ctx context.Context, client *http.Client, remedy execution.Remedy, token string) (raw []byte, etag, action string, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", remedy.Endpoint, nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(req)
	if err != nil {
		return nil, "", "", errors.New("remedy read-back unavailable")
	}
	defer response.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil || len(raw) > 16384 || response.StatusCode != 200 || !json.Valid(raw) {
		return nil, "", "", domain.ErrEvidenceUnavailable
	}
	var compact bytes.Buffer
	_ = json.Compact(&compact, raw)
	raw = compact.Bytes()
	etag = response.Header.Get("ETag")
	action = response.Header.Get("X-SDLC-Action")
	if etag != `"`+domain.Digest(raw)+`"` {
		return nil, "", "", errors.New("remedy endpoint lacks exact state concurrency token")
	}
	return raw, etag, action, nil
}
func (w *Worker) remediate(ctx context.Context, claim domain.ExecutionClaim, l domain.ExecutionLease, c service.ExecutionContext) (out domain.ExecutionResult, err error) {
	out.Stage = claim.Run.Stage
	cfg := w.Config.Remediation
	if cfg.Digest() != claim.Run.Target.RemediationSHA256 {
		return out, domain.ErrForbidden
	}
	diagnosis := latest(c, "diagnose")
	if diagnosis == nil || diagnosis.Outcome != "succeeded" {
		return out, domain.ErrValidationRequired
	}
	selected := ""
	for _, h := range diagnosis.Hypotheses {
		if h.Status == "supported" {
			if selected != "" {
				return out, domain.ErrValidationRequired
			}
			selected = h.ProposedRemedy
		}
	}
	var remedy execution.Remedy
	for _, r := range cfg.Remedies {
		if r.ID == selected {
			remedy = r
		}
	}
	u, e := url.Parse(remedy.Endpoint)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !json.Valid(remedy.Before) || !json.Valid(remedy.Desired) || len(remedy.Before) > 16384 || len(remedy.Desired) > 16384 {
		return out, domain.ErrForbidden
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return out, domain.ErrForbidden
		}
	}
	var before, desired bytes.Buffer
	_ = json.Compact(&before, remedy.Before)
	_ = json.Compact(&desired, remedy.Desired)
	token, e := mcpclient.ReadToken(remedy.TokenFile)
	if e != nil {
		return out, e
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return domain.ErrForbidden }}
	action := l.StepID
	if out.Stage == "verify_recovery" {
		for _, step := range c.PriorSteps {
			if step.Stage == "remediate" && step.Result != nil && step.Result.Outcome == "succeeded" {
				action = step.ID
			}
		}
	}
	state, etag, observedAction, err := remedyRead(ctx, client, remedy, token)
	if err != nil {
		return out, err
	}
	if bytes.Equal(state, before.Bytes()) && out.Stage == "remediate" && claim.Run.Status != "reconciling" {
		var current domain.ExecutionView
		if err = w.Gateway.Call(ctx, "sdlc_run_get", service.ExecutionIDInput{ProjectID: l.ProjectID, RunID: l.RunID}, &current); err != nil {
			return out, err
		}
		if current.Run.Status != "running" {
			return out, domain.ErrLeaseLost
		}
		var disclosure service.ExecutionContext
		if err = w.Gateway.Call(ctx, "sdlc_step_context", l, &disclosure); err != nil {
			return out, err
		}
		req, e := http.NewRequestWithContext(ctx, "PUT", remedy.Endpoint, bytes.NewReader(desired.Bytes()))
		if e != nil {
			return out, e
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", etag)
		req.Header.Set("Idempotency-Key", action)
		response, writeErr := client.Do(req)
		if response != nil {
			_ = response.Body.Close()
		}
		state, _, observedAction, err = remedyRead(ctx, client, remedy, token)
		if err != nil {
			if writeErr != nil {
				return out, errors.New("remedy write outcome unknown; stable action read-back required")
			}
			return out, err
		}
	}
	if !bytes.Equal(state, desired.Bytes()) || observedAction != action {
		return out, errors.New("remedy did not reach the exact authorized state for this action")
	}
	kind := "remedy"
	if out.Stage == "verify_recovery" {
		kind = "technical_probe"
	}
	effect, err := w.effect(ctx, l, execution.EffectReceipt{Kind: kind, ContractSHA256: cfg.Digest(), ExternalID: remedy.ID + ":" + action, BeforeSHA256: domain.Digest(before.Bytes()), AfterSHA256: domain.Digest(desired.Bytes()), ObservedSHA256: domain.Digest(state)}, state)
	if err != nil {
		return out, err
	}
	out.Effects = append(out.Effects, effect)
	if out.Stage == "verify_recovery" {
		records, evaluations, err := w.observe(ctx, claim, l, c)
		if err != nil {
			return out, err
		}
		for _, r := range records {
			out.ObservationIDs = append(out.ObservationIDs, r.ID)
		}
		for _, e := range evaluations {
			out.EvaluationIDs = append(out.EvaluationIDs, e.ID)
			if e.Outcome != "satisfied" {
				return out, errors.New("technical state recovered but an accepted business criterion is not satisfied")
			}
		}
	}
	out.Outcome = "succeeded"
	out.Criteria = criterionIDs(c)
	out.Summary = "Independent role verified the authorized remedy state."
	if out.Stage == "verify_recovery" {
		out.Summary = "Technical state and accepted business criteria recovered in a new complete observation window."
	}
	return out, nil
}
