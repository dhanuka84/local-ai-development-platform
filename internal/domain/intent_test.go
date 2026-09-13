package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestIntentContractRejectsAmbiguousCriteria(t *testing.T) {
	valid := IntentSpecification{Schema: IntentSchema, Stage: "implement", Goal: "Export valid CSV", Bindings: []ProductBinding{{RecordID: "brs-1", Kind: "brs", SHA256: Digest([]byte("brs"))}}, Criteria: []IntentCriterion{{ID: "csv", Statement: "Accepted CSV cases pass", Oracle: "work_packet", WorkPacketSHA256: Digest([]byte("packet"))}}}
	for _, tc := range []struct {
		name   string
		change func(*IntentSpecification)
	}{
		{"unknown oracle", func(s *IntentSpecification) { s.Criteria[0].Oracle = "model_says_pass" }},
		{"duplicate criterion", func(s *IntentSpecification) { s.Criteria = append(s.Criteria, s.Criteria[0]) }},
		{"unbound packet", func(s *IntentSpecification) { s.Criteria[0].WorkPacketSHA256 = "latest" }},
		{"missing BRS", func(s *IntentSpecification) { s.Bindings[0].Kind = "code" }},
		{"duplicate binding", func(s *IntentSpecification) { s.Bindings = append(s.Bindings, s.Bindings[0]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(valid)
			var changed IntentSpecification
			_ = json.Unmarshal(raw, &changed)
			tc.change(&changed)
			if changed.Validate() == nil {
				t.Fatal("unsafe criterion contract accepted")
			}
		})
	}
	raw, _ := json.Marshal(valid)
	if _, err := ParseIntent(string(raw)); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{string(raw) + ` {}`, strings.Replace(string(raw), `"goal":`, `"unknown":true,"goal":`, 1)} {
		if _, err := ParseIntent(content); err == nil {
			t.Fatal("noncanonical contract shape accepted")
		}
	}
}

func TestObservationReconciliationRequiresEvidence(t *testing.T) {
	start := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	rule := ReconciliationOracle{LeftSourceID: "event", RightSourceID: "audit", LeftSchemaVersion: "orders/v1", RightSchemaVersion: "orders/v1", JoinField: "order_id", ValueField: "status", ExpectedRightValue: "processed", MaxWindowSeconds: 3600}
	row := func(id, status string) map[string]json.RawMessage {
		a, _ := json.Marshal(id)
		b, _ := json.Marshal(status)
		return map[string]json.RawMessage{"order_id": a, "status": b}
	}
	fixture := func() SourceEnvelope {
		return SourceEnvelope{SchemaVersion: "orders/v1", Start: start, End: start.Add(time.Hour), Watermark: start.Add(time.Hour), Complete: true, Rows: []map[string]json.RawMessage{row("1", "processed")}}
	}
	for _, tc := range []struct {
		name, outcome string
		change        func(*SourceEnvelope, *SourceEnvelope)
	}{
		{"complete matching", "satisfied", func(a, b *SourceEnvelope) {}},
		{"wrong result", "violated", func(a, b *SourceEnvelope) { b.Rows[0] = row("1", "failed") }},
		{"complete missing result", "violated", func(a, b *SourceEnvelope) { b.Rows = nil }},
		{"delayed missing result", "inconclusive", func(a, b *SourceEnvelope) { b.Rows = nil; b.Watermark = start }},
		{"partial matching", "inconclusive", func(a, b *SourceEnvelope) { b.Complete = false }},
		{"empty sample", "inconclusive", func(a, b *SourceEnvelope) { a.Rows = nil; b.Rows = nil }},
		{"duplicate join", "inconclusive", func(a, b *SourceEnvelope) { b.Rows = append(b.Rows, row("1", "processed")) }},
		{"different window", "inconclusive", func(a, b *SourceEnvelope) { b.End = b.End.Add(time.Second) }},
		{"different schema", "inconclusive", func(a, b *SourceEnvelope) { b.SchemaVersion = "orders/v2" }},
		{"missing join field", "inconclusive", func(a, b *SourceEnvelope) { delete(a.Rows[0], "order_id") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := fixture(), fixture()
			tc.change(&a, &b)
			out := CompareObservations(rule, a, b)
			if out.Outcome != tc.outcome || len(out.Reasons) == 0 {
				t.Fatalf("got %+v", out)
			}
		})
	}
}
