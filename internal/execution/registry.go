// Package execution contains the operator-controlled execution contract. Model
// invocation and untrusted patch execution live in external workers, not MCP.
package execution

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type Registry struct {
	Schema   string                   `json:"schema"`
	Packages []domain.AgentPackage    `json:"packages"`
	Targets  []domain.ExecutionTarget `json:"targets"`
}

var shaPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func Load(filename string) (*Registry, error) {
	if filename == "" {
		return &Registry{}, nil
	}
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 1024*1024 {
		return nil, errors.New("execution registry exceeds 1 MiB")
	}
	var r Registry
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&r); err != nil {
		return nil, err
	}
	if !errors.Is(d.Decode(new(any)), io.EOF) {
		return nil, errors.New("one execution registry document required")
	}
	return &r, r.Validate()
}

func (r *Registry) Validate() error {
	if r.Schema != "hybrid-ai/sdlc-registry/v1" || len(r.Packages) > 100 || len(r.Targets) > 100 {
		return errors.New("invalid execution registry schema or size")
	}
	packages, targets := map[string]bool{}, map[string]bool{}
	for _, p := range r.Packages {
		if packages[p.ID] {
			return errors.New("duplicate package ID; register versions with distinct IDs")
		}
		packages[p.ID] = true
		if err := p.Validate(); err != nil {
			return fmt.Errorf("package %s: %w", p.ID, err)
		}
	}
	for _, t := range r.Targets {
		if !domain.ValidProductKey(t.ID) || !domain.ValidProductKey(t.ProjectID) || !domain.ValidProductKey(t.ProductID) || !domain.ValidProductKey(t.RepositoryID) || targets[t.ProjectID+":"+t.ID] || (t.Environment != "disposable" && t.Environment != "staging") || len(t.Classifications) == 0 || len(t.Sources) > 20 || len(t.Purposes) > 10 || len(t.ProtectedPaths) == 0 || len(t.AllowedRemedies) > 10 {
			return errors.New("invalid scoped execution target")
		}
		targets[t.ProjectID+":"+t.ID] = true
		if err := t.Budget.Validate(); err != nil {
			return err
		}
		if t.Qualification && t.Environment != "disposable" || t.ObservationSeconds < 0 || t.ObservationSeconds > 60 {
			return errors.New("qualification requires disposable targets and observation windows are bounded")
		}
		for _, digest := range []string{t.DeliverySHA256, t.RemediationSHA256} {
			if digest != "" && !shaPattern.MatchString(digest) {
				return errors.New("effect configuration requires an exact SHA256")
			}
		}
		for _, c := range t.Classifications {
			if c != "public" && c != "internal" {
				return errors.New("local execution target supports public and internal classified context")
			}
		}
		for _, p := range t.ProtectedPaths {
			if p == "." || path.IsAbs(p) || path.Clean(p) != p || p == ".." || strings.HasPrefix(p, "../") || strings.ContainsAny(p, "\\\x00\n") {
				return errors.New("protected paths must be clean repository-relative paths")
			}
		}
		actors := map[string]bool{}
		for _, role := range []string{"sdlc_builder", "sdlc_evaluator", "sdlc_delivery", "sdlc_diagnosis", "sdlc_remediation"} {
			actor, id := t.Participants[role], t.Packages[role]
			p, ok := r.Package(id)
			if !domain.ValidProductKey(actor) || actors[actor] || !ok || p.Role != role {
				return errors.New("each role requires a distinct assigned workload and matching package")
			}
			actors[actor] = true
			if p.Model != "" && p.MaxTokenCharge() > t.Budget.MaxTokens {
				return errors.New("target token budget cannot accommodate its model package")
			}
		}
		if len(t.Participants) != 5 || len(t.Packages) != 5 {
			return errors.New("unknown target participant role")
		}
		for _, id := range append(append([]string{}, t.Sources...), t.Purposes...) {
			if !domain.ValidProductKey(id) {
				return errors.New("invalid source or purpose")
			}
		}
	}
	return nil
}

func (r *Registry) Package(id string) (domain.AgentPackage, bool) {
	if r != nil {
		for _, p := range r.Packages {
			if p.ID == id {
				return p, true
			}
		}
	}
	return domain.AgentPackage{}, false
}
func (r *Registry) Target(project, id string) (domain.ExecutionTarget, map[string]domain.AgentPackage, error) {
	if r != nil {
		for _, t := range r.Targets {
			if t.ID == id && t.ProjectID == project {
				packages := map[string]domain.AgentPackage{}
				for role, id := range t.Packages {
					p, ok := r.Package(id)
					if !ok {
						return t, nil, domain.ErrQualityBlocked
					}
					packages[role] = p
				}
				return t, packages, nil
			}
		}
	}
	return domain.ExecutionTarget{}, nil, domain.ErrForbidden
}

// A changed operator configuration never silently changes the contract of an
// in-flight run. Its operator must restore that version or submit a new run.
func (r *Registry) Current(run domain.ExecutionRun) error {
	t, packages, err := r.Target(run.ProjectID, run.Target.ID)
	if err != nil {
		return err
	}
	if t.Digest() != run.Target.Digest() {
		return domain.ErrVersionConflict
	}
	for role, p := range packages {
		if p.Digest() != run.Packages[role].Digest() {
			return domain.ErrVersionConflict
		}
	}
	return nil
}

func HasTool(run domain.ExecutionRun, tool string) bool {
	return slices.Contains(run.Package().Tools, tool)
}
