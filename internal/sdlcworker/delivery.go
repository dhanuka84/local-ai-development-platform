package sdlcworker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
	"github.com/dhanuka84/hybrid-ai-platform/internal/mcpclient"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
)

var repositorySlug = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)

type forgeClient struct {
	endpoint, repository, token string
	client                      *http.Client
}

func newForge(c execution.DeliveryConfig) (*forgeClient, error) {
	u, err := url.Parse(c.ForgeURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !repositorySlug.MatchString(c.Repository) || !domain.ValidProductKey(c.BaseBranch) {
		return nil, domain.ErrForbidden
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, domain.ErrForbidden
		}
	}
	token, err := mcpclient.ReadToken(c.TokenFile)
	if err != nil {
		return nil, err
	}
	return &forgeClient{endpoint: strings.TrimRight(c.ForgeURL, "/"), repository: c.Repository, token: token, client: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return domain.ErrForbidden }}}, nil
}
func (f *forgeClient) request(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, f.endpoint+"/api/v1/repos/"+f.repository+path, bytes.NewReader(raw))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "token "+f.token)
	req.Header.Set("Content-Type", "application/json")
	response, err := f.client.Do(req)
	if err != nil {
		return nil, 0, errors.New("forge request outcome unavailable; read-back required")
	}
	defer response.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
	if err != nil || len(raw) > 2*1024*1024 {
		return nil, response.StatusCode, domain.ErrBudgetExhausted
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return raw, response.StatusCode, fmt.Errorf("forge %s %s rejected with HTTP %d", method, path, response.StatusCode)
	}
	return raw, response.StatusCode, nil
}
func (f *forgeClient) branch(ctx context.Context, name string) (string, []byte, int, error) {
	raw, status, err := f.request(ctx, "GET", "/branches/"+url.PathEscape(name), nil)
	if err != nil {
		return "", raw, status, err
	}
	var branch struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if json.Unmarshal(raw, &branch) != nil || !revisionPattern.MatchString(branch.Commit.ID) {
		return "", raw, status, domain.ErrEvidenceUnavailable
	}
	return branch.Commit.ID, raw, status, nil
}
func (f *forgeClient) git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "http.followRedirects=false"}, args...)...)
	cmd.Dir = dir
	// Only the trusted process gets this header. It is never written to .git,
	// argv, model input, sandbox environment or diagnostic output.
	header := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("sdlc:"+f.token))
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=" + header}
	out := &boundedBuffer{limit: 2 * 1024 * 1024}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return nil, errors.New("forge Git operation failed; inspect branch read-back")
	}
	if out.overflow {
		return nil, domain.ErrBudgetExhausted
	}
	return out.Bytes(), nil
}

type releaseManifest struct {
	Schema    string `json:"schema"`
	RunID     string `json:"run_id"`
	ActionID  string `json:"action_id"`
	Base      string `json:"base"`
	Commit    string `json:"commit"`
	Candidate string `json:"candidate_sha256"`
	Archive   string `json:"archive_sha256"`
	Branch    string `json:"branch"`
	Pull      int    `json:"pull"`
}

