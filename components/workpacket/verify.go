package workpacket

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMaxPatchBytes = 2 << 20
	maxCheckOutputBytes  = 32 << 10
)

// VerifyPatch applies a patch to a disposable local clone, checks file and
// size boundaries, and executes only the exact argv checks named by the packet.
// It does not modify the source checkout. OS/container isolation and egress
// denial remain deployment responsibilities.
func VerifyPatch(ctx context.Context, packet Packet, patch []byte) VerificationResult {
	evaluation := Evaluate(packet)
	result := VerificationResult{Evaluation: evaluation}
	if !evaluation.Allowed {
		result.Errors = append(result.Errors, "work packet policy rejected execution")
		return result
	}
	if strings.ToLower(strings.TrimSpace(packet.Mode)) != ModePatch {
		result.Errors = append(result.Errors, "verify requires mode=patch")
		return result
	}
	maxPatchBytes := packet.Limits.MaxPatchBytes
	if maxPatchBytes == 0 {
		maxPatchBytes = defaultMaxPatchBytes
	}
	if len(patch) == 0 {
		result.Errors = append(result.Errors, "patch is empty")
		return result
	}
	if len(patch) > maxPatchBytes {
		result.Errors = append(result.Errors, fmt.Sprintf("patch has %d bytes; limit is %d", len(patch), maxPatchBytes))
		return result
	}

	workspace, err := resolveWorkspace(packet.Workspace)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	baseRevision, err := gitOutput(ctx, workspace, "rev-parse", "--verify", strings.TrimSpace(packet.BaseRevision)+"^{commit}")
	if err != nil {
		result.Errors = append(result.Errors, "resolve base revision: "+err.Error())
		return result
	}
	baseRevision = strings.TrimSpace(baseRevision)
	result.BaseRevision = baseRevision

	temporaryRoot, err := os.MkdirTemp("", "hybrid-workpacket-")
	if err != nil {
		result.Errors = append(result.Errors, "create verification directory: "+err.Error())
		return result
	}
	defer os.RemoveAll(temporaryRoot)
	clonePath := filepath.Join(temporaryRoot, "repository")
	if output, err := commandOutput(ctx, "", safeEnvironment(), "git", "clone", "--quiet", "--no-local", "--no-checkout", "--", workspace, clonePath); err != nil {
		result.Errors = append(result.Errors, "clone repository: "+commandError(err, output))
		return result
	}
	if output, err := commandOutput(ctx, clonePath, safeEnvironment(), "git", "checkout", "--quiet", "--detach", baseRevision); err != nil {
		result.Errors = append(result.Errors, "checkout base revision: "+commandError(err, output))
		return result
	}
	patchPath := filepath.Join(temporaryRoot, "candidate.patch")
	if err := os.WriteFile(patchPath, patch, 0o600); err != nil {
		result.Errors = append(result.Errors, "write temporary patch: "+err.Error())
		return result
	}
	if output, err := commandOutput(ctx, clonePath, safeEnvironment(), "git", "apply", "--check", "--whitespace=error-all", "--", patchPath); err != nil {
		result.Errors = append(result.Errors, "patch does not apply cleanly: "+commandError(err, output))
		return result
	}
	if output, err := commandOutput(ctx, clonePath, safeEnvironment(), "git", "apply", "--index", "--whitespace=error-all", "--", patchPath); err != nil {
		result.Errors = append(result.Errors, "apply patch: "+commandError(err, output))
		return result
	}
	changes, total, err := stagedChanges(ctx, clonePath)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	result.ChangedFiles, result.TotalDiffLines = changes, total
	if len(changes) == 0 {
		result.Errors = append(result.Errors, "patch produces no tracked changes")
	}
	if len(changes) > packet.Limits.MaxChangedFiles {
		result.Errors = append(result.Errors, fmt.Sprintf("patch changes %d files; limit is %d", len(changes), packet.Limits.MaxChangedFiles))
	}
	if total > packet.Limits.MaxDiffLines {
		result.Errors = append(result.Errors, fmt.Sprintf("patch changes %d lines; limit is %d", total, packet.Limits.MaxDiffLines))
	}
	for _, change := range changes {
		if !allowedPath(change.Path, packet.AllowedFiles, evaluation.EffectiveForbiddenFiles) {
			result.Errors = append(result.Errors, fmt.Sprintf("changed file %q is outside the work-packet scope", change.Path))
		}
	}
	if output, err := commandOutput(ctx, clonePath, safeEnvironment(), "git", "diff", "--cached", "--check"); err != nil {
		result.Errors = append(result.Errors, "git diff --check failed: "+commandError(err, output))
	}
	if len(result.Errors) > 0 {
		return result
	}

	beforeHash, err := stagedPatchHash(ctx, clonePath)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	for _, check := range packet.Checks {
		checkResult := runCheck(ctx, clonePath, check)
		result.Checks = append(result.Checks, checkResult)
		if !checkResult.Passed {
			result.Errors = append(result.Errors, fmt.Sprintf("check %q failed", check.Name))
			return result
		}
	}
	if output, err := commandOutput(ctx, clonePath, safeEnvironment(), "git", "diff", "--quiet"); err != nil {
		result.Errors = append(result.Errors, "a verification command modified a tracked file outside the staged patch: "+commandError(err, output))
		return result
	}
	if untracked, err := gitOutput(ctx, clonePath, "ls-files", "--others", "--exclude-standard"); err != nil {
		result.Errors = append(result.Errors, "inspect untracked files: "+err.Error())
		return result
	} else if strings.TrimSpace(untracked) != "" {
		result.Errors = append(result.Errors, "a verification command created untracked files: "+strings.Join(strings.Fields(untracked), ", "))
		return result
	}
	afterHash, err := stagedPatchHash(ctx, clonePath)
	if err != nil {
		result.Errors = append(result.Errors, err.Error())
		return result
	}
	if beforeHash != afterHash {
		result.Errors = append(result.Errors, "a verification command changed the staged patch")
		return result
	}
	result.Accepted = true
	return result
}

