package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	"github.com/dhanuka84/hybrid-ai-platform/internal/sources"
	"github.com/dhanuka84/hybrid-ai-platform/internal/telemetry"
	"slices"
	"time"
)

func (s *Service) ProductSources(ctx context.Context, project string) ([]domain.SourceDescriptor, error) {
	_, p, err := s.productAccess(ctx, project, "sources", "read", "internal")
	if err != nil {
		return nil, err
	}
	if s.sources == nil {
		return []domain.SourceDescriptor{}, nil
	}
	out := []domain.SourceDescriptor{}
	for _, d := range s.sources.List(project) {
		for _, role := range d.Roles {
			if p.HasRole(project, role) {
				out = append(out, d)
				break
			}
		}
	}
	return out, nil
}

// Retention must not widen a source field's audience. Reauthorize the source
// contract at every KB hydration, including after descriptor changes/revocation.
func (s *Service) authorizeRetainedObservation(ctx context.Context, record domain.ProductRecord) error {
	if s.sources == nil {
		return ErrForbidden
	}
	d, ok := s.sources.Get(record.ProjectID, record.Source.SourceID)
	if !ok || d.ProductID != record.ProductID {
		return ErrForbidden
	}
	if record.Source.SchemaVersion != d.SchemaVersion {
		return domain.ErrQualityBlocked
	}
	p, err := s.authorizeExecutionSource(ctx, d, "read_observation", record.Source.Purpose)
	if err != nil {
		return err
	}
	role, ok := selectRole(p, record.ProjectID, d.Roles)
	if !ok {
		return ErrForbidden
	}
	var envelope domain.SourceEnvelope
	if json.Unmarshal([]byte(record.Content), &envelope) != nil {
		return domain.ErrQualityBlocked
	}
	if d.AdapterSHA256 != "" && envelope.AdapterSHA256 != d.AdapterSHA256 {
		return domain.ErrQualityBlocked
	}
	allowedField := func(field string) bool {
		_, known := d.Fields[field]
		return known && (len(d.FieldsByRole) == 0 || slices.Contains(d.FieldsByRole[role], field))
	}
	// Query metadata can contain sensitive filter values even when no rows were
	// returned. The collection scope must retain its restrictions in that case.
	for _, field := range record.Source.Fields {
		if !allowedField(field) {
			return ErrForbidden
		}
	}
	for field := range record.Source.Filters {
		if !allowedField(field) {
			return ErrForbidden
		}
	}
	for _, row := range envelope.Rows {
		for field := range row {
			if !allowedField(field) {
				return ErrForbidden
			}
		}
	}
	return nil
}

