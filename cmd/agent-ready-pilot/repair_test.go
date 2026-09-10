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

func TestCheckRepairAnswer(t *testing.T) {
	prev := patchAnswer{Patch: "@@ -1,2 +1,3 @@\nold", Summary: "s", Lesson: "l"}
	// Unchanged
	if err := checkRepairAnswer(prev, prev); err == nil || !strings.Contains(err.Error(), "unchanged") {
		t.Fatal("expected unchanged error")
	}
	// Count only change
	changed := patchAnswer{Patch: "@@ -1,2 +1,4 @@\nold", Summary: "s", Lesson: "l"}
	if err := checkRepairAnswer(prev, changed); err != nil {
		t.Fatalf("expected no error for count-only change, got %v", err)
	}
	// Source change
	srcChange := patchAnswer{Patch: "@@ -1,2 +1,3 @@\nnew", Summary: "s", Lesson: "l"}
	if err := checkRepairAnswer(prev, srcChange); err == nil || !strings.Contains(err.Error(), "more than hunk") {
		t.Fatal("expected source change error")
	}
	// Lesson change
	lessChange := patchAnswer{Patch: "@@ -1,2 +1,3 @@\nold", Summary: "s", Lesson: "x"}
	if err := checkRepairAnswer(prev, lessChange); err == nil || !strings.Contains(err.Error(), "lesson") {
		t.Fatal("expected lesson change error")
	}
	// Summary change
	sumChange := patchAnswer{Patch: "@@ -1,2 +1,3 @@\nold", Summary: "x", Lesson: "l"}
	if err := checkRepairAnswer(prev, sumChange); err == nil || !strings.Contains(err.Error(), "summary") {
		t.Fatal("expected summary change error")
	}
}

func TestRecountHunksPreservesSourceAndCountsOnlyContent(t *testing.T) {
	for _, tc := range []struct{ name, before, want string }{
		{"new file", "--- /dev/null\n+++ b/a.py\n@@ -0,0 +1,3 @@\n+pass\n", "--- /dev/null\n+++ b/a.py\n@@ -0,0 +1,1 @@\n+pass\n"},
		{"context removal and blank addition", "--- a/a\n+++ b/a\n@@ -2,9 +2,8 @@ func\n same\n-old\n+new\n+\n", "--- a/a\n+++ b/a\n@@ -2,2 +2,3 @@ func\n same\n-old\n+new\n+\n"},
		{"multiple files and newline marker", "--- a/a\n+++ b/a\n@@ -1,7 +1,7 @@\n-x\n+y\n\\ No newline at end of file\n--- /dev/null\n+++ b/b\n@@ -0,0 +1,4 @@\n+z\n", "--- a/a\n+++ b/a\n@@ -1,1 +1,1 @@\n-x\n+y\n\\ No newline at end of file\n--- /dev/null\n+++ b/b\n@@ -0,0 +1,1 @@\n+z\n"},
		{"multiple hunks", "--- a/a\n+++ b/a\n@@ -1,9 +1,9 @@\n-x\n+y\n@@ -8,9 +8,9 @@\n next\n-z\n", "--- a/a\n+++ b/a\n@@ -1,1 +1,1 @@\n-x\n+y\n@@ -8,2 +8,1 @@\n next\n-z\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := recountHunks(tc.before)
			if err != nil || got != tc.want {
				t.Fatalf("recount: %v\ngot %q\nwant %q", err, got, tc.want)
			}
			if !hunkCountsOnly(tc.before, got) {
				t.Fatal("recount changed protected bytes")
			}
		})
	}
	for _, invalid := range []string{"not a diff\n", "@@ -0,0 +1,2 @@\n+pass", "@@ -0,0 +1,2 @@\n+pass\n\n", "@@ -0,0 +1,2 @@\ninvalid\n"} {
		if _, err := recountHunks(invalid); err == nil {
			t.Fatalf("malformed input accepted: %q", invalid)
		}
	}
}
