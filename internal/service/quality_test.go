package service

import (
	"context"
	"errors"
	"math"
	"os/exec"
	"strings"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

func fixtureVectorHit(item domain.KnowledgeItem, score float32) domain.VectorHit {
	return domain.VectorHit{ID: item.ID, Version: item.Version, ContentSHA256: domain.Digest([]byte(item.RetrievalText())), Score: score}
}

func TestSourceVerificationChecksExactAllowlistedBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		output, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git fixture: %s %v", output, err)
		}
		return strings.TrimSpace(string(output))
	}
	git("init", "--quiet", "-b", "main")
	git("-c", "user.name=Synthetic Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "--allow-empty", "-m", "fixture")
	revision := git("rev-parse", "HEAD")
	manifest := domain.SourceManifest{SchemaVersion: "hybrid-ai/knowledge-source/v1", Sources: []domain.KnowledgeSource{{Kind: "repository", Reference: root, Branch: "main", Revision: revision}}}
	svc := New(&fakeRepository{}, artifacts.NewLocalStore(t.TempDir()), nil, nil, false, false)
	if err := svc.verifySources(context.Background(), manifest); !errors.Is(err, domain.ErrQualityBlocked) {
		t.Fatalf("unconfigured root allowed: %v", err)
	}
	svc.ConfigureSourceRoots([]string{root})
	if err := svc.verifySources(context.Background(), manifest); err != nil {
		t.Fatal(err)
	}
	git("-c", "user.name=Synthetic Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "--allow-empty", "-m", "changed")
	if err := svc.verifySources(context.Background(), manifest); !errors.Is(err, domain.ErrQualityBlocked) || !strings.Contains(err.Error(), "source_changed") {
		t.Fatalf("changed branch allowed: %v", err)
	}
	manifest.Sources[0].ApplicableThrough = git("rev-parse", "HEAD")
	if err := svc.verifySources(context.Background(), manifest); err != nil {
		t.Fatalf("explicit bounded applicability: %v", err)
	}
	git("-c", "user.name=Synthetic Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "--allow-empty", "-m", "outside range")
	if err := svc.verifySources(context.Background(), manifest); !errors.Is(err, domain.ErrQualityBlocked) {
		t.Fatal("open-ended applicability was accepted", err)
	}
}

func TestSearchRejectsStaleCrossProjectAndMalformedVectors(t *testing.T) {
	item := domain.KnowledgeItem{ID: "lesson", ProjectID: "product", Version: 2, Status: domain.CandidateApproved, Content: "current"}
	for _, name := range []string{"legacy", "old-version", "wrong-digest", "cross-project", "nan", "infinity"} {
		t.Run(name, func(t *testing.T) {
			hit := fixtureVectorHit(item, .9)
			project := "product"
			switch name {
			case "legacy":
				hit.Version = 0
				hit.ContentSHA256 = ""
			case "old-version":
				hit.Version--
			case "wrong-digest":
				hit.ContentSHA256 = domain.Digest([]byte("old"))
			case "cross-project":
				project = "other"
			case "nan":
				hit.Score = float32(math.NaN())
			case "infinity":
				hit.Score = float32(math.Inf(1))
			}
			repo := &fakeRepository{items: []domain.KnowledgeItem{item}, lexical: []domain.SearchHit{{KnowledgeItem: item}}}
			svc := New(repo, &fakeArtifacts{}, &fakeEmbedder{vectors: [][]float32{{1}}}, &fakeVectors{hits: []domain.VectorHit{hit}}, true, false)
			result, _, err := svc.Search(context.Background(), project, "query", 5)
			if !errors.Is(err, domain.ErrQualityBlocked) || len(result) != 0 {
				t.Fatalf("result=%v err=%v", result, err)
			}
		})
	}
}

func TestQualityBlockedTaskNeverChoosesCloudRoute(t *testing.T) {
	item := domain.KnowledgeItem{ID: "lesson", ProjectID: "product", Version: 2, Status: domain.CandidateApproved}
	repo := &fakeRepository{workflow: checkpointWorkflow("internal"), items: []domain.KnowledgeItem{item}}
	svc, ctx := checkpointService(repo, &fakeVectors{hits: []domain.VectorHit{{ID: item.ID, Score: .99}}})
	_, _, err := svc.BeginWorkflowTask(ctx, BeginWorkflowTaskInput{WorkflowID: "workflow", TaskKey: "quality", Title: "Quality", RAGQuery: "known", IdempotencyKey: "quality"})
	if !errors.Is(err, domain.ErrQualityBlocked) {
		t.Fatal(err)
	}
	for _, task := range repo.tasks {
		if task.Route == domain.TaskRouteRAGMissCloudReview {
			t.Fatal("quality failure became cloud route")
		}
	}
}
