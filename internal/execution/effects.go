package execution

import (
	"encoding/json"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

// Credentials stay in the executor's configuration. The frozen target binds
// the public route, repository and artifact/staging roots by digest.
type DeliveryConfig struct {
	ForgeURL              string `json:"forge_url"`
	Repository            string `json:"repository"`
	BaseBranch            string `json:"base_branch"`
	TokenFile             string `json:"token_file"`
	ArtifactRoot          string `json:"artifact_root"`
	StagingRoot           string `json:"staging_root"`
	ExpectedReleaseSHA256 string `json:"expected_release_sha256,omitempty"`
}

func (c DeliveryConfig) Digest() string {
	c.TokenFile = ""
	raw, _ := json.Marshal(c)
	return domain.Digest(raw)
}

type Remedy struct {
	ID        string          `json:"id"`
	Endpoint  string          `json:"endpoint"`
	TokenFile string          `json:"token_file"`
	Before    json.RawMessage `json:"before"`
	Desired   json.RawMessage `json:"desired"`
}
type RemediationConfig struct {
	Remedies []Remedy `json:"remedies"`
}

func (c RemediationConfig) Digest() string {
	copy := append([]Remedy{}, c.Remedies...)
	for i := range copy {
		copy[i].TokenFile = ""
	}
	raw, _ := json.Marshal(copy)
	return domain.Digest(raw)
}

type EffectReceipt struct {
	Schema          string `json:"schema"`
	RunID           string `json:"run_id"`
	StepID          string `json:"step_id"`
	Kind            string `json:"kind"`
	ContractSHA256  string `json:"contract_sha256"`
	ExternalID      string `json:"external_id"`
	BeforeSHA256    string `json:"before_sha256"`
	AfterSHA256     string `json:"after_sha256"`
	ObservedSHA256  string `json:"observed_sha256"`
	CandidateSHA256 string `json:"candidate_sha256,omitempty"`
	BaseRevision    string `json:"base_revision,omitempty"`
	Commit          string `json:"commit,omitempty"`
	Raw             string `json:"raw"`
}
