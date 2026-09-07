package main

import (
	"context"
	"strings"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

func TestRepairRefusesWrongCheckpointBeforeAnyModelOrRepositoryCall(t *testing.T) {
	task := domain.WorkflowTaskCheckpoint{ID: "task-a", State: domain.TaskStateValidationRequired, Version: 3, CandidateID: "candidate"}
	for _, repair := range []*patchRepair{nil, {ExpectedTaskVersion: 2, KnowledgeID: "candidate"}, {ExpectedTaskVersion: 3, KnowledgeID: "different"}} {
		r := runner{spec: spec{RepairTaskA: repair}}
		if _, err := r.repairTaskAPatch(context.Background(), task); err == nil {
			t.Fatal("stale or unbound recovery accepted")
		}
	}
	r := runner{spec: spec{RepairTaskA: &patchRepair{ExpectedTaskVersion: 3, KnowledgeID: "candidate"}}}
	task.State = domain.TaskStatePromotionRequired
	if _, err := r.repairTaskAPatch(context.Background(), task); err == nil {
		t.Fatal("already validated checkpoint accepted for repair")
	}
}

func TestRepairChangesOnlyHunkCounts(t *testing.T) {
	before := "--- /dev/null\n+++ b/labels.py\n@@ -0,0 +1,2 @@\n+pass\n"
	after := strings.Replace(before, "+1,2 @@", "+1,1 @@", 1)
	if !hunkCountsOnly(before, after) {
		t.Fatal("count-only correction refused")
	}
	for _, invalid := range []string{before, strings.Replace(after, "+pass", "+raise Exception()", 1), strings.Replace(after, "+1,1 @@", "+2,1 @@", 1), strings.TrimSuffix(after, "\n"), strings.Replace(after, "labels.py", "keys.py", 1)} {
		if hunkCountsOnly(before, invalid) {
			t.Fatal("non-count repair accepted")
		}
	}
}

func TestRepairEvidenceRequiresExactTaskAndSuccessfulArtifactReceipt(t *testing.T) {
	digest := strings.Repeat("a", 64)
	trace := domain.WorkflowTrace{Records: []domain.OperationRecord{{OperationScope: domain.OperationScope{TaskID: "task-a"}, Name: "model.generate.output", Phase: "outcome", Outcome: "success", References: []domain.EvidenceReference{{Kind: "artifact", SHA256: digest}}}}}
	if !taskArtifact(trace, "task-a", "model.generate.output", digest) {
		t.Fatal("exact evidence not found")
	}
	if taskArtifact(trace, "task-b", "model.generate.output", digest) || taskArtifact(trace, "task-a", "local.verifier.result", digest) || taskArtifact(trace, "task-a", "model.generate.output", "invalid") {
		t.Fatal("unbound artifact accepted")
	}
	trace.Records[0].Outcome = "failed"
	if taskArtifact(trace, "task-a", "model.generate.output", digest) {
		t.Fatal("failed storage receipt accepted")
	}
}
