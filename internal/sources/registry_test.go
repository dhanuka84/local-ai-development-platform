package sources

import (
	"context"
	"encoding/json"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(endpoint string) (domain.SourceDescriptor, domain.SourceQuery, domain.SourceEnvelope) {
	start := time.Now().UTC().Truncate(time.Minute).Add(-time.Hour)
	end := start.Add(30 * time.Minute)
	d := domain.SourceDescriptor{ID: "orders", ProjectID: "test", ProductID: "checkout", Kind: "event", Endpoint: endpoint, AllowLoopbackHTTP: true, SchemaVersion: "events/v1", Classification: "internal", Owner: "human:fixture", Roles: []string{"incident_diagnosis"}, Purposes: []string{"diagnosis"}, Fields: map[string]string{"event_id": "string", "event_time": "timestamp", "status": "string"}, Filters: []string{"status"}, MaxRows: 5, MaxBytes: 4096, TimeoutSeconds: 1, MaxWindowSeconds: 3600, RetentionSeconds: 3600}
	q := domain.SourceQuery{ProjectID: d.ProjectID, ProductID: d.ProductID, SourceID: d.ID, Purpose: "diagnosis", Start: start, End: end, Fields: []string{"event_id", "event_time", "status"}, Limit: 5, IdempotencyKey: "query-1"}
	timestamp, _ := json.Marshal(start.Add(time.Minute))
	envelope := domain.SourceEnvelope{SchemaVersion: d.SchemaVersion, Revision: "source-1", Start: start, End: end, Watermark: end, Complete: true, Offsets: map[string]int64{"orders-0": 12}, Rows: []map[string]json.RawMessage{{"event_id": json.RawMessage(`"event-1"`), "event_time": timestamp, "status": json.RawMessage(`"accepted"`)}}}
	return d, q, envelope
}

func TestBoundedSourceQuery(t *testing.T) {
	var result domain.SourceEnvelope
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost {
			t.Error("source query must use fixed read protocol")
		}
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer server.Close()
	d, q, base := fixture(server.URL)
	result = base
	r, err := New([]domain.SourceDescriptor{d})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Query(context.Background(), d, q)
	if err != nil || !out.Complete || out.Offsets["orders-0"] != 12 {
		t.Fatalf("query: %+v %v", out, err)
	}
	for name, mutate := range map[string]func(*domain.SourceEnvelope){
		"wrong schema":     func(v *domain.SourceEnvelope) { v.SchemaVersion = "other" },
		"missing offsets":  func(v *domain.SourceEnvelope) { v.Offsets = nil },
		"duplicate event":  func(v *domain.SourceEnvelope) { v.Rows = append(v.Rows, v.Rows[0]) },
		"foreign field":    func(v *domain.SourceEnvelope) { v.Rows[0]["secret"] = json.RawMessage(`"must-not-retain"`) },
		"null metric":      func(v *domain.SourceEnvelope) { v.Rows[0]["status"] = json.RawMessage(`null`) },
		"out of window":    func(v *domain.SourceEnvelope) { v.Rows[0]["event_time"], _ = json.Marshal(q.End) },
		"wrong field type": func(v *domain.SourceEnvelope) { v.Rows[0]["status"] = json.RawMessage(`123`) },
	} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(base)
			_ = json.Unmarshal(raw, &result)
			mutate(&result)
			if _, err := r.Query(context.Background(), d, q); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
	_, _, result = fixture(server.URL)
	result.Watermark = q.Start
	out, err = r.Query(context.Background(), d, q)
	if err != nil || out.Complete {
		t.Fatal("lagging watermark must report incomplete coverage")
	}
	before := calls.Load()
	for _, change := range []func(*domain.SourceQuery){func(v *domain.SourceQuery) { v.Fields = append(v.Fields, "secret") }, func(v *domain.SourceQuery) { v.ProjectID = "foreign" }, func(v *domain.SourceQuery) { v.Purpose = "reset_offsets" }, func(v *domain.SourceQuery) { v.Limit = 6 }, func(v *domain.SourceQuery) { v.End = v.Start.Add(2 * time.Hour) }} {
		bad := q
		bad.Fields = append([]string(nil), q.Fields...)
		change(&bad)
		if _, err = r.Query(context.Background(), d, bad); err == nil {
			t.Fatal("invalid query accepted")
		}
	}
	if calls.Load() != before {
		t.Fatal("denied query reached data source")
	}
}

func TestSourceTransportLimits(t *testing.T) {
	for _, scenario := range []string{"redirect", "bytes", "timeout", "trailing"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch scenario {
				case "redirect":
					http.Redirect(w, r, "https://example.invalid", 307)
				case "bytes":
					_, _ = w.Write(make([]byte, 8192))
				case "timeout":
					select {
					case <-r.Context().Done():
					case <-time.After(100 * time.Millisecond):
					}
				case "trailing":
					_, _ = w.Write([]byte(`{} {}`))
				}
			}))
			defer server.Close()
			d, q, _ := fixture(server.URL)
			registry, err := New([]domain.SourceDescriptor{d})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if _, err = registry.Query(ctx, d, q); err == nil {
				t.Fatal("unbounded source accepted")
			}
		})
	}
	for _, endpoint := range []string{"http://example.invalid", "file:///tmp/source", "https://user:secret@example.invalid/data", "http://localhost/data"} {
		d, _, _ := fixture(endpoint)
		if _, err := New([]domain.SourceDescriptor{d}); err == nil {
			t.Fatal("unsafe endpoint accepted")
		}
	}
}

func TestSourceCredentialIsPrivateAndNotDiscovered(t *testing.T) {
	var envelope domain.SourceEnvelope
	var called atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic-source-token" {
			t.Error("source credential not attached correctly")
		}
		_ = json.NewEncoder(w).Encode(envelope)
	}))
	defer server.Close()
	d, q, response := fixture(server.URL)
	envelope = response
	d.TokenFile = filepath.Join(t.TempDir(), "source-token")
	if err := os.WriteFile(d.TokenFile, []byte("synthetic-source-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := New([]domain.SourceDescriptor{d})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Query(context.Background(), d, q); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(d.TokenFile, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Query(context.Background(), d, q); err == nil {
		t.Fatal("readable credential accepted")
	}
	if called.Load() != 1 {
		t.Fatal("unsafe credential request reached source")
	}
	if listed := r.List(d.ProjectID); len(listed) != 1 || listed[0].TokenFile != "" || listed[0].Endpoint != "" {
		t.Fatal("source discovery exposed credentials")
	}
}
