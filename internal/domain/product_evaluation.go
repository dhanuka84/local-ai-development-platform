package domain

import (
	"context"
	"encoding/json"
	"time"
)

type ObservationEvaluation struct {
	ID            string         `json:"id"`
	ProjectID     string         `json:"project_id"`
	ProductID     string         `json:"product_id"`
	Actor         string         `json:"actor"`
	Owner         string         `json:"owner"`
	Intent        ProductBinding `json:"intent"`
	CriterionID   string         `json:"criterion_id"`
	Left          ProductBinding `json:"left"`
	Right         ProductBinding `json:"right"`
	RequestSHA256 string         `json:"request_sha256"`
	Method        string         `json:"method"`
	Outcome       string         `json:"outcome"`
	Matched       int            `json:"matched"`
	Missing       int            `json:"missing"`
	Mismatched    int            `json:"mismatched"`
	Reasons       []string       `json:"reasons"`
	CreatedAt     time.Time      `json:"created_at"`
	Evidence      Artifact       `json:"evidence"`
}

type ProductEvaluationRepository interface {
	RecordProductEvaluation(context.Context, ObservationEvaluation) (ObservationEvaluation, error)
	GetProductEvaluation(context.Context, string, string) (ObservationEvaluation, error)
}

// CompareObservations is a deterministic, versioned evaluator. Empty or
// partial observations cannot satisfy a criterion, and missing rows only
// establish a violation when the right-hand observation covers the window.
func CompareObservations(rule ReconciliationOracle, left, right SourceEnvelope) ObservationEvaluation {
	out := ObservationEvaluation{Method: "hybrid-ai/observation-reconciliation/v1", Outcome: "inconclusive", Reasons: []string{}}
	if left.SchemaVersion != rule.LeftSchemaVersion || right.SchemaVersion != rule.RightSchemaVersion || left.Start.IsZero() || !left.End.After(left.Start) || !left.Start.Equal(right.Start) || !left.End.Equal(right.End) || left.End.Sub(left.Start) > time.Duration(rule.MaxWindowSeconds)*time.Second {
		out.Reasons = append(out.Reasons, "Source schema or time windows do not match the accepted criterion.")
		return out
	}
	leftKeys, rightValues := map[string]bool{}, map[string]string{}
	stringField := func(row map[string]json.RawMessage, field string) (string, bool) {
		var value string
		err := json.Unmarshal(row[field], &value)
		return value, err == nil && value != ""
	}
	for _, row := range left.Rows {
		key, ok := stringField(row, rule.JoinField)
		if !ok || leftKeys[key] {
			out.Reasons = append(out.Reasons, "Left join keys are missing, duplicated or not strings.")
			return out
		}
		leftKeys[key] = true
	}
	for _, row := range right.Rows {
		key, ok := stringField(row, rule.JoinField)
		value, valid := stringField(row, rule.ValueField)
		_, duplicate := rightValues[key]
		if !ok || !valid || duplicate {
			out.Reasons = append(out.Reasons, "Right join keys or values are missing, duplicated or not strings.")
			return out
		}
		rightValues[key] = value
	}
	for key := range leftKeys {
		value, ok := rightValues[key]
		if !ok {
			out.Missing++
		} else if value != rule.ExpectedRightValue {
			out.Mismatched++
		} else {
			out.Matched++
		}
	}
	leftComplete := left.Complete && !left.Watermark.Before(left.End)
	rightComplete := right.Complete && !right.Watermark.Before(right.End)
	switch {
	case out.Mismatched > 0 || (out.Missing > 0 && rightComplete):
		out.Outcome = "violated"
		out.Reasons = append(out.Reasons, "Observed records violate the accepted requirement; this does not identify a root cause.")
	case len(leftKeys) == 0:
		out.Reasons = append(out.Reasons, "No eligible input events were observed; an empty sample cannot prove recovery.")
	case !leftComplete || !rightComplete:
		out.Reasons = append(out.Reasons, "Coverage is incomplete; delayed or missing evidence prevents satisfaction.")
	default:
		out.Outcome = "satisfied"
		out.Reasons = append(out.Reasons, "All observed input records meet the accepted criterion in this complete window.")
	}
	return out
}