func immutableFile(path string, raw []byte) error {
	if old, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(old, raw) {
			return domain.ErrVersionConflict
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0400)
	if err != nil {
		if os.IsExist(err) {
			old, e := os.ReadFile(path)
			if e == nil && bytes.Equal(old, raw) {
				return nil
			}
			return domain.ErrVersionConflict
		}
		return err
	}
	_, err = file.Write(raw)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func stageRelease(root string, manifest []byte, archive []byte, expected string) ([]byte, error) {
	if !filepath.IsAbs(root) {
		return nil, domain.ErrForbidden
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(root, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	sha := domain.Digest(manifest)
	release := filepath.Join(root, "releases", sha)
	if err = immutableFile(filepath.Join(release, "manifest.json"), manifest); err != nil {
		return nil, err
	}
	if err = immutableFile(filepath.Join(release, "source.tar"), archive); err != nil {
		return nil, err
	}
	current := filepath.Join(root, "current.json")
	old, err := os.ReadFile(current)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if !bytes.Equal(old, manifest) && ((len(old) == 0 && expected != "") || (len(old) > 0 && domain.Digest(old) != expected)) {
		return nil, errors.New("staging current release differs from the explicit replacement contract")
	}
	tmp, err := os.CreateTemp(root, ".current-")
	if err != nil {
		return nil, err
	}
	name := tmp.Name()
	defer os.Remove(name)
	_, err = tmp.Write(manifest)
	if err == nil {
		err = tmp.Sync()
	}
	_ = tmp.Close()
	if err != nil {
		return nil, err
	}
	if err = os.Rename(name, current); err != nil {
		return nil, err
	}
	return os.ReadFile(current)
}
func (w *Worker) effect(ctx context.Context, l domain.ExecutionLease, r execution.EffectReceipt, raw []byte) (domain.ExecutionEffect, error) {
	r.Schema = "hybrid-ai/effect-receipt/v1"
	r.RunID = l.RunID
	r.StepID = l.StepID
	r.Raw = string(raw)
	receipt, _ := json.Marshal(r)
	a, err := w.put(ctx, l, receipt, "application/json")
	return domain.ExecutionEffect{Kind: r.Kind, Key: l.StepID + ":" + r.Kind, ExternalID: r.ExternalID, BeforeSHA256: r.BeforeSHA256, AfterSHA256: r.AfterSHA256, ObservedSHA256: r.ObservedSHA256, Status: "verified", Evidence: a}, err
}
func latest(c service.ExecutionContext, stage string) *domain.ExecutionResult {
	for i := len(c.PriorSteps) - 1; i >= 0; i-- {
		if c.PriorSteps[i].Stage == stage && c.PriorSteps[i].Result != nil {
			return c.PriorSteps[i].Result
		}
	}
	return nil
}

func (w *Worker) delivery(ctx context.Context, claim domain.ExecutionClaim, l domain.ExecutionLease, c service.ExecutionContext) (out domain.ExecutionResult, err error) {
	out.Stage = claim.Run.Stage
	cfg := w.Config.Delivery
	if cfg.Digest() != claim.Run.Target.DeliverySHA256 || !filepath.IsAbs(cfg.ArtifactRoot) || !filepath.IsAbs(cfg.StagingRoot) {
		return out, errors.New("delivery route does not match the frozen operator contract")
	}
	forge, err := newForge(cfg)
	if err != nil {
		return out, err
	}
	packet, packetSHA, err := packetFromContext(c)
	if err != nil {
		return out, err
	}
	candidate := latest(c, "build")
	evaluation := latest(c, "evaluate")
	if candidate == nil || evaluation == nil || evaluation.Outcome != "succeeded" || evaluation.CandidateSHA != candidate.Patch.SHA256 {
		return out, domain.ErrValidationRequired
	}
	patch, err := w.get(ctx, claim.Run, candidate.Patch.SHA256)
	if err != nil {
		return out, err
	}
	out.CandidateSHA = candidate.Patch.SHA256
	out.BaseRevision = packet.BaseRevision
	out.PacketSHA256 = packetSHA
	// The action branch uses the logical step identity, never the lease fence.
	action := l.StepID
	if out.Stage == "verify_delivery" {
		for _, s := range c.PriorSteps {
			if s.Stage == "deliver" && s.Result != nil && s.Result.Outcome == "succeeded" {
				action = s.ID
			}
		}
	}
	branch := "sdlc/" + claim.Run.ID + "/" + action
	base, _, _, err := forge.branch(ctx, cfg.BaseBranch)
	if err != nil {
		return out, err
	}
	if base != packet.BaseRevision {
		return out, errors.New("forge base advanced beyond the accepted intent")
	}
	root, err := os.MkdirTemp("", "hybrid-sdlc-delivery-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(root)
	repository := filepath.Join(root, "repository")
	if err = snapshot(ctx, w.Config.Workspaces[claim.Run.Target.RepositoryID], packet.BaseRevision, repository); err != nil {
		return out, err
	}
	patchPath := filepath.Join(root, "candidate.patch")
	if err = os.WriteFile(patchPath, patch, 0600); err != nil {
		return out, err
	}
	if _, err = git(ctx, repository, "apply", "--index", "--whitespace=error-all", "--", patchPath); err != nil {
		return out, err
	}
	tree, err := git(ctx, repository, "write-tree")
	if err != nil {
		return out, err
	}
	cmd := exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "commit-tree", strings.TrimSpace(string(tree)), "-p", packet.BaseRevision, "-m", "SDLC "+claim.Run.ID+" "+action)
	cmd.Dir = repository
	stamp := claim.Run.CreatedAt.Format(time.RFC3339)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=SDLC delivery", "GIT_AUTHOR_EMAIL=sdlc@example.invalid", "GIT_COMMITTER_NAME=SDLC delivery", "GIT_COMMITTER_EMAIL=sdlc@example.invalid", "GIT_AUTHOR_DATE=" + stamp, "GIT_COMMITTER_DATE=" + stamp}
	commitRaw, err := cmd.Output()
	if err != nil {
		return out, errors.New("deterministic release commit failed")
	}
	commit := strings.TrimSpace(string(commitRaw))
	canWrite := out.Stage == "deliver" && claim.Run.Status != "reconciling"
	guard := func() error {
		var current domain.ExecutionView
		if e := w.Gateway.Call(ctx, "sdlc_run_get", service.ExecutionIDInput{ProjectID: l.ProjectID, RunID: l.RunID}, &current); e != nil {
			return e
		}
		if current.Run.Status != "running" {
			return domain.ErrLeaseLost
		}
		var disclosure service.ExecutionContext
		return w.Gateway.Call(ctx, "sdlc_step_context", l, &disclosure)
	}
	observed, branchRaw, status, readErr := forge.branch(ctx, branch)
	if readErr != nil && status == 404 && canWrite {
		if err = guard(); err != nil {
			return out, err
		}
		_, pushErr := forge.git(ctx, repository, "push", "--porcelain", "--force-with-lease=refs/heads/"+branch+":", "--", forge.endpoint+"/"+forge.repository+".git", commit+":refs/heads/"+branch)
		observed, branchRaw, _, readErr = forge.branch(ctx, branch)
		if readErr != nil && pushErr != nil {
			return out, pushErr
		}
	}
	if readErr != nil || observed != commit {
		return out, errors.New("release branch read-back does not match the exact candidate")
	}
	add := func(kind, id string, body []byte) error {
		sha := domain.Digest(body)
		effect, e := w.effect(ctx, l, execution.EffectReceipt{Kind: kind, ContractSHA256: cfg.Digest(), ExternalID: id, BeforeSHA256: domain.Digest([]byte(packet.BaseRevision)), AfterSHA256: sha, ObservedSHA256: sha, CandidateSHA256: candidate.Patch.SHA256, BaseRevision: packet.BaseRevision, Commit: commit}, body)
		if e == nil {
			out.Effects = append(out.Effects, effect)
		}
		return e
	}
	if err = add("forge_commit", commit, branchRaw); err != nil {
		return out, err
	}
	type pull struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Head   struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"base"`
	}
	pullsRaw, _, err := forge.request(ctx, "GET", "/pulls?state=open&limit=50", nil)
	if err != nil {
		return out, err
	}
	var pulls []pull
	if json.Unmarshal(pullsRaw, &pulls) != nil || len(pulls) >= 50 {
		return out, domain.ErrBudgetExhausted
	}
	var selected pull
	for _, p := range pulls {
		if p.Head.Ref == branch {
			if selected.Number != 0 {
				return out, domain.ErrVersionConflict
			}
			selected = p
		}
	}
	if selected.Number == 0 && canWrite {
		if err = guard(); err != nil {
			return out, err
		}
		_, _, createErr := forge.request(ctx, "POST", "/pulls", map[string]any{"base": cfg.BaseBranch, "head": branch, "title": "SDLC " + claim.Run.ID, "body": "Accepted intent " + claim.Run.Intent.RecordID + "; independently evaluated candidate " + candidate.Patch.SHA256})
		pullsRaw, _, err = forge.request(ctx, "GET", "/pulls?state=open&limit=50", nil)
		if err != nil {
			return out, err
		}
		if json.Unmarshal(pullsRaw, &pulls) != nil {
			return out, domain.ErrEvidenceUnavailable
		}
		for _, p := range pulls {
			if p.Head.Ref == branch {
				selected = p
			}
		}
		if selected.Number == 0 {
			return out, createErr
		}
	}
	if selected.Number == 0 || selected.Head.SHA != commit || selected.Base.SHA != base || selected.State != "open" {
		return out, errors.New("PR read-back does not match the accepted release")
	}
	prRaw, _ := json.Marshal(selected)
	if err = add("forge_pr", fmt.Sprint(selected.Number), prRaw); err != nil {
		return out, err
	}
	archive, err := git(ctx, repository, "archive", "--format=tar", commit)
	if err != nil {
		return out, err
	}
	manifest := releaseManifest{Schema: "hybrid-ai/release/v1", RunID: claim.Run.ID, ActionID: action, Base: base, Commit: commit, Candidate: candidate.Patch.SHA256, Archive: domain.Digest(archive), Branch: branch, Pull: selected.Number}
	manifestRaw, _ := json.Marshal(manifest)
	artifactPath := filepath.Join(cfg.ArtifactRoot, manifest.Archive+".tar")
	manifestPath := filepath.Join(cfg.ArtifactRoot, domain.Digest(manifestRaw)+".json")
	if canWrite {
		if err = guard(); err != nil {
			return out, err
		}
		if err = immutableFile(artifactPath, archive); err != nil {
			return out, err
		}
		if err = immutableFile(manifestPath, manifestRaw); err != nil {
			return out, err
		}
	}
	actual, err := os.ReadFile(artifactPath)
	if err != nil || !bytes.Equal(actual, archive) {
		return out, domain.ErrEvidenceUnavailable
	}
	actualManifest, err := os.ReadFile(manifestPath)
	if err != nil || !bytes.Equal(actualManifest, manifestRaw) {
		return out, domain.ErrEvidenceUnavailable
	}
	if err = add("artifact", manifest.Archive, manifestRaw); err != nil {
		return out, err
	}
	if canWrite {
		if err = guard(); err != nil {
			return out, err
		}
		if _, err = stageRelease(cfg.StagingRoot, manifestRaw, archive, cfg.ExpectedReleaseSHA256); err != nil {
			return out, err
		}
	}
	staged, err := os.ReadFile(filepath.Join(cfg.StagingRoot, "current.json"))
	if err != nil || !bytes.Equal(staged, manifestRaw) {
		return out, domain.ErrVersionConflict
	}
	stagedArchive, err := os.ReadFile(filepath.Join(cfg.StagingRoot, "releases", domain.Digest(staged), "source.tar"))
	if err != nil || !bytes.Equal(stagedArchive, archive) {
		return out, domain.ErrEvidenceUnavailable
	}
	if err = add("staging", domain.Digest(staged), staged); err != nil {
		return out, err
	}
	if out.Stage == "verify_delivery" {
		// Read the published commit through native Git, then independently rerun
		// protected behavior tests. A publisher's earlier report cannot satisfy CI.
		if _, err = forge.git(ctx, repository, "fetch", "--quiet", "--no-tags", "--", forge.endpoint+"/"+forge.repository+".git", "refs/heads/"+branch); err != nil {
			return out, err
		}
		delivered, err := git(ctx, repository, "rev-parse", "FETCH_HEAD")
		if err != nil || strings.TrimSpace(string(delivered)) != commit {
			return out, domain.ErrVersionConflict
		}
		fetchedArchive, err := git(ctx, repository, "archive", "--format=tar", "FETCH_HEAD")
		if err != nil || !bytes.Equal(fetchedArchive, stagedArchive) {
			return out, domain.ErrEvidenceUnavailable
		}
		sandbox, err := w.Sandbox.Verify(ctx, claim, repository, packet, patch)
		if err != nil {
			return out, err
		}
		a, err := w.put(ctx, l, sandbox.Raw, "application/json")
		if err != nil {
			return out, err
		}
		out.Artifacts = append(out.Artifacts, a)
		out.SandboxImage = claim.Run.Package().EvaluatorImage
		if !sandbox.Verification.Accepted {
			out.Outcome = "failed"
			out.Summary = "Delivered release failed independent protected behavior checks."
			return out, nil
		}
		for _, check := range sandbox.Verification.Checks {
			argv, _ := json.Marshal(check.Argv)
			a, e := w.put(ctx, l, []byte(check.Output), "text/plain")
			if e != nil {
				return out, e
			}
			out.Checks = append(out.Checks, domain.ExecutionCheck{Name: check.Name, ArgvSHA256: domain.Digest(argv), ExitCode: check.ExitCode, Output: a})
		}
		if err = guard(); err != nil {
			return out, err
		}
		ciContext := "sdlc/" + claim.Run.ID
		statuses, err := forge.ensureCI(ctx, commit, ciContext)
		if err != nil {
			return out, err
		}
		if err = add("ci", commit, statuses); err != nil {
			return out, err
		}
		if err = add("behavior_probe", commit, sandbox.Raw); err != nil {
			return out, err
		}
	}
	out.Outcome = "succeeded"
	out.Criteria = criterionIDs(c)
	out.Summary = "Exact release commit, PR, artifact and staging read-back verified."
	return out, nil
}

func (f *forgeClient) ensureCI(ctx context.Context, commit, contextName string) ([]byte, error) {
	read := func() ([]byte, bool, error) {
		raw, _, err := f.request(ctx, "GET", "/commits/"+commit+"/status", nil)
		if err != nil {
			return nil, false, err
		}
		var ci struct {
			Statuses []struct {
				Context string `json:"context"`
				Status  string `json:"status"`
			} `json:"statuses"`
		}
		if json.Unmarshal(raw, &ci) != nil {
			return nil, false, domain.ErrEvidenceUnavailable
		}
		for _, s := range ci.Statuses {
			if s.Context == contextName {
				return raw, s.Status == "success", nil
			}
		}
		return raw, false, nil
	}
	raw, found, err := read()
	if err != nil || found {
		return raw, err
	}
	_, _, writeErr := f.request(ctx, "POST", "/statuses/"+commit, map[string]string{"state": "success", "context": contextName, "description": "Independent protected product checks passed"})
	raw, found, err = read()
	if err != nil || !found {
		if writeErr != nil {
			return nil, writeErr
		}
		return nil, domain.ErrEvidenceUnavailable
	}
	return raw, nil
}
