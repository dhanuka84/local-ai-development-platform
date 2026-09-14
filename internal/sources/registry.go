// Package sources implements the bounded HTTP protocol for operator-configured
// data source adapters. It never constructs native SQL or changes event offsets.
package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type Registry struct {
	entries map[string]domain.SourceDescriptor
	client  *http.Client
}

func New(entries []domain.SourceDescriptor) (*Registry, error) {
	r := &Registry{entries: map[string]domain.SourceDescriptor{}, client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("source redirects are forbidden") }}}
	for _, d := range entries {
		if !domain.ValidProductKey(d.ID) || !domain.ValidProductKey(d.ProjectID) || !domain.ValidProductKey(d.ProductID) || d.Owner == "" || len(d.Roles) == 0 || len(d.Purposes) == 0 || len(d.Fields) < 2 || len(d.Fields) > 32 || d.Fields["event_id"] != "string" || d.Fields["event_time"] != "timestamp" || d.MaxRows < 1 || d.MaxRows > 1000 || d.MaxBytes < 256 || d.MaxBytes > 2*1024*1024 || d.TimeoutSeconds < 1 || d.TimeoutSeconds > 30 || d.MaxWindowSeconds < 1 || d.MaxWindowSeconds > 31*86400 || d.RetentionSeconds < 60 || d.RetentionSeconds > 365*86400 || d.SchemaVersion == "" {
			return nil, fmt.Errorf("invalid source contract %q", d.ID)
		}
		if !slices.Contains([]string{"brs", "code", "log", "metric", "event", "lake", "audit", "incident"}, d.Kind) {
			return nil, fmt.Errorf("invalid source kind")
		}
		if !slices.Contains([]string{"public", "internal", "confidential", "restricted"}, d.Classification) {
			return nil, fmt.Errorf("invalid source classification")
		}
		for field, kind := range d.Fields {
			if !domain.ValidProductKey(field) || !slices.Contains([]string{"string", "number", "boolean", "timestamp"}, kind) {
				return nil, fmt.Errorf("invalid source field")
			}
		}
		for _, field := range d.Filters {
			if d.Fields[field] != "string" {
				return nil, fmt.Errorf("unknown source filter")
			}
		}
		for role, fields := range d.FieldsByRole {
			if !slices.Contains(d.Roles, role) || !slices.Contains(fields, "event_id") || !slices.Contains(fields, "event_time") {
				return nil, fmt.Errorf("invalid role field scope")
			}
			for _, field := range fields {
				if _, ok := d.Fields[field]; !ok {
					return nil, fmt.Errorf("unknown role field")
				}
			}
		}
		u, err := url.Parse(d.Endpoint)
		if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
			return nil, fmt.Errorf("invalid fixed source endpoint")
		}
		if u.Scheme != "https" {
			ip := net.ParseIP(u.Hostname())
			if u.Scheme != "http" || !d.AllowLoopbackHTTP || ip == nil || !ip.IsLoopback() {
				return nil, fmt.Errorf("source requires HTTPS or explicit loopback HTTP")
			}
		}
		key := d.ProjectID + ":" + d.ID
		if _, ok := r.entries[key]; ok {
			return nil, fmt.Errorf("duplicate source")
		}
		r.entries[key] = d
	}
	return r, nil
}

func Load(path string) (*Registry, error) {
	if path == "" {
		return New(nil)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > 256*1024 {
		return nil, fmt.Errorf("source registry too large")
	}
	var entries []domain.SourceDescriptor
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&entries); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("source registry has trailing content")
	}
	return New(entries)
}

func (r *Registry) Get(project, id string) (domain.SourceDescriptor, bool) {
	d, ok := r.entries[project+":"+id]
	return d, ok
}
func (r *Registry) List(project string) []domain.SourceDescriptor {
	out := []domain.SourceDescriptor{}
	for _, d := range r.entries {
		if d.ProjectID == project {
			d.Endpoint = ""
			d.TokenFile = ""
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b domain.SourceDescriptor) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func ValidateQuery(d domain.SourceDescriptor, q domain.SourceQuery) error {
	if q.ProjectID != d.ProjectID || q.ProductID != d.ProductID || q.SourceID != d.ID || !slices.Contains(d.Purposes, q.Purpose) || !domain.ValidProductKey(q.IdempotencyKey) || q.Start.IsZero() || !q.End.After(q.Start) || q.End.Sub(q.Start) > time.Duration(d.MaxWindowSeconds)*time.Second || q.End.After(time.Now().Add(time.Minute)) || q.Limit < 1 || q.Limit > d.MaxRows || len(q.Fields) < 2 || len(q.Fields) > len(d.Fields) || !slices.Contains(q.Fields, "event_id") || !slices.Contains(q.Fields, "event_time") {
		return domain.ErrForbidden
	}
	seen := map[string]bool{}
	for _, field := range q.Fields {
		if _, ok := d.Fields[field]; !ok || seen[field] {
			return domain.ErrForbidden
		}
		seen[field] = true
	}
	for field, value := range q.Filters {
		if !slices.Contains(d.Filters, field) || len(value) > 256 {
			return domain.ErrForbidden
		}
	}
	return nil
}

func (r *Registry) Query(ctx context.Context, d domain.SourceDescriptor, q domain.SourceQuery) (out domain.SourceEnvelope, err error) {
	if err = ValidateQuery(d, q); err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.TimeoutSeconds)*time.Second)
	defer cancel()
	raw, _ := json.Marshal(q)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.Endpoint, bytes.NewReader(raw))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	if d.TokenFile != "" {
		info, e := os.Stat(d.TokenFile)
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 8192 {
			return out, errors.New("source credential requires a private regular file")
		}
		token, e := os.ReadFile(d.TokenFile)
		if e != nil || len(token) > 8192 || strings.TrimSpace(string(token)) == "" {
			return out, errors.New("source credential unavailable")
		}
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	}
	response, err := r.client.Do(req)
	if err != nil {
		return out, errors.New("source request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return out, errors.New("source returned unsuccessful status")
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, d.MaxBytes+1))
	if err != nil {
		return out, errors.New("source body unavailable")
	}
	if int64(len(payload)) > d.MaxBytes {
		return out, errors.New("source byte limit exceeded")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil || decoder.Decode(new(any)) != io.EOF {
		return domain.SourceEnvelope{}, errors.New("source schema invalid")
	}
	return ValidateEnvelope(d, q, out)
}