func (s *Service) QueryProductSource(ctx context.Context, q domain.SourceQuery) (out domain.SourceReceipt, err error) {
	p, err := identity.RequirePrincipal(ctx)
	if err != nil {
		return out, err
	}
	scope := domain.ScopeFromContext(ctx)
	scope.ProjectID = q.ProjectID
	ctx = domain.WithOperationScope(ctx, scope)
	r, ok := s.repository.(domain.SourceRepository)
	if !ok || s.sources == nil {
		return out, domain.ErrEvidenceUnavailable
	}
	d, ok := s.sources.Get(q.ProjectID, q.SourceID)
	if !ok {
		return out, ErrForbidden
	}
	if _, err = s.authorizeExecutionSource(ctx, d, "query", q.Purpose); err != nil {
		return out, err
	}
	role, ok := selectRole(p, q.ProjectID, d.Roles)
	if !ok {
		return out, ErrForbidden
	}
	ctx = telemetry.WithRole(ctx, role)
	delegator := ""
	if p.Delegation != nil {
		delegator = p.Delegation.DelegatedBy
	}
	if run, ok := executionGrant(ctx); ok {
		delegator = run.Owner
	}
	ctx = telemetry.WithAccountability(ctx, q.ProductID, d.Owner, delegator)
	raw, _ := json.Marshal(q)
	querySHA := domain.Digest(raw)
	descriptor, _ := json.Marshal(d)
	descriptorSHA := domain.Digest(descriptor)
	ctx, finish, err := s.BeginOperation(ctx, "source.query", domain.ScopeFromContext(ctx), domain.EvidenceReference{Kind: "source", ID: d.ID, SHA256: descriptorSHA}, domain.EvidenceReference{Kind: "query", ID: q.IdempotencyKey, SHA256: querySHA})
	if err != nil {
		return out, err
	}
	defer func() { err = errors.Join(err, finish(err)) }()
	if err = sources.ValidateQuery(d, q); err != nil {
		return out, err
	}
	if len(d.FieldsByRole) > 0 {
		allowed, ok := d.FieldsByRole[role]
		if !ok {
			return out, ErrForbidden
		}
		for _, field := range q.Fields {
			if !slices.Contains(allowed, field) {
				return out, ErrForbidden
			}
		}
		for field := range q.Filters {
			if !slices.Contains(allowed, field) {
				return out, ErrForbidden
			}
		}
	}
	if !slices.Contains([]string{"operations", "incident_diagnosis", "sdlc_diagnosis", "sdlc_evaluator"}, role) {
		return out, ErrForbidden
	}
	previous, e := r.GetSourceReceipt(ctx, q.ProjectID, q.SourceID, p.ID, q.IdempotencyKey)
	if e != nil {
		return out, e
	}
	if previous.ID != "" {
		if previous.QuerySHA256 != querySHA || previous.DescriptorSHA256 != descriptorSHA {
			return out, domain.ErrVersionConflict
		}
		if previous.RecordID != "" {
			if _, e = s.GetProductRecord(ctx, q.ProjectID, previous.RecordID, true); e != nil {
				return out, e
			}
		}
		return previous, nil
	}
	out.ID, err = domain.NewID()
	if err != nil {
		return out, err
	}
	out.ProjectID = q.ProjectID
	out.ProductID = q.ProductID
	out.SourceID = d.ID
	out.Actor = p.ID
	out.Role = role
	out.Owner = d.Owner
	out.Purpose = q.Purpose
	out.QuerySHA256 = querySHA
	out.DescriptorSHA256 = descriptorSHA
	out.IdempotencyKey = q.IdempotencyKey
	result, queryErr := s.sources.Query(ctx, d, q)
	out.CollectedAt = time.Now().UTC()
	if queryErr != nil {
		out.Status = "failed"
		out.Reason = "source request or schema/coverage validation failed; no observation retained"
		receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err = r.RecordSourceObservation(receiptCtx, out, nil); err != nil {
			return out, err
		}
		return out, fmt.Errorf("source collection failed; receipt %s", out.ID)
	}
	out.Status = "complete"
	if !result.Complete {
		out.Status = "partial"
		out.Reason = "coverage is incomplete; missing events or metrics cannot be treated as zero or absent"
	}
	out.Source = domain.ProductSourceRef{SourceID: d.ID, Revision: result.Revision, SchemaVersion: result.SchemaVersion, ReceiptID: out.ID, Start: &result.Start, End: &result.End, Watermark: &result.Watermark, CollectedAt: &out.CollectedAt, Complete: result.Complete, Snapshot: result.Snapshot, Offsets: result.Offsets}
	out.Source.Purpose, out.Source.Fields, out.Source.Filters, out.Source.RowLimit = q.Purpose, q.Fields, q.Filters, q.Limit
	raw, err = json.Marshal(result)
	if err != nil {
		return out, err
	}
	out.Evidence, err = s.artifacts.Put(ctx, raw, "application/json")
	if err != nil {
		return out, err
	}
	out.RecordID, err = domain.NewID()
	if err != nil {
		return out, err
	}
	expires := out.CollectedAt.Add(time.Duration(d.RetentionSeconds) * time.Second)
	record := domain.ProductRecord{ID: out.RecordID, ProjectID: q.ProjectID, ProductID: q.ProductID, Key: "observation:" + out.ID, Kind: "observation", Version: 1, Title: d.Kind + " observation from " + d.ID, Content: string(raw), Classification: d.Classification, Origin: "source_adapter", Status: "observed", Actor: p.ID, Owner: d.Owner, Source: out.Source, Evidence: out.Evidence, CreatedAt: out.CollectedAt, ExpiresAt: &expires}
	record.SHA256 = record.Digest()
	if err = r.RecordSourceObservation(ctx, out, &record); err != nil {
		return out, err
	}
	return out, nil
}
