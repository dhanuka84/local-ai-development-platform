// Package workpacket defines the bounded execution contract used by the
// OpenClaw orchestration layer. It does not call a model or persist knowledge.
package workpacket

const SchemaVersion = "hybrid-ai/work-packet/v1"

const (
	ModeReadOnly = "readonly"
	ModePatch    = "patch"

	TaskDevelopment = "development"
	TaskMaintenance = "maintenance"

	DataPublic       = "public"
	DataInternal     = "internal"
	DataConfidential = "confidential"
	DataRestricted   = "restricted"
)

// Packet is an explicit contract between the OpenClaw coordinator and a local
// execution worker. Commands are argv arrays by design; they are never passed
// through a shell by the verifier.
type Packet struct {
	SchemaVersion      string   `json:"schema_version"`
	ID                 string   `json:"id"`
	Goal               string   `json:"goal"`
	Workspace          string   `json:"workspace"`
	BaseRevision       string   `json:"base_revision,omitempty"`
	Mode               string   `json:"mode"`
	TaskClass          string   `json:"task_class"`
	DataClassification string   `json:"data_classification"`
	Categories         []string `json:"categories,omitempty"`
	LocalOnly          bool     `json:"local_only"`
	CloudReview        bool     `json:"cloud_review"`
	CloudProvider      string   `json:"cloud_provider,omitempty"`
	CloudApprovedBy    string   `json:"cloud_approved_by,omitempty"`
	Destructive        bool     `json:"destructive"`
	ApprovedBy         string   `json:"approved_by,omitempty"`
	AllowedFiles       []string `json:"allowed_files,omitempty"`
	ForbiddenFiles     []string `json:"forbidden_files,omitempty"`
	Rollback           []string `json:"rollback,omitempty"`
	Checks             []Check  `json:"checks,omitempty"`
	Limits             Limits   `json:"limits"`
}

type Check struct {
	Name           string   `json:"name"`
	Argv           []string `json:"argv"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

type Limits struct {
	MaxChangedFiles int `json:"max_changed_files"`
	MaxDiffLines    int `json:"max_diff_lines"`
	MaxPatchBytes   int `json:"max_patch_bytes,omitempty"`
}

type Evaluation struct {
	Allowed                 bool     `json:"allowed"`
	Risk                    string   `json:"risk"`
	Route                   string   `json:"route"`
	RequiresApproval        bool     `json:"requires_approval"`
	EffectiveForbiddenFiles []string `json:"effective_forbidden_files"`
	Errors                  []string `json:"errors,omitempty"`
	Warnings                []string `json:"warnings,omitempty"`
}

type FileChange struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type CheckResult struct {
	Name     string   `json:"name"`
	Argv     []string `json:"argv"`
	ExitCode int      `json:"exit_code"`
	Passed   bool     `json:"passed"`
	Output   string   `json:"output,omitempty"`
}

type VerificationResult struct {
	Accepted       bool          `json:"accepted"`
	Evaluation     Evaluation    `json:"evaluation"`
	BaseRevision   string        `json:"base_revision,omitempty"`
	ChangedFiles   []FileChange  `json:"changed_files,omitempty"`
	TotalDiffLines int           `json:"total_diff_lines"`
	Checks         []CheckResult `json:"checks,omitempty"`
	Errors         []string      `json:"errors,omitempty"`
}