func resolveWorkspace(value string) (string, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve workspace symlinks: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("workspace is not a directory")
	}
	if _, err := gitOutput(context.Background(), resolved, "rev-parse", "--show-toplevel"); err != nil {
		return "", fmt.Errorf("workspace is not a Git repository: %w", err)
	}
	return resolved, nil
}

func stagedChanges(ctx context.Context, repository string) ([]FileChange, int, error) {
	output, err := commandBytes(ctx, repository, safeEnvironment(), "git", "diff", "--cached", "--numstat", "--no-renames", "-z")
	if err != nil {
		return nil, 0, fmt.Errorf("inspect staged patch: %w", err)
	}
	records := bytes.Split(output, []byte{0})
	changes := make([]FileChange, 0, len(records))
	total := 0
	for _, record := range records {
		if len(record) == 0 {
			continue
		}
		parts := bytes.SplitN(record, []byte{'\t'}, 3)
		if len(parts) != 3 {
			return nil, 0, fmt.Errorf("unexpected git numstat record")
		}
		if string(parts[0]) == "-" || string(parts[1]) == "-" {
			return nil, 0, fmt.Errorf("binary patches are not permitted")
		}
		additions, addErr := strconv.Atoi(string(parts[0]))
		deletions, deleteErr := strconv.Atoi(string(parts[1]))
		if addErr != nil || deleteErr != nil {
			return nil, 0, fmt.Errorf("invalid git numstat counts")
		}
		name := filepath.ToSlash(string(parts[2]))
		changes = append(changes, FileChange{Path: name, Additions: additions, Deletions: deletions})
		total += additions + deletions
	}
	return changes, total, nil
}

func stagedPatchHash(ctx context.Context, repository string) (string, error) {
	patch, err := commandBytes(ctx, repository, safeEnvironment(), "git", "diff", "--cached", "--binary", "--no-ext-diff")
	if err != nil {
		return "", fmt.Errorf("hash staged patch: %w", err)
	}
	digest := sha256.Sum256(patch)
	return hex.EncodeToString(digest[:]), nil
}

func runCheck(parent context.Context, repository string, check Check) CheckResult {
	timeout := check.TimeoutSeconds
	if timeout == 0 {
		timeout = 300
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeout)*time.Second)
	defer cancel()
	output, err := commandOutput(ctx, repository, safeEnvironment(), check.Argv[0], check.Argv[1:]...)
	result := CheckResult{Name: check.Name, Argv: append([]string{}, check.Argv...), ExitCode: 0, Passed: err == nil, Output: output}
	if err != nil {
		result.ExitCode = -1
		if exitError, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitError.ExitCode()
		}
		if ctx.Err() == context.DeadlineExceeded {
			result.Output = strings.TrimSpace(result.Output + "\ncheck timed out")
		}
	}
	return result
}

func gitOutput(ctx context.Context, repository string, args ...string) (string, error) {
	output, err := commandOutput(ctx, repository, safeEnvironment(), "git", args...)
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), commandError(err, output))
	}
	return output, nil
}

func commandBytes(ctx context.Context, directory string, environment []string, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	command.Env = environment
	return command.Output()
}

func commandOutput(ctx context.Context, directory string, environment []string, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	command.Env = environment
	buffer := &limitedBuffer{limit: maxCheckOutputBytes}
	command.Stdout = buffer
	command.Stderr = buffer
	err := command.Run()
	return strings.TrimSpace(buffer.String()), err
}

func commandError(err error, output string) string {
	if strings.TrimSpace(output) == "" {
		return err.Error()
	}
	return err.Error() + ": " + strings.TrimSpace(output)
}

func safeEnvironment() []string {
	allowed := []string{"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL", "GOCACHE", "GOMODCACHE", "GOPATH"}
	environment := make([]string, 0, len(allowed)+3)
	for _, key := range allowed {
		if value, found := os.LookupEnv(key); found {
			environment = append(environment, key+"="+value)
		}
	}
	return append(environment, "CI=1", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = b.buffer.Write(value)
	}
	return original, nil
}

func (b *limitedBuffer) String() string {
	value := b.buffer.String()
	if b.buffer.Len() == b.limit {
		value += "\n[output truncated]"
	}
	return value
}
