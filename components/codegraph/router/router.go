// SPDX-License-Identifier: MPL-2.0

// Package router detects supported source languages and combines analyzer
// snapshots for mixed-language repositories.
package router

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/components/codegraph"
)

type Candidate struct {
	Analyzer   codegraph.Analyzer
	Extensions []string
	Markers    []string
}

type Router struct {
	candidates []Candidate
}

func New(candidates ...Candidate) (*Router, error) {
	if len(candidates) == 0 {
		return nil, errors.New("at least one code analyzer candidate is required")
	}
	for index := range candidates {
		if candidates[index].Analyzer == nil {
			return nil, errors.New("code analyzer candidate is required")
		}
		if len(candidates[index].Extensions) == 0 {
			return nil, fmt.Errorf("analyzer %q has no source extensions", candidates[index].Analyzer.Name())
		}
		for extensionIndex := range candidates[index].Extensions {
			candidates[index].Extensions[extensionIndex] = strings.ToLower(candidates[index].Extensions[extensionIndex])
		}
		for markerIndex := range candidates[index].Markers {
			candidates[index].Markers[markerIndex] = strings.ToLower(candidates[index].Markers[markerIndex])
		}
	}
	return &Router{candidates: candidates}, nil
}

func (r *Router) Name() string    { return "multi-language" }
func (r *Router) Version() string { return "1" }

func (r *Router) Analyze(ctx context.Context, request codegraph.Request) (codegraph.Snapshot, error) {
	matched, err := r.detect(request.RepositoryPath)
	if err != nil {
		return codegraph.Snapshot{}, err
	}
	if len(matched) == 0 {
		return codegraph.Snapshot{}, errors.New("repository has no supported Go, Java, Kotlin, TypeScript, JavaScript, or Python source files")
	}
	if len(matched) == 1 {
		return matched[0].Analyze(ctx, request)
	}

	revision, branch, dirty, err := codegraph.ResolveRepositoryState(ctx, request.RepositoryPath, request.Revision, request.Branch)
	if err != nil {
		return codegraph.Snapshot{}, err
	}
	if dirty && !request.AllowDirty {
		return codegraph.Snapshot{}, errors.New("repository has uncommitted changes; commit them or explicitly allow a dirty analysis")
	}

	started := time.Now().UTC()
	partials := make([]codegraph.Snapshot, 0, len(matched))
	for _, analyzer := range matched {
		childRequest := request
		childRequest.Revision = revision
		childRequest.Branch = branch
		childRequest.AllowDirty = true
		partial, err := analyzer.Analyze(ctx, childRequest)
		if err != nil {
			return codegraph.Snapshot{}, fmt.Errorf("%s analyzer: %w", analyzer.Name(), err)
		}
		partials = append(partials, partial)
	}
	return merge(request.RepositoryPath, request.RepositoryName, branch, revision, dirty, started, partials)
}

func (r *Router) detect(root string) ([]codegraph.Analyzer, error) {
	extensions := make(map[string]struct{})
	markers := make(map[string]struct{})
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && ignoredDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() {
			extensions[strings.ToLower(filepath.Ext(entry.Name()))] = struct{}{}
			markers[strings.ToLower(entry.Name())] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("detect repository languages: %w", err)
	}
	matched := make([]codegraph.Analyzer, 0, len(r.candidates))
	for _, candidate := range r.candidates {
		if !hasMarker(candidate.Markers, markers) {
			continue
		}
		for _, extension := range candidate.Extensions {
			if _, ok := extensions[extension]; ok {
				matched = append(matched, candidate.Analyzer)
				break
			}
		}
	}
	return matched, nil
}

func hasMarker(required []string, found map[string]struct{}) bool {
	if len(required) == 0 {
		return true
	}
	for _, marker := range required {
		if _, ok := found[marker]; ok {
			return true
		}
	}
	return false
}

func merge(root, repositoryName, branch, revision string, dirty bool, started time.Time, partials []codegraph.Snapshot) (codegraph.Snapshot, error) {
	entities := make(map[string]codegraph.Entity)
	relations := make([]codegraph.Relation, 0)
	analyzers := make([]string, 0, len(partials))
	statistics := map[string]int64{"analyzers": int64(len(partials))}
	repositoryKey := codegraph.StableKey("multi", codegraph.EntityRepository, ".")
	if repositoryName = strings.TrimSpace(repositoryName); repositoryName == "" {
		repositoryName = filepath.Base(root)
	}
	entities[repositoryKey] = codegraph.Entity{
		Key: repositoryKey, Language: "multi", Kind: codegraph.EntityRepository,
		Name: repositoryName, QualifiedName: ".", Metadata: map[string]string{
			"path": root, "repository": repositoryName, "branch": branch, "revision": revision,
		},
	}
	for _, partial := range partials {
		analyzers = append(analyzers, partial.Analyzer+"@"+partial.AnalyzerVersion)
		for key, value := range partial.Statistics {
			statistics[partial.Analyzer+"."+key] += value
		}
		for _, entity := range partial.Entities {
			if existing, ok := entities[entity.Key]; ok && existing.ContentHash != entity.ContentHash {
				return codegraph.Snapshot{}, fmt.Errorf("analyzers emitted conflicting entity %q", entity.Key)
			}
			entities[entity.Key] = entity
			if entity.Kind == codegraph.EntityRepository && entity.Key != repositoryKey {
				relations = append(relations, codegraph.Relation{
					SourceKey: repositoryKey, TargetKey: entity.Key, Kind: codegraph.RelationContains,
					Evidence: "language analyzer root", Confidence: 1,
				})
			}
		}
		relations = append(relations, partial.Relations...)
	}
	orderedEntities := make([]codegraph.Entity, 0, len(entities))
	for _, entity := range entities {
		orderedEntities = append(orderedEntities, entity)
	}
	if dirty {
		revision += "+worktree." + codegraph.WorktreeFingerprint(orderedEntities)
	}
	for index := range orderedEntities {
		if orderedEntities[index].Kind == codegraph.EntityRepository {
			if orderedEntities[index].Metadata == nil {
				orderedEntities[index].Metadata = map[string]string{}
			}
			orderedEntities[index].Metadata["repository"] = repositoryName
			orderedEntities[index].Metadata["branch"] = branch
			orderedEntities[index].Metadata["revision"] = revision
		}
	}
	sort.Strings(analyzers)
	snapshot := codegraph.Snapshot{
		RepositoryPath: root, RepositoryName: repositoryName, Branch: branch, Revision: revision,
		Analyzer:        "multi-language",
		AnalyzerVersion: strings.Join(analyzers, "+"), StartedAt: started, CompletedAt: time.Now().UTC(),
		Entities: orderedEntities, Relations: relations, Statistics: statistics,
	}
	if err := snapshot.Normalize(); err != nil {
		return codegraph.Snapshot{}, fmt.Errorf("normalize combined code graph: %w", err)
	}
	return snapshot, nil
}

func ignoredDirectory(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".idea", ".gradle", ".mvn-cache", ".venv", "venv",
		"node_modules", "target", "build", "dist", "coverage", "vendor":
		return true
	default:
		return false
	}
}
