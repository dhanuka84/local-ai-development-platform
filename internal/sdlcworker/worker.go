package sdlcworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/components/workpacket"
	"github.com/dhanuka84/hybrid-ai-platform/internal/agentmodel"
	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
)

type Gateway interface {
	Call(context.Context, string, any, any) error
}
type Config struct {
	ProjectID      string                      `json:"project_id"`
	MCPURL         string                      `json:"mcp_url"`
	TokenFile      string                      `json:"token_file"`
	Role           string                      `json:"role"`
	OllamaURL      string                      `json:"ollama_url"`
	Workspaces     map[string]string           `json:"workspaces"`
	SpoolDirectory string                      `json:"spool_directory"`
	TrialKind      string                      `json:"trial_kind"`
	Docker         string                      `json:"docker"`
	Delivery       execution.DeliveryConfig    `json:"delivery"`
	Remediation    execution.RemediationConfig `json:"remediation"`
}
type Worker struct {
	Config  Config
	Gateway Gateway
	Model   *agentmodel.Client
	Sandbox Sandbox
}

func New(cfg Config, gateway Gateway) (*Worker, error) {
	if !domain.ValidProductKey(cfg.ProjectID) || cfg.SpoolDirectory == "" || gateway == nil || !slices.Contains([]string{"sdlc_builder", "sdlc_evaluator", "sdlc_delivery", "sdlc_diagnosis", "sdlc_remediation"}, cfg.Role) || (cfg.TrialKind != "fixture" && cfg.TrialKind != "local_model") {
		return nil, errors.New("worker requires a scoped project, role, evidence directory and declared trial kind")
	}
	w := &Worker{Config: cfg, Gateway: gateway, Sandbox: Sandbox{Docker: cfg.Docker}}
	if cfg.Role == "sdlc_builder" || cfg.Role == "sdlc_diagnosis" {
		model, err := agentmodel.New(cfg.OllamaURL)
		if err != nil {
			return nil, err
		}
		w.Model = model
	}
	return w, nil
}

func (w *Worker) RunOne(ctx context.Context, id string) (domain.ExecutionRun, error) {
	input := service.ExecutionIDInput{ProjectID: w.Config.ProjectID, RunID: id}
	var view domain.ExecutionView
	if err := w.Gateway.Call(ctx, "sdlc_run_get", input, &view); err != nil {
		return domain.ExecutionRun{}, err
	}
	if domain.ExecutionRole(view.Run.Stage) != w.Config.Role {
		return view.Run, errors.New("this worker does not own the next stage")
	}
	var claim domain.ExecutionClaim
	if err := w.Gateway.Call(ctx, "sdlc_step_claim", input, &claim); err != nil {
		return view.Run, err
	}
	lease := domain.ExecutionLease{ProjectID: claim.Run.ProjectID, RunID: claim.Run.ID, StepID: claim.Step.ID, Fence: claim.Step.Fence, Token: claim.Token}
	ctx, cancel := context.WithDeadline(ctx, claim.Step.LeaseUntil.Add(-time.Second))
	defer cancel()
	var contextData service.ExecutionContext
	if err := w.Gateway.Call(ctx, "sdlc_step_context", lease, &contextData); err != nil {
		return claim.Run, err
	}
	var result domain.ExecutionResult
	var err error
	switch claim.Run.Stage {
	case "build":
		result, err = w.build(ctx, claim, lease, contextData)
	case "evaluate":
		result, err = w.evaluate(ctx, claim, lease, contextData)
	case "deliver", "verify_delivery":
		result, err = w.delivery(ctx, claim, lease, contextData)
	case "diagnose":
		result, err = w.diagnose(ctx, claim, lease, contextData)
	case "remediate", "verify_recovery":
		result, err = w.remediate(ctx, claim, lease, contextData)
	default:
		err = errors.New("the assigned action adapter is not configured")
	}
	if err != nil {
		result.Stage = claim.Run.Stage
		result.Outcome = "blocked"
		result.Summary = err.Error()
		result.Criteria = nil
	}
	var out domain.ExecutionRun
	if completeErr := w.Gateway.Call(ctx, "sdlc_step_complete", service.ExecutionCompleteInput{ExecutionLease: lease, Result: result}, &out); completeErr != nil {
		return claim.Run, completeErr
	}
	return out, nil
}

func (w *Worker) put(ctx context.Context, l domain.ExecutionLease, raw []byte, media string) (out domain.Artifact, err error) {
	if len(raw) == 0 {
		raw = []byte("No output was produced.")
	}
	// Retain exact evidence locally before transport. A lost lease or gateway
	// failure leaves an immutable receipt available for accountable recovery.
	store := artifacts.NewLocalStore(w.Config.SpoolDirectory)
	local, err := store.Put(context.WithoutCancel(ctx), raw, media)
	if err != nil {
		return out, err
	}
	if err = os.Chmod(strings.TrimPrefix(local.URI, "file://"), 0400); err != nil {
		return out, err
	}
	err = w.Gateway.Call(ctx, "sdlc_artifact_put", struct {
		domain.ExecutionLease
		Content   string `json:"content"`
		MediaType string `json:"media_type"`
	}{l, string(raw), media}, &out)
	return out, err
}
func (w *Worker) get(ctx context.Context, run domain.ExecutionRun, sha string) ([]byte, error) {
	var out struct {
		Content string `json:"content"`
	}
	err := w.Gateway.Call(ctx, "sdlc_artifact_get", struct {
		service.ExecutionIDInput
		SHA256 string `json:"sha256"`
	}{service.ExecutionIDInput{ProjectID: run.ProjectID, RunID: run.ID}, sha}, &out)
	if err != nil {
		return nil, err
	}
	if domain.Digest([]byte(out.Content)) != sha {
		return nil, domain.ErrEvidenceUnavailable
	}
	return []byte(out.Content), nil
}

