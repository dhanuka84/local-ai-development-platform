package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/dhanuka84/hybrid-ai-platform/components/workpacket"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
)

// Repair is explicit and narrow: only hunk counts may change.
// It cannot replace source lines, revise the lesson, skip checks, or approve it.
type patchRepair struct {
	Method              string `json:"method,omitempty"`
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

func (r *runner) repairTaskAPatch(ctx context.Context, task domain.WorkflowTaskCheckpoint) (v domain.KnowledgeValidation, err error) {
	repair := r.spec.RepairTaskA
	if repair == nil || task.State != domain.TaskStateValidationRequired || task.Version != repair.ExpectedTaskVersion || task.CandidateID != repair.KnowledgeID {
		return domain.KnowledgeValidation{}, domain.ErrVersionConflict
	}
	if repair.Method != "" && repair.Method != "local_model" && repair.Method != "recount" {
		return domain.KnowledgeValidation{}, errors.New("unknown patch repair method")
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
	ctx, finish, err := r.app.Service.BeginOperation(ctx, "pilot.patch_repair", domain.ScopeFromContext(ctx))
	if err != nil {
		return domain.KnowledgeValidation{}, err
	}
	defer func() { err = errors.Join(err, finish(err)) }()
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
	prompt := "Correct only the numerical line counts in the unified diff hunk headers of this synthetic local patch. Count the actual added and removed lines carefully. Keep every non-hunk-header line byte-for-byte unchanged, including the final newline (which is part of the last line, not an extra blank line). The +++/--- headers do not count as changes. The output must actually correct a count if one is wrong. The lesson, summary, and source bytes must be preserved exactly. Return JSON with patch, summary and lesson. Do not claim tests passed. Previous final answer: " + envelope.Response + "\nVerifier error: " + strings.Join(verification.Errors, "; ")
	var corrected patchAnswer
	if repair.Method == "recount" {
		corrected = previous
		corrected.Patch, err = recountHunks(previous.Patch)
		if err != nil {
			return domain.KnowledgeValidation{}, err
		}
		// This is derived syntax repair, never a replacement model output. Retain
		// the method and exact input binding alongside the separately stored patch.
		derived, marshalErr := json.Marshal(map[string]string{"method": "hunk-recount-v1", "model_output_sha256": repair.ModelOutputSHA256, "verifier_result_sha256": repair.VerifierSHA256, "patch": corrected.Patch})
		if marshalErr != nil {
			return domain.KnowledgeValidation{}, marshalErr
		}
		artifact, storeErr := r.store.Put(ctx, derived, "application/json")
		if storeErr != nil {
			return domain.KnowledgeValidation{}, storeErr
		}
		if err = r.app.Service.RecordOperationEvidence(ctx, "pilot.patch_repair.derived", artifact); err != nil {
			return domain.KnowledgeValidation{}, err
		}
	} else {
		answer, generateErr := r.generate(ctx, prompt)
		if generateErr != nil {
			return domain.KnowledgeValidation{}, generateErr
		}
		if err = json.Unmarshal(answer, &corrected); err != nil {
			return domain.KnowledgeValidation{}, errors.New("invalid JSON in repair response")
		}
	}
	if err = checkRepairAnswer(previous, corrected); err != nil {
		return domain.KnowledgeValidation{}, err
	}
	return r.app.Service.VerifyKnowledgePatch(ctx, service.ValidationInput{
		KnowledgeID: item.ID, ExpectedVersion: item.Version,
		SourceManifest: domain.SourceManifest{SchemaVersion: "hybrid-ai/knowledge-source/v1", Sources: []domain.KnowledgeSource{{Kind: "repository", Reference: r.spec.TaskA.Workspace, Branch: r.spec.Branch, Revision: r.spec.TaskA.BaseRevision}}},
		Criteria:       []domain.ValidationCriterion{{Name: "local hunk-count repair", Passed: true, Observation: "Only hunk counts changed; all source lines and pending lesson preserved. Method: " + repair.Method + ". The verifier must still execute every scoped check."}},
	}, r.spec.TaskA, []byte(corrected.Patch))
}

// recountHunks only rewrites the two counts in recognized unified-diff headers.
// File headers and newline markers are never counted as content. Unknown lines
// within a hunk fail closed; git apply and all packet checks remain mandatory.
func recountHunks(patch string) (string, error) {
	if !strings.HasSuffix(patch, "\n") {
		return "", errors.New("recount requires a newline-terminated unified diff")
	}
	lines := strings.Split(patch, "\n")
	hunks := 0
	for i := 0; i < len(lines)-1; i++ {
		header := hunkHeader.FindStringSubmatch(lines[i])
		if header == nil {
			continue
		}
		hunks++
		oldCount, newCount := 0, 0
		for j := i + 1; j < len(lines)-1; j++ {
			line := lines[j]
			if hunkHeader.MatchString(line) || strings.HasPrefix(line, "diff --git ") ||
				(strings.HasPrefix(line, "--- ") && j+1 < len(lines)-1 && strings.HasPrefix(lines[j+1], "+++ ")) {
				break
			}
			switch {
			case strings.HasPrefix(line, " "):
				oldCount++
				newCount++
			case strings.HasPrefix(line, "+"):
				newCount++
			case strings.HasPrefix(line, "-"):
				oldCount++
			case line == `\ No newline at end of file`:
			default:
				return "", fmt.Errorf("invalid unified-diff content at line %d", j+1)
			}
		}
		lines[i] = "@@ -" + header[1] + "," + strconv.Itoa(oldCount) + " +" + header[2] + "," + strconv.Itoa(newCount) + " @@" + header[3]
	}
	if hunks == 0 {
		return "", errors.New("recount requires at least one unified-diff hunk")
	}
	return strings.Join(lines, "\n"), nil
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

func checkRepairAnswer(previous, corrected patchAnswer) error {
	if previous.Patch == corrected.Patch && previous.Summary == corrected.Summary && previous.Lesson == corrected.Lesson {
		return errors.New("local repair returned the unchanged malformed patch; output retained, no validation accepted")
	}
	if corrected.Summary != previous.Summary {
		return errors.New("summary changed during repair")
	}
	if corrected.Lesson != previous.Lesson {
		return errors.New("lesson changed during repair")
	}
	if !hunkCountsOnly(previous.Patch, corrected.Patch) {
		return errors.New("repair changed more than hunk counts; output retained, no validation accepted")
	}
	return nil
}

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
