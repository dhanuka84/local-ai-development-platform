// SPDX-License-Identifier: MPL-2.0

package codegraph

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// ResolveRepositoryState returns the exact Git branch, HEAD, and whether the
// worktree differs from it. Expected values make analysis fail closed when a
// checkout moves between task planning and indexing.
func ResolveRepositoryState(ctx context.Context, root, expectedRevision, expectedBranch string) (string, string, bool, error) {
	command := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "HEAD")
	output, err := command.CombinedOutput()
	if err != nil {
		return "", "", false, fmt.Errorf("resolve Git revision: %w: %s", err, strings.TrimSpace(string(output)))
	}
	revision := strings.TrimSpace(string(output))
	if expectedRevision = strings.TrimSpace(expectedRevision); expectedRevision != "" && expectedRevision != revision {
		return "", "", false, fmt.Errorf("repository is at revision %s, expected %s", revision, expectedRevision)
	}
	branchCommand := exec.CommandContext(ctx, "git", "-C", root, "symbolic-ref", "--quiet", "--short", "HEAD")
	branchOutput, branchErr := branchCommand.CombinedOutput()
	branch := strings.TrimSpace(string(branchOutput))
	if branchErr != nil {
		branch = "detached"
	}
	if expectedBranch = strings.TrimSpace(expectedBranch); expectedBranch != "" && expectedBranch != branch {
		return "", "", false, fmt.Errorf("repository is on branch %s, expected %s", branch, expectedBranch)
	}
	status := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain=v1", "--untracked-files=normal")
	statusOutput, err := status.CombinedOutput()
	if err != nil {
		return "", "", false, fmt.Errorf("inspect Git worktree: %w: %s", err, strings.TrimSpace(string(statusOutput)))
	}
	return revision, branch, len(statusOutput) > 0, nil
}

// WorktreeFingerprint creates a stable suffix for an explicitly permitted
// dirty analysis. File entities carry whole-file hashes, so the suffix changes
// whenever analyzed source changes.
func WorktreeFingerprint(entities []Entity) string {
	ordered := append([]Entity(nil), entities...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Key < ordered[j].Key })
	parts := make([]string, 0, len(ordered))
	for _, entity := range ordered {
		parts = append(parts, entity.Key+":"+entity.ContentHash)
	}
	hash := HashContent([]byte(strings.Join(parts, "\n")))
	return hash[:12]
}
