// Package sourceadapter translates bounded reviewed views to native protocols.
// Its credentials are operator-owned files and never enter model context.
package sourceadapter

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
	"github.com/dhanuka84/hybrid-ai-platform/internal/mcpclient"
	"github.com/dhanuka84/hybrid-ai-platform/internal/sources"
)

type Backend struct {
	Kind                string   `json:"kind"`
	Endpoint            string   `json:"endpoint,omitempty"`
	CredentialFile      string   `json:"credential_file,omitempty"`
	DatabaseURLFile     string   `json:"database_url_file,omitempty"`
	View                string   `json:"view,omitempty"`
	WatermarkView       string   `json:"watermark_view,omitempty"`
	Query               string   `json:"query,omitempty"`
	WatermarkQuery      string   `json:"watermark_query,omitempty"`
	Brokers             []string `json:"brokers,omitempty"`
	Topic               string   `json:"topic,omitempty"`
	Partitions          []int32  `json:"partitions,omitempty"`
	MaxScanRecords      int      `json:"max_scan_records"`
	Bucket              string   `json:"bucket,omitempty"`
	Object              string   `json:"object,omitempty"`
	VersionID           string   `json:"version_id,omitempty"`
	ExpectedETag        string   `json:"expected_etag,omitempty"`
	Tool                string   `json:"tool,omitempty"`
	AllowPlaintextLocal bool     `json:"allow_plaintext_local"`
	ExpectedStreams     []string `json:"expected_streams,omitempty"`
}

func (b Backend) Digest() string {
	raw, _ := json.Marshal(struct {
		Schema  string
		Backend Backend
	}{"hybrid-ai/native-source/v1", b})
	return domain.Digest(raw)
}

type View struct {
	Contract domain.SourceDescriptor `json:"contract"`
	Backend  Backend                 `json:"backend"`
}
type Config struct {
	Schema      string `json:"schema"`
	Address     string `json:"address"`
	TokenFile   string `json:"token_file"`
	TLSCertFile string `json:"tls_cert_file,omitempty"`
	TLSKeyFile  string `json:"tls_key_file,omitempty"`
	Views       []View `json:"views"`
}
type Adapter struct {
	views  map[string]View
	client *http.Client
	mu     sync.Mutex
	active int
	last   map[string]time.Time
}

var identifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,62}$`)

func validView(name string) bool {
	parts := strings.Split(name, ".")
	return len(parts) == 2 && identifier.MatchString(parts[0]) && identifier.MatchString(parts[1])
}

func New(views []View) (*Adapter, error) {
	a := &Adapter{views: map[string]View{}, last: map[string]time.Time{}, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("native source redirects forbidden") }}}
	if len(views) > 100 {
		return nil, errors.New("native source view count exceeded")
	}
	for _, v := range views {
		if _, err := sources.New([]domain.SourceDescriptor{v.Contract}); err != nil {
			return nil, err
		}
		b := v.Backend
		if v.Contract.AdapterSHA256 != b.Digest() || b.MaxScanRecords < 1 || b.MaxScanRecords > 10000 {
			return nil, errors.New("native view requires exact backend digest and scan bound")
		}
		if len(b.Query) > 4096 || len(b.WatermarkQuery) > 4096 || len(b.Object) > 1024 || len(b.VersionID) > 128 || len(b.ExpectedETag) > 128 {
			return nil, errors.New("native backend configuration exceeds bounds")
		}
		if b.Kind != "postgres" && b.Kind != "kafka" && b.Endpoint == "" {
			return nil, errors.New("native endpoint required")
		}
		switch b.Kind {
		case "postgres":
			if v.Contract.Kind != "audit" || b.DatabaseURLFile == "" || !validView(b.View) || !validView(b.WatermarkView) {
				return nil, errors.New("audit reader requires fixed reviewed data and watermark views")
			}
		case "kafka":
			if v.Contract.Kind != "event" || len(b.Brokers) == 0 || len(b.Brokers) > 8 || !domain.ValidProductKey(b.Topic) || len(b.Partitions) == 0 || len(b.Partitions) > 8 {
				return nil, errors.New("Kafka reader requires fixed topic and bounded partitions")
			}
			seen := map[int32]bool{}
			for _, p := range b.Partitions {
				if p < 0 || seen[p] {
					return nil, errors.New("invalid Kafka partition")
				}
				seen[p] = true
			}
		case "s3":
			if v.Contract.Kind != "lake" || b.Bucket == "" || b.Object == "" || b.CredentialFile == "" {
				return nil, errors.New("lake reader requires fixed bucket, object and credentials")
			}
		case "loki":
			if v.Contract.Kind != "log" || b.Query == "" || len(b.ExpectedStreams) == 0 || len(b.ExpectedStreams) > 8 {
				return nil, errors.New("log reader requires a fixed query and producer watermark streams")
			}
		case "prometheus":
			if v.Contract.Kind != "metric" || b.Query == "" || b.WatermarkQuery == "" {
				return nil, errors.New("metric reader requires fixed value and watermark queries")
			}
		case "mcp":
			if b.Tool == "" || b.CredentialFile == "" {
				return nil, errors.New("MCP view requires a fixed read-only tool and credential")
			}
		default:
			return nil, errors.New("unknown native source kind")
		}
		if b.Endpoint != "" {
			u, err := url.Parse(b.Endpoint)
			if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && b.AllowPlaintextLocal)) {
				return nil, errors.New("invalid native endpoint")
			}
		}
		key := v.Contract.ProjectID + ":" + v.Contract.ID
		if _, exists := a.views[key]; exists {
			return nil, errors.New("duplicate native source view")
		}
		a.views[key] = v
	}
	return a, nil
}

func (a *Adapter) Query(ctx context.Context, q domain.SourceQuery) (out domain.SourceEnvelope, err error) {
	v, ok := a.views[q.ProjectID+":"+q.SourceID]
	if !ok {
		return out, domain.ErrForbidden
	}
	if err = sources.ValidateQuery(v.Contract, q); err != nil {
		return out, err
	}
	a.mu.Lock()
	key := q.ProjectID + ":" + q.SourceID
	if a.active >= 4 || time.Since(a.last[key]) < 20*time.Millisecond {
		a.mu.Unlock()
		return out, domain.ErrResourceBusy
	}
	a.active++
	a.last[key] = time.Now()
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.active--; a.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(v.Contract.TimeoutSeconds)*time.Second)
	defer cancel()
	switch v.Backend.Kind {
	case "postgres":
		out, err = readPostgres(ctx, v, q)
	case "s3":
		out, err = readS3(ctx, v, q)
	case "kafka":
		out, err = readKafka(ctx, v, q)
	case "loki":
		out, err = a.readLoki(ctx, v, q)
	case "prometheus":
		out, err = a.readPrometheus(ctx, v, q)
	case "mcp":
		out, err = readMCP(ctx, v, q)
	}
	if err != nil {
		return domain.SourceEnvelope{}, errors.New("native source read or coverage validation failed")
	}
	out.AdapterSHA256 = v.Backend.Digest()
	raw, _ := json.Marshal(out)
	if int64(len(raw)) > v.Contract.MaxBytes {
		return domain.SourceEnvelope{}, domain.ErrBudgetExhausted
	}
	return sources.ValidateEnvelope(v.Contract, q, out)
}

func (a *Adapter) Handler(tokenFile string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/query" {
			http.NotFound(w, r)
			return
		}
		token, err := mcpclient.ReadToken(tokenFile)
		if err != nil || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
		if err != nil || len(raw) > 16384 {
			http.Error(w, "query exceeds bound", 400)
			return
		}
		var q domain.SourceQuery
		if execution.DecodeProposal(raw, &q) != nil {
			http.Error(w, "invalid query", 400)
			return
		}
		out, err := a.Query(r.Context(), q)
		if err != nil {
			http.Error(w, "source read denied, unavailable or incomplete", 422)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
}

func Load(path string) (Config, error) {
	var cfg Config
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if len(raw) > 1024*1024 {
		return cfg, domain.ErrBudgetExhausted
	}
	err = execution.DecodeProposal(raw, &cfg)
	if err == nil && (cfg.Schema != "hybrid-ai/source-adapter/v1" || cfg.Address == "" || cfg.TokenFile == "") {
		err = errors.New("invalid native source server configuration")
	}
	return cfg, err
}

func envelope(v View, q domain.SourceQuery) domain.SourceEnvelope {
	return domain.SourceEnvelope{SchemaVersion: v.Contract.SchemaVersion, AdapterSHA256: v.Backend.Digest(), Start: q.Start, End: q.End, Watermark: time.Unix(0, 0).UTC(), Rows: []map[string]json.RawMessage{}, Complete: true}
}
func projectRow(q domain.SourceQuery, row map[string]json.RawMessage) (map[string]json.RawMessage, bool, error) {
	var when time.Time
	if json.Unmarshal(row["event_time"], &when) != nil {
		return nil, false, errors.New("native event timestamp missing")
	}
	if when.Before(q.Start) || !when.Before(q.End) {
		return nil, false, nil
	}
	for field, expected := range q.Filters {
		var actual string
		if json.Unmarshal(row[field], &actual) != nil {
			return nil, false, errors.New("native filter field missing")
		}
		if actual != expected {
			return nil, false, nil
		}
	}
	out := map[string]json.RawMessage{}
	for _, field := range q.Fields {
		value, ok := row[field]
		if !ok {
			return nil, false, errors.New("native field missing")
		}
		out[field] = value
	}
	return out, true, nil
}
func appendRow(out *domain.SourceEnvelope, q domain.SourceQuery, row map[string]json.RawMessage) error {
	projected, match, err := projectRow(q, row)
	if err != nil {
		return err
	}
	if match {
		if len(out.Rows) >= q.Limit {
			out.Complete = false
		} else {
			out.Rows = append(out.Rows, projected)
		}
	}
	return nil
}
func jsonValue(value any) json.RawMessage        { raw, _ := json.Marshal(value); return raw }
func selected(fields []string, name string) bool { return slices.Contains(fields, name) }
