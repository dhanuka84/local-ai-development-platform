package service

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/components/workpacket"
	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
)

func TestExecutedVerifierRecordsActualCommandEvidence(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("fixture git: %s %v", out, err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet", "-b", "main")
	git("-c", "user.name=Synthetic Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "--allow-empty", "-m", "fixture")
	revision := git("rev-parse", "HEAD")
	id := "00000000-0000-4000-8000-000000000099"
	repo := &governanceFake{fakeRepository: &fakeRepository{items: []domain.KnowledgeItem{{ID: id, ProjectID: "product", Version: 1, Content: "synthetic patch lesson", Status: domain.CandidatePending}}}}
	store := artifacts.NewLocalStore(t.TempDir())
	svc := New(repo, store, nil, nil, false, false)
	svc.ConfigureSourceRoots([]string{root})
	if err := svc.ConfigureAuthorization(&fakeAuthorizer{decision: domain.AuthorizationDecision{Allowed: true}}, false); err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithPrincipal(context.Background(), soloDeveloper())
	input := ValidationInput{KnowledgeID: id, ExpectedVersion: 1, SourceManifest: domain.SourceManifest{SchemaVersion: "hybrid-ai/knowledge-source/v1", Sources: []domain.KnowledgeSource{{Kind: "repository", Reference: root, Revision: revision, Branch: "main"}}}, Criteria: []domain.ValidationCriterion{{Name: "synthetic expected change", Passed: true, Observation: "Fixture adds one text line"}}}
	packet := workpacket.Packet{SchemaVersion: workpacket.SchemaVersion, ID: "synthetic-verifier-fixture", Goal: "Add a fixture text file", Workspace: root, BaseRevision: revision, Mode: workpacket.ModePatch, TaskClass: workpacket.TaskDevelopment, DataClassification: workpacket.DataInternal, LocalOnly: true, AllowedFiles: []string{"fixture.txt"}, Rollback: []string{"Discard disposable clone"}, Checks: []workpacket.Check{{Name: "staged-diff", Argv: []string{"git", "diff", "--cached", "--check"}, TimeoutSeconds: 10}}, Limits: workpacket.Limits{MaxChangedFiles: 1, MaxDiffLines: 5, MaxPatchBytes: 10000}}
	patch := []byte("diff --git a/fixture.txt b/fixture.txt\nnew file mode 100644\n--- /dev/null\n+++ b/fixture.txt\n@@ -0,0 +1 @@\n+synthetic fixture\n")
	report, err := svc.VerifyKnowledgePatch(ctx, input, packet, patch)
	if err != nil {
		t.Fatal(err)
	}
	if report.Method != "workpacket" || report.Verdict != "pass" || len(report.Commands) != 1 || report.Commands[0].ExitCode != 0 || len(report.SourceManifest.Sources) != 3 {
		t.Fatalf("incorrect evidence: %+v", report)
	}
	if _, err := store.Read(ctx, report.Commands[0].OutputSHA256); err != nil {
		t.Fatal(err)
	}
	if report.ValidatedBy != soloDeveloper().ID || repo.decisionCalls != 0 {
		t.Fatal("verifier impersonated approval")
	}
	if git("status", "--porcelain") != "" {
		t.Fatal("verifier modified source checkout")
	}
}
