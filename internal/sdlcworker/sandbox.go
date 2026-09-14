package sdlcworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/components/workpacket"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
)

var pinnedImage = regexp.MustCompile(`^(sha256:[a-f0-9]{64}|[a-zA-Z0-9./:_-]+@sha256:[a-f0-9]{64})$`)
var revisionPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

type Sandbox struct{ Docker string }
type SandboxResult struct {
	Verification workpacket.VerificationResult
	Raw          []byte
	ImageID      string
}

type boundedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - b.Len()
	if remaining < 0 {
		remaining = 0
	}
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

func command(ctx context.Context, dir string, input []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_PROTOCOL_FROM_USER=0", "PYTHONDONTWRITEBYTECODE=1"}
	cmd.Stdin = bytes.NewReader(input)
	out := &boundedBuffer{limit: 2 * 1024 * 1024}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	if out.overflow {
		return nil, errors.New("worker command output exceeded limit")
	}
	if err != nil {
		return out.Bytes(), fmt.Errorf("trusted worker command %s failed", filepath.Base(name))
	}
	return out.Bytes(), nil
}

func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return command(ctx, dir, nil, "git", append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "protocol.file.allow=always"}, args...)...)
}

// snapshot creates a shallow, credential-free checkout at exactly the accepted
// commit. The sandbox never receives the operator's original .git directory,
// worktree changes, hooks, credential helpers, environment or model transcript.
func snapshot(ctx context.Context, workspace, revision, destination string) error {
	if !revisionPattern.MatchString(revision) {
		return errors.New("an exact Git commit is required")
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(destination, 0755); err != nil {
		return err
	}
	if _, err = git(ctx, destination, "init", "--quiet"); err != nil {
		return err
	}
	if _, err = git(ctx, destination, "fetch", "--quiet", "--depth=1", "--no-tags", "--", abs, revision); err != nil {
		return err
	}
	if _, err = git(ctx, destination, "checkout", "--quiet", "--detach", revision); err != nil {
		return err
	}
	if err = os.Remove(filepath.Join(destination, ".git", "FETCH_HEAD")); err != nil && !os.IsNotExist(err) {
		return err
	}
	paths, err := git(ctx, destination, "ls-tree", "-r", "--name-only", "-z", revision)
	if err != nil {
		return err
	}
	for _, name := range strings.Split(string(paths), "\x00") {
		if name == "" {
			continue
		}
		base := strings.ToLower(filepath.Base(name))
		if base == ".env" || strings.HasPrefix(base, ".env.") && base != ".env.example" || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") || base == "credentials.json" {
			return errors.New("tracked credential material is outside the evaluator snapshot contract")
		}
	}
	return nil
}

func (s Sandbox) Verify(ctx context.Context, claim domain.ExecutionClaim, workspace string, packet workpacket.Packet, patch []byte) (out SandboxResult, err error) {
	pkg := claim.Run.Package()
	if s.Docker == "" {
		s.Docker = "docker"
	}
	if pkg.Role != "sdlc_evaluator" || !pinnedImage.MatchString(pkg.EvaluatorImage) || packet.CloudReview || !packet.LocalOnly || len(patch) > 2*1024*1024 {
		return out, errors.New("invalid isolated evaluation contract")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(pkg.TimeoutSeconds)*time.Second)
	defer cancel()
	image, err := command(ctx, "", nil, s.Docker, "image", "inspect", "--format", "{{.Id}}", pkg.EvaluatorImage)
	if err != nil {
		return out, errors.New("pinned evaluator image is unavailable; pulling during execution is forbidden")
	}
	out.ImageID = strings.TrimSpace(string(image))
	root, err := os.MkdirTemp("", "hybrid-sdlc-evaluator-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(root)
	if err = os.Chmod(root, 0755); err != nil {
		return out, err
	}
	repository := filepath.Join(root, "repository")
	if err = snapshot(ctx, workspace, packet.BaseRevision, repository); err != nil {
		return out, err
	}
	name := "hybrid-sdlc-eval-" + claim.Step.ID + fmt.Sprintf("-%d", claim.Step.Fence)
	defer func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_, _ = command(cleanupCtx, "", nil, s.Docker, "rm", "--force", name)
	}()
	raw, _ := json.Marshal(struct {
		Packet workpacket.Packet `json:"packet"`
		Patch  string            `json:"patch"`
	}{packet, string(patch)})
	args := []string{"run", "--rm", "--pull=never", "--name", name, "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges:true", "--pids-limit=128", "--memory=512m", "--memory-swap=512m", "--cpus=1", "--user=65532:65532", "--stop-timeout=1", "--tmpfs=/tmp:rw,exec,nosuid,nodev,size=256m,mode=1777", "--mount", "type=bind,src=" + repository + ",dst=/input/repository,readonly", "--env=PYTHONDONTWRITEBYTECODE=1", "--interactive", pkg.EvaluatorImage}
	out.Raw, err = command(ctx, "", raw, s.Docker, args...)
	if err != nil {
		return out, err
	}
	if err = execution.DecodeProposal(out.Raw, &out.Verification); err != nil {
		return out, errors.New("isolated verifier did not return one valid receipt")
	}
	if out.Verification.BaseRevision != "" && out.Verification.BaseRevision != packet.BaseRevision {
		return out, domain.ErrVersionConflict
	}
	return out, nil
}
