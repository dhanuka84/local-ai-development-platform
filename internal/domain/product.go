package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ProductRecord is an immutable version. Accepted heads and projection receipts
// are separate mutable pointers; generated proposals never advance a head.
type ProductRecord struct {
	ID             string           `json:"id"`
	ProjectID      string           `json:"project_id"`
	ProductID      string           `json:"product_id"`
	Key            string           `json:"key"`
	Kind           string           `json:"kind"`
	Version        int              `json:"version"`
	Title          string           `json:"title"`
	Content        string           `json:"content"`
	Classification string           `json:"classification"`
	Origin         string           `json:"origin"`
	Status         string           `json:"status"`
	Owner          string           `json:"owner"`
	Actor          string           `json:"actor"`
	Source         ProductSourceRef `json:"source"`
	SHA256         string           `json:"sha256"`
	Evidence       Artifact         `json:"evidence"`
	CreatedAt      time.Time        `json:"created_at"`
	ExpiresAt      *time.Time       `json:"expires_at,omitempty"`
}

type ProductSourceRef struct {
	SourceID      string            `json:"source_id"`
	Revision      string            `json:"revision"`
	SchemaVersion string            `json:"schema_version"`
	ReceiptID     string            `json:"receipt_id,omitempty"`
	Start         *time.Time        `json:"start,omitempty"`
	End           *time.Time        `json:"end,omitempty"`
	Watermark     *time.Time        `json:"watermark,omitempty"`
	CollectedAt   *time.Time        `json:"collected_at,omitempty"`
	Snapshot      string            `json:"snapshot,omitempty"`
	Offsets       map[string]int64  `json:"offsets,omitempty"`
	Complete      bool              `json:"complete"`
	Purpose       string            `json:"purpose,omitempty"`
	Fields        []string          `json:"fields,omitempty"`
	Filters       map[string]string `json:"filters,omitempty"`
	RowLimit      int               `json:"row_limit,omitempty"`
}

type ProductRecordInput struct {
	ProjectID       string     `json:"project_id"`
	ProductID       string     `json:"product_id"`
	Key             string     `json:"key"`
	Kind            string     `json:"kind"`
	ExpectedVersion int        `json:"expected_version"`
	Title           string     `json:"title"`
	Content         string     `json:"content"`
	Classification  string     `json:"classification"`
	SourceID        string     `json:"source_id"`
	SourceRevision  string     `json:"source_revision"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
}

type ProductDecision struct {
	ProjectID      string `json:"project_id"`
	RecordID       string `json:"record_id"`
	ExpectedSHA256 string `json:"expected_sha256"`
	Decision       string `json:"decision"`
	Reason         string `json:"reason"`
	ValidationID   string `json:"validation_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Actor          string `json:"-"`
}

type ProductValidation struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	RecordID   string    `json:"record_id"`
	SHA256     string    `json:"sha256"`
	Actor      string    `json:"actor"`
	Method     string    `json:"method"`
	Evidence   Artifact  `json:"evidence"`
	ValidUntil time.Time `json:"valid_until"`
}

type ProductRelation struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	ProductID string `json:"product_id"`
	FromID    string `json:"from_id"`
	ToID      string `json:"to_id"`
	Kind      string `json:"kind"`
	Evidence  string `json:"evidence"`
	Actor     string `json:"actor"`
}

type ProductContextRequest struct {
	ProjectID string   `json:"project_id"`
	ProductID string   `json:"product_id"`
	Query     string   `json:"query"`
	RootIDs   []string `json:"root_ids,omitempty"`
	Limit     int      `json:"limit"`
	MaxBytes  int      `json:"max_bytes"`
}

type ProductContext struct {
	Records   []ProductRecord   `json:"records"`
	Relations []ProductRelation `json:"relations"`
	Backend   string            `json:"backend"`
	Truncated bool              `json:"truncated"`
	Bytes     int               `json:"bytes"`
	Warnings  []string          `json:"warnings"`
}

type ProductProjection struct {
	RecordID  string `json:"record_id"`
	SHA256    string `json:"sha256"`
	Model     string `json:"model"`
	Dimension int    `json:"dimension"`
}

func (p ProductProjection) Digest() string {
	raw, _ := json.Marshal(p)
	return Digest(raw)
}

var productKey = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,127}$`)

func ValidProductKey(value string) bool { return productKey.MatchString(value) }

func ValidateProductInput(in ProductRecordInput) error {
	if !ValidProductKey(in.ProjectID) || !ValidProductKey(in.ProductID) || !ValidProductKey(in.Key) ||
		in.ExpectedVersion < 0 || strings.TrimSpace(in.Title) == "" || len(in.Title) > 240 ||
		strings.TrimSpace(in.Content) == "" || len(in.Content) > 128*1024 ||
		strings.TrimSpace(in.SourceID) == "" || len(in.SourceID) > 256 ||
		strings.TrimSpace(in.SourceRevision) == "" || len(in.SourceRevision) > 256 {
		return fmt.Errorf("invalid product identity, source, version or content bounds")
	}
	switch in.Kind {
	case "product", "brs", "feature", "code", "test", "release", "incident", "hypothesis", "procedure", "intent":
	default:
		return fmt.Errorf("unsupported product record kind")
	}
	switch in.Classification {
	case "public", "internal", "confidential", "restricted":
	default:
		return fmt.Errorf("classification is required")
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(time.Now()) {
		return fmt.Errorf("record is already expired")
	}
	if in.Kind == "intent" {
		_, err := ParseIntent(in.Content)
		return err
	}
	return nil
}

func (r ProductRecord) Digest() string {
	raw, _ := json.Marshal([]any{r.ProjectID, r.ProductID, r.Key, r.Kind, r.Version, r.Title,
		r.Content, r.Classification, r.Origin, r.Owner, r.Actor, r.Source, r.ExpiresAt})
	return Digest(raw)
}

func (r ProductRecord) Eligible() bool {
	return (r.Status == "accepted" || r.Status == "observed") && (r.ExpiresAt == nil || r.ExpiresAt.After(time.Now()))
}

func (r ProductRecord) RetrievalText() string {
	return r.Kind + " " + r.Title + "\n" + r.Content
}

func ValidProductRelation(kind string) bool {
	switch kind {
	case "contains", "specifies", "implements", "tests", "deployed_as", "observed_in", "affects", "supports", "contradicts", "supersedes":
		return true
	}
	return false
}

type ProductRepository interface {
	PutProductRecord(context.Context, ProductRecord, int) (ProductRecord, error)
	GetProductRecord(context.Context, string, string, bool) (ProductRecord, error)
	DecideProductRecord(context.Context, ProductDecision) (ProductRecord, error)
	ValidateProductRecord(context.Context, ProductValidation) error
	SearchProductRecords(context.Context, string, string, string, int) ([]ProductRecord, error)
	PutProductRelation(context.Context, ProductRelation) (ProductRelation, error)
	ProductRelations(context.Context, string, string, []string, int) ([]ProductRelation, error)
	RecordProductProjection(context.Context, ProductProjection) error
	ProductProjection(context.Context, string) (ProductProjection, error)
	ProductRecordForIndex(context.Context, string) (ProductRecord, error)
}

type ProductVectorStore interface {
	UpsertProductRecord(context.Context, ProductRecord, ProductProjection, []float32) error
	SearchProductRecords(context.Context, string, string, []float32, int) ([]VectorHit, error)
	VerifyProductProjection(context.Context, ProductRecord, ProductProjection) error
}

type ProductGraphProjector interface {
	ProjectProductRecord(context.Context, ProductRecord, []ProductRelation) error
}
