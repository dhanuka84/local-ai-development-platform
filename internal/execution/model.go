package execution

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type ModelRequest struct {
	Model     string          `json:"model"`
	System    string          `json:"system"`
	Prompt    string          `json:"prompt"`
	Format    json.RawMessage `json:"format"`
	Stream    bool            `json:"stream"`
	Think     bool            `json:"think"`
	Options   ModelOptions    `json:"options"`
	KeepAlive string          `json:"keep_alive"`
}

// Constrained decoding complements server-side validation; it grants no
// authority and cannot establish that a candidate passes its product tests.
func ProposalFormat(role string) json.RawMessage {
	if role == "sdlc_builder" {
		return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["schema","plan","criteria","base_revision","files"],"properties":{"schema":{"const":"hybrid-ai/builder-proposal/v1"},"plan":{"type":"array","minItems":1,"maxItems":20,"items":{"type":"object","additionalProperties":false,"required":["description","criteria"],"properties":{"description":{"type":"string"},"criteria":{"type":"array","items":{"type":"string"}}}}},"criteria":{"type":"array","minItems":1,"items":{"type":"string"}},"base_revision":{"type":"string"},"files":{"type":"object","minProperties":1,"maxProperties":30,"additionalProperties":{"type":"string"}}}}`)
	}
	return json.RawMessage(`"json"`)
}

func ValidProposalFormat(role string, raw json.RawMessage) bool {
	var a, b any
	if json.Unmarshal(raw, &a) != nil || json.Unmarshal(ProposalFormat(role), &b) != nil {
		return false
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

type ModelOptions struct {
	NumPredict  int     `json:"num_predict"`
	Temperature float64 `json:"temperature"`
	Seed        int     `json:"seed"`
}
type ModelResponse struct {
	Model           string `json:"model"`
	Response        string `json:"response"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
}
type BuilderProposal struct {
	Schema       string                     `json:"schema"`
	Plan         []domain.ExecutionPlanStep `json:"plan"`
	Criteria     []string                   `json:"criteria"`
	BaseRevision string                     `json:"base_revision"`
	Patch        string                     `json:"patch"`
	Files        map[string]string          `json:"files,omitempty"`
}
type DiagnosisProposal struct {
	Schema     string                       `json:"schema"`
	Criteria   []string                     `json:"criteria"`
	Summary    string                       `json:"summary"`
	Hypotheses []domain.ExecutionHypothesis `json:"hypotheses"`
}
type DisclosureManifest struct {
	Schema             string                  `json:"schema"`
	RunID              string                  `json:"run_id"`
	StepID             string                  `json:"step_id"`
	ContextSHA256      string                  `json:"context_sha256"`
	PackageSHA256      string                  `json:"package_sha256"`
	RepositoryID       string                  `json:"repository_id"`
	RepositoryRevision string                  `json:"repository_revision"`
	Files              map[string]string       `json:"files"`
	Observations       []domain.ProductBinding `json:"observations"`
	Provider           string                  `json:"provider"`
	Model              string                  `json:"model"`
	ModelSHA256        string                  `json:"model_sha256"`
}

func DecodeProposal(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if !errors.Is(d.Decode(new(any)), io.EOF) {
		return errors.New("one strict JSON proposal required")
	}
	return nil
}
