// SPDX-License-Identifier: MPL-2.0

package scipanalyzer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/components/codegraph"
)

type Config struct {
	Name     string
	Version  string
	Command  string
	Language string
}

type Analyzer struct {
	config Config
}

func New(config Config) (*Analyzer, error) {
	config.Name = strings.TrimSpace(config.Name)
	config.Version = strings.TrimSpace(config.Version)
	config.Command = strings.TrimSpace(config.Command)
	config.Language = normalizeLanguage(config.Language)
	if config.Name == "" || config.Version == "" || config.Command == "" || config.Language == "" {
		return nil, errors.New("SCIP analyzer name, version, command, and language are required")
	}
	return &Analyzer{config: config}, nil
}

func (a *Analyzer) Name() string    { return a.config.Name }
func (a *Analyzer) Version() string { return a.config.Version }

func (a *Analyzer) Analyze(ctx context.Context, request codegraph.Request) (codegraph.Snapshot, error) {
	started := time.Now().UTC()
	root, err := filepath.Abs(strings.TrimSpace(request.RepositoryPath))
	if err != nil {
		return codegraph.Snapshot{}, fmt.Errorf("resolve repository path: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return codegraph.Snapshot{}, fmt.Errorf("stat repository path: %w", err)
	}
	if !info.IsDir() {
		return codegraph.Snapshot{}, errors.New("repository path must be a directory")
	}
	revision, branch, dirty, err := codegraph.ResolveRepositoryState(ctx, root, request.Revision, request.Branch)
	if err != nil {
		return codegraph.Snapshot{}, err
	}
	if dirty && !request.AllowDirty {
		return codegraph.Snapshot{}, errors.New("repository has uncommitted changes; commit them or explicitly allow a dirty analysis")
	}
	repositoryName := strings.TrimSpace(request.RepositoryName)
	if repositoryName == "" {
		repositoryName = filepath.Base(root)
	}

	temporary, err := os.MkdirTemp("", "hybrid-codegraph-")
	if err != nil {
		return codegraph.Snapshot{}, fmt.Errorf("create analyzer work directory: %w", err)
	}
	defer os.RemoveAll(temporary)
	staged := filepath.Join(temporary, "repository")
	output := filepath.Join(temporary, "indexes")
	if err := copyRepository(root, staged); err != nil {
		return codegraph.Snapshot{}, err
	}
	if err := os.MkdirAll(output, 0o700); err != nil {
		return codegraph.Snapshot{}, fmt.Errorf("create SCIP output directory: %w", err)
	}
	cacheRoot := strings.TrimSpace(os.Getenv("CODEGRAPH_ANALYZER_CACHE_ROOT"))
	if cacheRoot == "" {
		cacheRoot = filepath.Join(os.TempDir(), "hybrid-analyzer-cache")
	}
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		return codegraph.Snapshot{}, fmt.Errorf("create analyzer dependency cache: %w", err)
	}
	command := exec.CommandContext(ctx, a.config.Command, staged, output)
	command.Env = append(os.Environ(),
		"CODEGRAPH_PROJECT_NAME="+repositoryName,
		"HOME="+filepath.Join(temporary, "home"),
		"XDG_CACHE_HOME="+filepath.Join(cacheRoot, "xdg"),
		"NPM_CONFIG_CACHE="+filepath.Join(cacheRoot, "npm"),
		"GRADLE_USER_HOME="+filepath.Join(cacheRoot, "gradle"),
		"MAVEN_OPTS=-Dmaven.repo.local="+filepath.Join(cacheRoot, "maven"),
	)
	var commandOutput cappedBuffer
	commandOutput.limit = 64 * 1024
	command.Stdout, command.Stderr = &commandOutput, &commandOutput
	if err := command.Run(); err != nil {
		return codegraph.Snapshot{}, fmt.Errorf("run %s analyzer: %w: %s", a.Name(), err, strings.TrimSpace(commandOutput.String()))
	}
	indexFiles, err := sortedIndexFiles(output)
	if err != nil {
		return codegraph.Snapshot{}, fmt.Errorf("list SCIP indexes: %w", err)
	}
	if len(indexFiles) == 0 {
		return codegraph.Snapshot{}, fmt.Errorf("%s analyzer produced no SCIP indexes: %s", a.Name(), strings.TrimSpace(commandOutput.String()))
	}
	entities, relations, statistics, err := importIndexes(importRequest{
		RepositoryPath: root, StagedRepositoryPath: staged,
		RepositoryName: repositoryName, Branch: branch, Revision: revision,
		IndexFiles: indexFiles, Language: a.config.Language,
		MaxFiles: request.MaxFiles, MaxEntities: request.MaxEntities, MaxRelations: request.MaxRelations,
	})
	if err != nil {
		return codegraph.Snapshot{}, err
	}
	if dirty {
		revision += "+worktree." + codegraph.WorktreeFingerprint(entities)
	}
	for index := range entities {
		if entities[index].Kind == codegraph.EntityRepository {
			entities[index].Metadata["revision"] = revision
		}
	}
	snapshot := codegraph.Snapshot{
		RepositoryPath: root, RepositoryName: repositoryName, Branch: branch, Revision: revision,
		Analyzer: a.Name(), AnalyzerVersion: a.Version(),
		StartedAt: started, CompletedAt: time.Now().UTC(), Entities: entities, Relations: relations, Statistics: statistics,
	}
	if err := snapshot.Normalize(); err != nil {
		return codegraph.Snapshot{}, fmt.Errorf("normalize SCIP code graph: %w", err)
	}
	return snapshot, nil
}

func copyRepository(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return os.MkdirAll(target, 0o700)
		}
		if entry.IsDir() && ignoredDirectory(entry.Name()) {
			return filepath.SkipDir
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o600)
		if fileInfo, statErr := entry.Info(); statErr == nil && fileInfo.Mode()&0o111 != 0 {
			mode = 0o700
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		closeOutputErr := output.Close()
		closeInputErr := input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOutputErr != nil {
			return closeOutputErr
		}
		return closeInputErr
	})
}

func ignoredDirectory(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".idea", ".gradle", ".venv", "venv", "node_modules",
		"target", "build", "dist", "coverage", "vendor", "__pycache__", ".pytest_cache":
		return true
	default:
		return false
	}
}

type cappedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(value []byte) (int, error) {
	written := len(value)
	remaining := b.limit - b.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = b.Buffer.Write(value)
	}
	return written, nil
}
