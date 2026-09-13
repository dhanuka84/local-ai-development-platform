package service

import (
	"context"
	"errors"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/authorization"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	"github.com/dhanuka84/hybrid-ai-platform/internal/sources"
)

func TestRetainedObservationProtectsEmptyResultMetadata(t *testing.T) {
	d := domain.SourceDescriptor{ID: "audit", ProjectID: "fixture", ProductID: "orders", Kind: "audit", Endpoint: "https://source.invalid/view", SchemaVersion: "v1", Classification: "confidential", Owner: "human:owner", Roles: []string{"operations", "incident_diagnosis"}, Purposes: []string{"diagnosis"}, Fields: map[string]string{"event_id": "string", "event_time": "timestamp", "private_account": "string"}, FieldsByRole: map[string][]string{"operations": {"event_id", "event_time", "private_account"}, "incident_diagnosis": {"event_id", "event_time"}}, Filters: []string{"private_account"}, MaxRows: 10, MaxBytes: 4096, TimeoutSeconds: 1, MaxWindowSeconds: 3600, RetentionSeconds: 3600}
	registry, err := sources.New([]domain.SourceDescriptor{d})
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{sources: registry, authorizer: authorization.Disabled{}}
	record := domain.ProductRecord{ProjectID: d.ProjectID, ProductID: d.ProductID, Content: `{"rows":[]}`, Source: domain.ProductSourceRef{SourceID: d.ID, SchemaVersion: d.SchemaVersion}}
	diagnostic := identity.WithPrincipal(context.Background(), domain.Principal{ID: "agent:fixture", RoleBindings: map[string][]string{d.ProjectID: {"incident_diagnosis"}}})
	operator := identity.WithPrincipal(context.Background(), domain.Principal{ID: "human:owner", Human: true, RoleBindings: map[string][]string{d.ProjectID: {"operations"}}})
	if err = s.authorizeRetainedObservation(diagnostic, record); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*domain.ProductRecord)
	}{
		{"selected fields", func(r *domain.ProductRecord) { r.Source.Fields = []string{"event_id", "event_time", "private_account"} }},
		{"filter metadata", func(r *domain.ProductRecord) {
			r.Source.Filters = map[string]string{"private_account": "synthetic-private-value"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := record
			tc.change(&r)
			if err := s.authorizeRetainedObservation(diagnostic, r); !errors.Is(err, ErrForbidden) {
				t.Fatalf("private metadata not protected: %v", err)
			}
			if err := s.authorizeRetainedObservation(operator, r); err != nil {
				t.Fatal(err)
			}
		})
	}
}
