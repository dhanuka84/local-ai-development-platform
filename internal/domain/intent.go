package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const IntentSchema = "hybrid-ai/sdlc-intent/v1"

// An intent is accepted through exact-version QA and human decision gates.
// A proposed specification grants no execution or publication authority.
type IntentSpecification struct {
	Schema         string             `json:"schema"`
	Goal           string             `json:"goal"`
	Stage          string             `json:"stage"`
	Bindings       []ProductBinding   `json:"bindings"`
	Criteria       []IntentCriterion  `json:"criteria"`
	Assumptions    []IntentAssumption `json:"assumptions"`
	Clarifications []string           `json:"clarifications"`
}

type ProductBinding struct {
	RecordID string `json:"record_id"`
	SHA256   string `json:"sha256"`
	Kind     string `json:"kind"`
}

type IntentAssumption struct {
	Statement string `json:"statement"`
	Confirmed bool   `json:"confirmed"`
}

type IntentCriterion struct {
	ID               string                `json:"id"`
	Statement        string                `json:"statement"`
	Oracle           string                `json:"oracle"`
	WorkPacketSHA256 string                `json:"work_packet_sha256,omitempty"`
	Reconciliation   *ReconciliationOracle `json:"reconciliation,omitempty"`
}

// Reconciliation checks a declared business requirement in one observed
// window. It does not establish the cause of an incident.
type ReconciliationOracle struct {
	LeftSourceID       string `json:"left_source_id"`
	RightSourceID      string `json:"right_source_id"`
	LeftSchemaVersion  string `json:"left_schema_version"`
	RightSchemaVersion string `json:"right_schema_version"`
	JoinField          string `json:"join_field"`
	ValueField         string `json:"value_field"`
	ExpectedRightValue string `json:"expected_right_value"`
	MaxWindowSeconds   int    `json:"max_window_seconds"`
}

func ParseIntent(content string) (IntentSpecification, error) {
	var spec IntentSpecification
	decoder := json.NewDecoder(bytes.NewBufferString(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return spec, fmt.Errorf("invalid intent document: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return spec, errors.New("intent requires exactly one JSON document")
	}
	return spec, spec.Validate()
}

func boundedText(v string, n int) bool { return strings.TrimSpace(v) != "" && len(v) <= n }

func (s IntentSpecification) Validate() error {
	if s.Schema != IntentSchema || !boundedText(s.Goal, 4096) || len(s.Bindings) < 1 || len(s.Bindings) > 12 || len(s.Criteria) < 1 || len(s.Criteria) > 20 || len(s.Assumptions) > 20 || len(s.Clarifications) > 20 {
		return errors.New("intent schema, goal, bindings and bounded criteria are required")
	}
	switch s.Stage {
	case "discover", "define", "plan", "design", "implement", "review", "test", "secure", "release", "deploy", "operate", "diagnose", "recover", "improve", "retire":
	default:
		return errors.New("unknown SDLC stage")
	}
	seen, brs := map[string]bool{}, false
	for _, b := range s.Bindings {
		if !ValidProductKey(b.RecordID) || !digestPattern.MatchString(b.SHA256) || seen[b.RecordID] {
			return errors.New("intent bindings require unique exact records and digests")
		}
		switch b.Kind {
		case "brs", "feature", "code", "test", "release", "incident", "procedure":
		default:
			return errors.New("unsupported intent binding kind")
		}
		brs = brs || b.Kind == "brs"
		seen[b.RecordID] = true
	}
	if !brs {
		return errors.New("an intent must bind accepted business requirements")
	}
	seen = map[string]bool{}
	for _, c := range s.Criteria {
		if !ValidProductKey(c.ID) || !boundedText(c.Statement, 2048) || seen[c.ID] {
			return errors.New("criteria require unique IDs and explicit statements")
		}
		seen[c.ID] = true
		switch c.Oracle {
		case "work_packet":
			if !digestPattern.MatchString(c.WorkPacketSHA256) || c.Reconciliation != nil {
				return errors.New("work-packet criteria require the exact protected packet digest")
			}
		case "observation_reconciliation":
			r := c.Reconciliation
			if c.WorkPacketSHA256 != "" || r == nil || !ValidProductKey(r.LeftSourceID) || !ValidProductKey(r.RightSourceID) || r.LeftSourceID == r.RightSourceID || !boundedText(r.LeftSchemaVersion, 128) || !boundedText(r.RightSchemaVersion, 128) || !ValidProductKey(r.JoinField) || !ValidProductKey(r.ValueField) || !boundedText(r.ExpectedRightValue, 512) || r.MaxWindowSeconds < 1 || r.MaxWindowSeconds > 31*24*3600 {
				return errors.New("reconciliation requires two versioned sources, join, expected value and bounded window")
			}
		default:
			return errors.New("criterion oracle is not supported")
		}
	}
	for _, a := range s.Assumptions {
		if !boundedText(a.Statement, 2048) {
			return errors.New("invalid assumption")
		}
	}
	for _, c := range s.Clarifications {
		if !boundedText(c, 2048) {
			return errors.New("invalid clarification")
		}
	}
	return nil
}

type IntentContext struct {
	Intent        ProductBinding      `json:"intent"`
	Specification IntentSpecification `json:"specification"`
	Records       []ProductRecord     `json:"records"`
	Ready         bool                `json:"ready"`
	Blockers      []string            `json:"blockers"`
}
