package domain

import (
	"context"
	"encoding/json"
	"time"
)

// SourceDescriptor is operator-controlled configuration, never a model-supplied
// URL, SQL statement, consumer group or authentication header.
type SourceDescriptor struct {
	ID                string              `json:"id"`
	ProjectID         string              `json:"project_id"`
	ProductID         string              `json:"product_id"`
	Kind              string              `json:"kind"`
	Endpoint          string              `json:"endpoint"`
	TokenFile         string              `json:"token_file,omitempty"`
	SchemaVersion     string              `json:"schema_version"`
	AdapterSHA256     string              `json:"adapter_sha256,omitempty"`
	Classification    string              `json:"classification"`
	Owner             string              `json:"owner"`
	Roles             []string            `json:"roles"`
	Purposes          []string            `json:"purposes"`
	Fields            map[string]string   `json:"fields"`
	FieldsByRole      map[string][]string `json:"fields_by_role,omitempty"`
	Filters           []string            `json:"filters"`
	MaxRows           int                 `json:"max_rows"`
	MaxBytes          int64               `json:"max_bytes"`
	TimeoutSeconds    int                 `json:"timeout_seconds"`
	MaxWindowSeconds  int                 `json:"max_window_seconds"`
	RetentionSeconds  int                 `json:"retention_seconds"`
	AllowLoopbackHTTP bool                `json:"allow_loopback_http,omitempty"`
}

type SourceQuery struct {
	ProjectID      string            `json:"project_id"`
	ProductID      string            `json:"product_id"`
	SourceID       string            `json:"source_id"`
	Purpose        string            `json:"purpose"`
	Start          time.Time         `json:"start"`
	End            time.Time         `json:"end"`
	Fields         []string          `json:"fields"`
	Filters        map[string]string `json:"filters,omitempty"`
	Limit          int               `json:"limit"`
	IdempotencyKey string            `json:"idempotency_key"`
}

// SourceEnvelope is a bounded read-only connector protocol. The connector owns
// translating a reviewed view to native log/metric/event/lake/audit queries.
type SourceEnvelope struct {
	SchemaVersion string                       `json:"schema_version"`
	AdapterSHA256 string                       `json:"adapter_sha256,omitempty"`
	Revision      string                       `json:"revision"`
	Start         time.Time                    `json:"start"`
	End           time.Time                    `json:"end"`
	Watermark     time.Time                    `json:"watermark"`
	Snapshot      string                       `json:"snapshot,omitempty"`
	Offsets       map[string]int64             `json:"offsets,omitempty"`
	Complete      bool                         `json:"complete"`
	Rows          []map[string]json.RawMessage `json:"rows"`
}

type SourceReceipt struct {
	ID               string           `json:"id"`
	ProjectID        string           `json:"project_id"`
	ProductID        string           `json:"product_id"`
	SourceID         string           `json:"source_id"`
	Actor            string           `json:"actor"`
	Role             string           `json:"role"`
	Owner            string           `json:"owner"`
	Purpose          string           `json:"purpose"`
	QuerySHA256      string           `json:"query_sha256"`
	DescriptorSHA256 string           `json:"descriptor_sha256"`
	IdempotencyKey   string           `json:"idempotency_key"`
	Status           string           `json:"status"`
	Reason           string           `json:"reason"`
	CollectedAt      time.Time        `json:"collected_at"`
	Source           ProductSourceRef `json:"source"`
	Evidence         Artifact         `json:"evidence"`
	RecordID         string           `json:"record_id"`
}

type SourceRepository interface {
	RecordSourceObservation(context.Context, SourceReceipt, *ProductRecord) error
	GetSourceReceipt(context.Context, string, string, string, string) (SourceReceipt, error)
}
