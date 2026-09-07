package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/dhanuka84/hybrid-ai-platform/components/workpacket"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
)

// Repair is explicit and narrow: a local model may correct hunk counts only.
// It cannot replace source lines, revise the lesson, skip checks, or approve it.
type patchRepair struct {
	ExpectedTaskVersion int    `json:"expected_task_version"`
	KnowledgeID         string `json:"knowledge_id"`
	ExpectedVersion     int    `json:"expected_version"`
	ModelOutputSHA256   string `json:"model_output_sha256"`
	VerifierSHA256      string `json:"verifier_result_sha256"`
}

type patchAnswer struct {
	Patch   string `json:"patch"`
	Summary string `json:"summary"`
	Lesson  string `json:"lesson"`
}

func (r *runner) repairTaskAPatch(ctx context.Context, task domain.WorkflowTaskCheckpoint) (domain.KnowledgeValidation, error) {
	repair := r.spec.RepairTaskA
	if repair == nil || task.State != domain.TaskStateValidationRequired || task.Version != repair.ExpectedTaskVersion || task.CandidateID != repair.KnowledgeID {
		return domain.KnowledgeValidation{}, domain.ErrVersionConflict
	}
	item, err := r.app.Service.Get(ctx, task.CandidateID, true)
	if err != nil {
		return domain.KnowledgeValidation{}, err
	}
	if item.Status != domain.CandidatePending || item.Version != repair.ExpectedVersion || item.ProjectID != task.ProjectID || item.WorkflowID != task.WorkflowID {
		return domain.KnowledgeValidation{}, domain.ErrVersionConflict
	}
	trace, err := r.app.Service.WorkflowTrace(ctx, task.WorkflowID)
	if err != nil {
		return domain.KnowledgeValidation{}, err
	}
	if !taskArtifact(trace, task.ID, "model.generate.output", repair.ModelOutputSHA256) || !taskArtifact(trace, task.ID, "local.verifier.result", repair.VerifierSHA256) {
		return domain.KnowledgeValidation{}, domain.ErrEvidenceUnavailable
	}
	raw, err := r.store.Read(ctx, repair.ModelOutputSHA256)
	if err != nil {
		return domain.KnowledgeValidation{}, err
	}
	var envelope struct {
		Model    string `json:"model"`
		Done     bool   `json:"done"`
		Response string `json:"response"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil || !envelope.Done || envelope.Model != r.spec.Model {
		return domain.KnowledgeValidation{}, domain.ErrEvidenceUnavailable
	}
	var previous patchAnswer
	if err = json.Unmarshal([]byte(envelope.Response), &previous); err != nil || previous.Patch == "" || strings.TrimSpace(previous.Lesson) != item.Content || strings.TrimSpace(previous.Summary) != item.Summary {
		return domain.KnowledgeValidation{}, errors.New("repair output does not match the exact pending lesson")
	}
	failed, err := r.store.Read(ctx, repair.VerifierSHA256)
	if err != nil {
		return domain.KnowledgeValidation{}, err
	}
	var verification workpacket.VerificationResult
	if err = json.Unmarshal(failed, &verification); err != nil || verification.Accepted || verification.BaseRevision != r.spec.TaskA.BaseRevision || !strings.Contains(strings.Join(verification.Errors, "\n"), "corrupt patch") {
		return domain.KnowledgeValidation{}, errors.New("repair requires a bound corrupt-patch verifier result")
	}
	ctx = domain.WithOperationScope(ctx, domain.OperationScope{ProjectID: task.ProjectID, WorkflowID: task.WorkflowID, TaskID: task.ID})
	packetJSON, err := json.Marshal(r.spec.TaskA)
	if err != nil {
		return domain.KnowledgeValidation{}, err
	}
	for _, data := range [][]byte{packetJSON, []byte(fmt.Sprintf("model_output_sha256=%s\nverifier_result_sha256=%s\nknowledge_id=%s\nversion=%d", repair.ModelOutputSHA256, repair.VerifierSHA256, item.ID, item.Version))} {
		artifact, err := r.store.Put(ctx, data, "text/plain")
		if err != nil {
			return domain.KnowledgeValidation{}, err
		}
		if err = r.app.Service.RecordOperationEvidence(ctx, "pilot.patch_repair.input", artifact); err != nil {
			return domain.KnowledgeValidation{}, err
		}
	}
	// Only the final synthetic answer and sanitized verifier error are disclosed,
	// never the model's separate thinking or arbitrary repository content.
	prompt := "Correct only the numerical line counts in the unified diff hunk headers of this synthetic local patch. Count the actual added and removed lines carefully. Keep every non-hunk-header line byte-for-byte unchanged, including the final newline. Return JSON with patch, summary and lesson. Copy summary and lesson exactly unchanged. Do not claim tests passed. Previous final answer: " + envelope.Response + "\nVerifier error: " + strings.Join(verification.Errors, "; ")
	answer, err := r.generate(ctx, prompt)
	if err != nil {
		return domain.KnowledgeValidation{}, err
	}
	var corrected patchAnswer
	if err = json.Unmarshal(answer, &corrected); err != nil || corrected.Summary != previous.Summary || corrected.Lesson != previous.Lesson || !hunkCountsOnly(previous.Patch, corrected.Patch) {
		return domain.KnowledgeValidation{}, errors.New("local repair changed more than hunk counts; output retained, no validation accepted")
	}
	return r.app.Service.VerifyKnowledgePatch(ctx, service.ValidationInput{
		KnowledgeID: item.ID, ExpectedVersion: item.Version,
		SourceManifest: domain.SourceManifest{SchemaVersion: "hybrid-ai/knowledge-source/v1", Sources: []domain.KnowledgeSource{{Kind: "repository", Reference: r.spec.TaskA.Workspace, Branch: r.spec.Branch, Revision: r.spec.TaskA.BaseRevision}}},
		Criteria:       []domain.ValidationCriterion{{Name: "local hunk-count repair", Passed: true, Observation: "Only hunk counts changed; all source lines and pending lesson preserved. The verifier must still execute every scoped check."}},
	}, r.spec.TaskA, []byte(corrected.Patch))
}

func taskArtifact(trace domain.WorkflowTrace, taskID, name, digest string) bool {
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(digest) {
		return false
	}
	for _, record := range trace.Records {
		if record.TaskID != taskID || record.Name != name || record.Phase != "outcome" || record.Outcome != "success" {
			continue
		}
		for _, ref := range record.References {
			if ref.Kind == "artifact" && ref.SHA256 == digest {
				return true
			}
		}
	}
	return false
}

var hunkHeader = regexp.MustCompile(`^@@ -([0-9]+)(?:,[0-9]+)? \+([0-9]+)(?:,[0-9]+)? @@(.*)$`)

func hunkCountsOnly(before, after string) bool {
	old, next := strings.Split(before, "\n"), strings.Split(after, "\n")
	if before == after || len(old) != len(next) {
		return false
	}
	for i := range old {
		if old[i] == next[i] {
			continue
		}
		a, b := hunkHeader.FindStringSubmatch(old[i]), hunkHeader.FindStringSubmatch(next[i])
		if len(a) != 4 || len(b) != 4 || a[1] != b[1] || a[2] != b[2] || a[3] != b[3] {
			return false
		}
	}
	return true
}