// ValidateEnvelope is shared by native adapters and the receiving gateway.
// Native protocol success alone does not establish typed or complete evidence.
func ValidateEnvelope(d domain.SourceDescriptor, q domain.SourceQuery, out domain.SourceEnvelope) (domain.SourceEnvelope, error) {
	if d.AdapterSHA256 != "" && out.AdapterSHA256 != d.AdapterSHA256 {
		return domain.SourceEnvelope{}, errors.New("native adapter contract changed")
	}
	if out.SchemaVersion != d.SchemaVersion || out.Revision == "" || len(out.Revision) > 256 || !out.Start.Equal(q.Start) || !out.End.Equal(q.End) || out.Watermark.IsZero() || out.Watermark.After(time.Now().Add(time.Minute)) || len(out.Rows) > q.Limit {
		return domain.SourceEnvelope{}, errors.New("source coverage contract invalid")
	}
	if d.Kind == "event" && len(out.Offsets) == 0 {
		return domain.SourceEnvelope{}, errors.New("event offsets missing")
	}
	if d.Kind == "lake" && out.Snapshot == "" {
		return domain.SourceEnvelope{}, errors.New("lake snapshot missing")
	}
	if len(out.Offsets) > 32 || len(out.Snapshot) > 256 {
		return domain.SourceEnvelope{}, errors.New("source lineage exceeds bounds")
	}
	for partition, offset := range out.Offsets {
		if !domain.ValidProductKey(partition) || offset < 0 {
			return domain.SourceEnvelope{}, errors.New("invalid event offset")
		}
	}
	seen := map[string]bool{}
	for _, row := range out.Rows {
		if len(row) != len(q.Fields) {
			return domain.SourceEnvelope{}, errors.New("source returned unexpected fields")
		}
		for _, field := range q.Fields {
			value, ok := row[field]
			if !ok {
				return domain.SourceEnvelope{}, errors.New("source field missing")
			}
			if string(value) == "null" {
				return domain.SourceEnvelope{}, errors.New("source field null; missing data must remain explicit")
			}
			switch d.Fields[field] {
			case "string":
				var v string
				if json.Unmarshal(value, &v) != nil || len(v) > 4096 {
					return domain.SourceEnvelope{}, errors.New("invalid string field")
				}
			case "timestamp":
				var v time.Time
				if json.Unmarshal(value, &v) != nil || v.IsZero() {
					return domain.SourceEnvelope{}, errors.New("invalid timestamp")
				}
			case "number":
				var v float64
				if json.Unmarshal(value, &v) != nil || math.IsNaN(v) || math.IsInf(v, 0) {
					return domain.SourceEnvelope{}, errors.New("invalid number")
				}
			case "boolean":
				var v bool
				if json.Unmarshal(value, &v) != nil {
					return domain.SourceEnvelope{}, errors.New("invalid boolean")
				}
			}
		}
		var id string
		var eventTime time.Time
		_ = json.Unmarshal(row["event_id"], &id)
		_ = json.Unmarshal(row["event_time"], &eventTime)
		if id == "" || seen[id] || eventTime.Before(q.Start) || !eventTime.Before(q.End) {
			return domain.SourceEnvelope{}, errors.New("duplicate or out-of-window source event")
		}
		seen[id] = true
		for field, expected := range q.Filters {
			var actual string
			if json.Unmarshal(row[field], &actual) != nil || actual != expected {
				return domain.SourceEnvelope{}, errors.New("source filter did not reconcile")
			}
		}
	}
	if out.Watermark.Before(q.End) {
		out.Complete = false
	}
	return out, nil
}
