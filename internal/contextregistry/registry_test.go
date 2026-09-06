package contextregistry

import (
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"testing"
	"time"
)

func TestReviewedRegistryAndBoundedRequests(t *testing.T) {
	defs, sha, err := Load()
	if err != nil || len(defs) != 6 || len(sha) != 64 {
		t.Fatalf("registry %v", err)
	}
	r := domain.MetricRequest{ProjectID: "test", MetricID: "validated_reuse_rate", Version: 1, Start: time.Now().Add(-time.Hour), End: time.Now()}
	if err = ValidateRequest(r); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"select * from secrets", "unknown", "candidate_approval_rate;drop table projects"} {
		r.MetricID = id
		if err = ValidateRequest(r); err == nil {
			t.Fatal("arbitrary query accepted")
		}
	}
	r.MetricID = "validated_reuse_rate"
	r.Dimensions = map[string]string{"arbitrary_table": "private"}
	if err = ValidateRequest(r); err == nil {
		t.Fatal("unknown dimension accepted")
	}
}