func packetFromContext(c service.ExecutionContext) (workpacket.Packet, string, error) {
	var p workpacket.Packet
	sha := ""
	for _, criterion := range c.Intent.Specification.Criteria {
		if criterion.Oracle != "work_packet" || sha != "" && sha != criterion.WorkPacketSHA256 {
			return p, "", domain.ErrValidationRequired
		}
		sha = criterion.WorkPacketSHA256
	}
	for _, record := range c.Intent.Records {
		if record.Kind == "test" && domain.Digest([]byte(record.Content)) == sha {
			err := json.Unmarshal([]byte(record.Content), &p)
			return p, sha, err
		}
	}
	return p, "", domain.ErrEvidenceUnavailable
}

func (w *Worker) build(ctx context.Context, claim domain.ExecutionClaim, l domain.ExecutionLease, c service.ExecutionContext) (out domain.ExecutionResult, err error) {
	out.Stage = "build"
	packet, _, err := packetFromContext(c)
	if err != nil {
		return out, err
	}
	workspace := w.Config.Workspaces[claim.Run.Target.RepositoryID]
	if workspace == "" {
		return out, errors.New("accepted repository has no operator-configured local workspace")
	}
	files, err := builderFiles(ctx, workspace, packet)
	if err != nil {
		return out, err
	}
	responseContract := execution.BuilderProposal{Schema: "hybrid-ai/builder-proposal/v1", BaseRevision: packet.BaseRevision, Criteria: criterionIDs(c), Plan: []domain.ExecutionPlanStep{{Description: "Describe the bounded implementation", Criteria: criterionIDs(c)}}, Files: map[string]string{"allowed/repository/path": "Complete replacement UTF-8 file content; include only files permitted by the accepted packet. The runtime constructs the patch."}}
	raw, _ := json.Marshal(struct {
		Schema           string                    `json:"schema"`
		Context          service.ExecutionContext  `json:"context"`
		Files            map[string]string         `json:"files"`
		ResponseContract execution.BuilderProposal `json:"response_contract"`
	}{"hybrid-ai/builder-request/v1", c, files, responseContract})
	contextRaw, _ := json.Marshal(c)
	manifest := execution.DisclosureManifest{Schema: "hybrid-ai/disclosed-context/v1", RunID: claim.Run.ID, StepID: claim.Step.ID, ContextSHA256: domain.Digest(contextRaw), PackageSHA256: claim.Run.Package().Digest(), RepositoryID: claim.Run.Target.RepositoryID, RepositoryRevision: packet.BaseRevision, Files: map[string]string{}, Observations: []domain.ProductBinding{}, Provider: "ollama", Model: claim.Run.Package().Model, ModelSHA256: claim.Run.Package().ModelSHA256}
	for path, content := range files {
		manifest.Files[path] = domain.Digest([]byte(content))
	}
	generated, err := w.Model.GenerateForExecution(ctx, claim.Run.Package(), string(raw))
	modelEvidence, evidenceErr := w.modelEvidence(ctx, claim, l, manifest, generated)
	if evidenceErr != nil {
		return out, evidenceErr
	}
	out.Model = &modelEvidence
	if err != nil {
		return out, err
	}
	var proposal execution.BuilderProposal
	if err = execution.DecodeProposal([]byte(generated.Output.Response), &proposal); err != nil {
		return out, errors.New("builder output is not a valid typed proposal")
	}
	if len(proposal.Files) > 0 {
		if proposal.Patch != "" {
			return out, domain.ErrValidationRequired
		}
		proposal.Patch, err = execution.FilePatch(files, proposal.Files)
		if err != nil {
			return out, err
		}
	}
	if proposal.Schema != "hybrid-ai/builder-proposal/v1" || proposal.BaseRevision != packet.BaseRevision || len(proposal.Patch) > packet.Limits.MaxPatchBytes {
		return out, domain.ErrValidationRequired
	}
	out.Patch, err = w.put(ctx, l, []byte(proposal.Patch), "text/x-diff")
	if err != nil {
		return out, err
	}
	out.Outcome = "succeeded"
	out.Summary = "Local builder proposed a criteria-linked patch for independent evaluation."
	out.Plan = proposal.Plan
	out.Criteria = proposal.Criteria
	out.BaseRevision = proposal.BaseRevision
	return out, nil
}

