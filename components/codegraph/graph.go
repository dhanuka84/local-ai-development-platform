// SPDX-License-Identifier: MPL-2.0

// Package codegraph defines the language-neutral graph emitted by source
// analyzers. PostgreSQL remains the authoritative persistence layer; these
// values are an interchange contract, not an in-memory database.
package codegraph

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type EntityKind string

const (
	EntityRepository EntityKind = "repository"
	EntityModule     EntityKind = "module"
	EntityPackage    EntityKind = "package"
	EntityFile       EntityKind = "file"
	EntityType       EntityKind = "type"
	EntityInterface  EntityKind = "interface"
	EntityFunction   EntityKind = "function"
	EntityMethod     EntityKind = "method"
	EntityField      EntityKind = "field"
	EntityVariable   EntityKind = "variable"
	EntityConstant   EntityKind = "constant"
	EntityTest       EntityKind = "test"
)

type RelationKind string

const (
	RelationContains   RelationKind = "contains"
	RelationDefines    RelationKind = "defines"
	RelationImports    RelationKind = "imports"
	RelationCalls      RelationKind = "calls"
	RelationReferences RelationKind = "references"
	RelationImplements RelationKind = "implements"
	RelationEmbeds     RelationKind = "embeds"
	RelationTests      RelationKind = "tests"
)

type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

type Location struct {
	FilePath string   `json:"file_path,omitempty"`
	Start    Position `json:"start"`
	End      Position `json:"end"`
}

type Entity struct {
	Key           string            `json:"key"`
	Language      string            `json:"language"`
	Kind          EntityKind        `json:"kind"`
	Name          string            `json:"name"`
	QualifiedName string            `json:"qualified_name"`
	Signature     string            `json:"signature,omitempty"`
	ContentHash   string            `json:"content_hash,omitempty"`
	Location      Location          `json:"location"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type Relation struct {
	SourceKey  string            `json:"source_key"`
	TargetKey  string            `json:"target_key"`
	Kind       RelationKind      `json:"kind"`
	Evidence   string            `json:"evidence,omitempty"`
	Confidence float32           `json:"confidence"`
	Location   Location          `json:"location"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type Snapshot struct {
	RepositoryPath  string           `json:"repository_path"`
	RepositoryName  string           `json:"repository_name"`
	Branch          string           `json:"branch"`
	Revision        string           `json:"revision"`
	Analyzer        string           `json:"analyzer"`
	AnalyzerVersion string           `json:"analyzer_version"`
	StartedAt       time.Time        `json:"started_at"`
	CompletedAt     time.Time        `json:"completed_at"`
	Entities        []Entity         `json:"entities"`
	Relations       []Relation       `json:"relations"`
	Statistics      map[string]int64 `json:"statistics,omitempty"`
}

func StableKey(language string, kind EntityKind, qualifiedName string) string {
	return strings.ToLower(strings.TrimSpace(language)) + ":" + string(kind) + ":" + strings.TrimSpace(qualifiedName)
}

func HashContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func (s *Snapshot) Normalize() error {
	if strings.TrimSpace(s.RepositoryPath) == "" {
		return errors.New("repository path is required")
	}
	if strings.TrimSpace(s.RepositoryName) == "" || strings.TrimSpace(s.Branch) == "" {
		return errors.New("repository name and branch are required")
	}
	if strings.TrimSpace(s.Revision) == "" {
		return errors.New("repository revision is required")
	}
	if strings.TrimSpace(s.Analyzer) == "" || strings.TrimSpace(s.AnalyzerVersion) == "" {
		return errors.New("analyzer name and version are required")
	}

	entities := make(map[string]Entity, len(s.Entities))
	for _, entity := range s.Entities {
		if err := validateEntity(entity); err != nil {
			return err
		}
		if _, exists := entities[entity.Key]; exists {
			return fmt.Errorf("duplicate entity key %q", entity.Key)
		}
		entities[entity.Key] = entity
	}

	relations := make(map[string]Relation, len(s.Relations))
	for _, relation := range s.Relations {
		if _, ok := entities[relation.SourceKey]; !ok {
			return fmt.Errorf("relation source %q is not an entity", relation.SourceKey)
		}
		if _, ok := entities[relation.TargetKey]; !ok {
			return fmt.Errorf("relation target %q is not an entity", relation.TargetKey)
		}
		if relation.Kind == "" {
			return errors.New("relation kind is required")
		}
		if relation.Confidence == 0 {
			relation.Confidence = 1
		}
		if relation.Confidence < 0 || relation.Confidence > 1 {
			return fmt.Errorf("relation confidence must be between 0 and 1")
		}
		key := relationKey(relation)
		if _, exists := relations[key]; !exists {
			relations[key] = relation
		}
	}

	s.Entities = s.Entities[:0]
	for _, entity := range entities {
		s.Entities = append(s.Entities, entity)
	}
	sort.Slice(s.Entities, func(i, j int) bool { return s.Entities[i].Key < s.Entities[j].Key })

	s.Relations = s.Relations[:0]
	for _, relation := range relations {
		s.Relations = append(s.Relations, relation)
	}
	sort.Slice(s.Relations, func(i, j int) bool {
		return relationKey(s.Relations[i]) < relationKey(s.Relations[j])
	})
	if s.Statistics == nil {
		s.Statistics = map[string]int64{}
	}
	s.Statistics["entities"] = int64(len(s.Entities))
	s.Statistics["relations"] = int64(len(s.Relations))
	return nil
}

func validateEntity(entity Entity) error {
	if strings.TrimSpace(entity.Key) == "" || entity.Kind == "" || strings.TrimSpace(entity.QualifiedName) == "" {
		return errors.New("entity key, kind, and qualified name are required")
	}
	if entity.Location.Start.Line < 0 || entity.Location.Start.Column < 0 ||
		entity.Location.End.Line < 0 || entity.Location.End.Column < 0 {
		return fmt.Errorf("entity %q has a negative source position", entity.Key)
	}
	return nil
}

func relationKey(relation Relation) string {
	return relation.SourceKey + "\x00" + string(relation.Kind) + "\x00" + relation.TargetKey + "\x00" +
		relation.Location.FilePath + "\x00" + fmt.Sprint(relation.Location.Start.Line, ":", relation.Location.Start.Column)
}
