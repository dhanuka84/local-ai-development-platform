// Package telemetry assigns real OpenTelemetry span IDs while PostgreSQL owns
// durable evidence. Export is an independent, retryable projection.
package telemetry

import (
	"context"
	"encoding/json"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var tracer = sdktrace.NewTracerProvider().Tracer("hybrid-ai/platform")

type modelKey struct{}
type roleKey struct{}

func WithModel(ctx context.Context, provider, model string) context.Context {
	return context.WithValue(ctx, modelKey{}, [2]string{provider, model})
}
func WithRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, roleKey{}, role)
}

func Start(ctx context.Context, name string, scope domain.OperationScope, refs []domain.EvidenceReference) (context.Context, trace.Span, domain.OperationRecord) {
	parent := trace.SpanContextFromContext(ctx)
	ctx, span := tracer.Start(ctx, name)
	ctx = domain.WithOperationScope(ctx, scope)
	id, _ := domain.NewID()
	eventID, _ := domain.NewID()
	actor, role := "system:local-worker", "operations"
	if principal, ok := identity.PrincipalFromContext(ctx); ok {
		actor = principal.ID
		role = "authenticated"
	}
	if acting, ok := ctx.Value(roleKey{}).(string); ok {
		role = acting
	}
	if refs == nil {
		refs = []domain.EvidenceReference{}
	}
	payload, _ := json.Marshal(struct {
		Scope      domain.OperationScope
		References []domain.EvidenceReference
	}{scope, refs})
	record := domain.OperationRecord{ID: eventID, OperationID: id, OperationScope: scope, TraceID: span.SpanContext().TraceID().String(), SpanID: span.SpanContext().SpanID().String(), Name: name, Phase: "intent", Outcome: "pending", Actor: actor, Role: role, InputSHA256: domain.Digest(payload), References: refs, Rationale: "preconditions_checked_at_owned_boundary", RecordedAt: time.Now().UTC()}
	if model, ok := ctx.Value(modelKey{}).([2]string); ok {
		record.Provider = model[0]
		record.Model = model[1]
	}
	if parent.IsValid() {
		record.ParentSpanID = parent.SpanID().String()
	}
	return ctx, span, record
}

func Result(record domain.OperationRecord, outcome string, refs []domain.EvidenceReference) domain.OperationRecord {
	record.ID, _ = domain.NewID()
	record.Phase = "outcome"
	record.Outcome = outcome
	record.RecordedAt = time.Now().UTC()
	if refs != nil {
		record.References = append(record.References, refs...)
	}
	payload, _ := json.Marshal(struct {
		Outcome    string
		References []domain.EvidenceReference
	}{outcome, record.References})
	record.ResultSHA256 = domain.Digest(payload)
	return record
}