func (w *Worker) modelEvidence(ctx context.Context, claim domain.ExecutionClaim, l domain.ExecutionLease, manifest execution.DisclosureManifest, generated agentmodel.Result) (out domain.ModelEvidence, err error) {
	p := claim.Run.Package()
	out = domain.ModelEvidence{Provider: "ollama", Model: p.Model, ModelSHA256: p.ModelSHA256, InputTokens: generated.Output.PromptEvalCount, OutputTokens: generated.Output.EvalCount, DurationMillis: generated.Duration.Milliseconds(), TrialKind: w.Config.TrialKind}
	if out.Prompt, err = w.put(ctx, l, generated.Request, "application/json"); err != nil {
		return out, err
	}
	if out.Response, err = w.put(ctx, l, generated.Response, "application/json"); err != nil {
		return out, err
	}
	raw, _ := json.Marshal(manifest)
	out.Manifest, err = w.put(ctx, l, raw, "application/json")
	return out, err
}

func criterionIDs(c service.ExecutionContext) []string {
	ids := []string{}
	for _, criterion := range c.Intent.Specification.Criteria {
		ids = append(ids, criterion.ID)
	}
	return ids
}

func builderFiles(ctx context.Context, workspace string, p workpacket.Packet) (map[string]string, error) {
	if !revisionPattern.MatchString(p.BaseRevision) {
		return nil, domain.ErrValidationRequired
	}
	paths, err := git(ctx, workspace, "ls-tree", "-r", "--name-only", "-z", p.BaseRevision)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	total := 0
	for _, name := range strings.Split(string(paths), "\x00") {
		if name == "" {
			continue
		}
		allowed := false
		for _, pattern := range p.AllowedFiles {
			match, _ := filepath.Match(pattern, name)
			allowed = allowed || match || name == pattern
		}
		for _, pattern := range workpacket.Evaluate(p).EffectiveForbiddenFiles {
			match, _ := filepath.Match(pattern, name)
			if match || name == pattern || strings.HasPrefix(name, strings.TrimSuffix(pattern, "/")+"/") {
				allowed = false
			}
		}
		if !allowed {
			continue
		}
		data, err := git(ctx, workspace, "show", "--no-textconv", p.BaseRevision+":"+name)
		if err != nil {
			return nil, err
		}
		if bytesContainNUL(data) {
			return nil, errors.New("binary source is outside the local model disclosure contract")
		}
		total += len(data)
		if total > 64*1024 || len(out) >= 30 {
			return nil, domain.ErrBudgetExhausted
		}
		out[name] = string(data)
	}
	return out, nil
}
func bytesContainNUL(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

func (w *Worker) evaluate(ctx context.Context, claim domain.ExecutionClaim, l domain.ExecutionLease, c service.ExecutionContext) (out domain.ExecutionResult, err error) {
	out.Stage = "evaluate"
	packet, sha, err := packetFromContext(c)
	if err != nil {
		return out, err
	}
	var candidate *domain.ExecutionResult
	for i := len(c.PriorSteps) - 1; i >= 0; i-- {
		if c.PriorSteps[i].Stage == "build" && c.PriorSteps[i].Result != nil {
			candidate = c.PriorSteps[i].Result
			break
		}
	}
	if candidate == nil {
		return out, domain.ErrEvidenceUnavailable
	}
	out.CandidateSHA = candidate.Patch.SHA256
	out.PacketSHA256 = sha
	out.BaseRevision = packet.BaseRevision
	out.SandboxImage = claim.Run.Package().EvaluatorImage
	patch, err := w.get(ctx, claim.Run, candidate.Patch.SHA256)
	if err != nil {
		return out, err
	}
	workspace := w.Config.Workspaces[claim.Run.Target.RepositoryID]
	if workspace == "" {
		return out, errors.New("evaluator repository is not configured")
	}
	sandbox, verifyErr := w.Sandbox.Verify(ctx, claim, workspace, packet, patch)
	if len(sandbox.Raw) > 0 {
		receipt, e := w.put(ctx, l, sandbox.Raw, "application/json")
		if e != nil {
			return out, e
		}
		out.Artifacts = append(out.Artifacts, receipt)
	}
	if verifyErr != nil {
		return out, verifyErr
	}
	for _, check := range sandbox.Verification.Checks {
		raw, _ := json.Marshal(check.Argv)
		output, e := w.put(ctx, l, []byte(check.Output), "text/plain")
		if e != nil {
			return out, e
		}
		out.Checks = append(out.Checks, domain.ExecutionCheck{Name: check.Name, ArgvSHA256: domain.Digest(raw), ExitCode: check.ExitCode, Output: output})
	}
	out.Outcome = "failed"
	out.Summary = "Isolated protected verification rejected this candidate."
	if sandbox.Verification.Accepted {
		out.Outcome = "succeeded"
		out.Criteria = criterionIDs(c)
		out.Summary = "Independent isolated product checks passed against the exact accepted packet and candidate."
	}
	return out, nil
}

func (c Config) String() string {
	return fmt.Sprintf("SDLC worker role=%s project=%s", c.Role, c.ProjectID)
}
