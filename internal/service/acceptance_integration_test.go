package service

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dhanuka84/hybrid-ai-platform/components/workpacket"
	"github.com/dhanuka84/hybrid-ai-platform/internal/artifacts"
	"github.com/dhanuka84/hybrid-ai-platform/internal/authorization"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	"github.com/dhanuka84/hybrid-ai-platform/internal/milvus"
	"github.com/dhanuka84/hybrid-ai-platform/internal/postgres"
	"github.com/dhanuka84/hybrid-ai-platform/internal/worker"
	"github.com/dhanuka84/hybrid-ai-platform/migrations"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type acceptanceEmbedder struct{}

func (acceptanceEmbedder) EmbeddingIdentity() (string, string, int) {
	return "ollama", "synthetic-deterministic-fixture", 4
}
func (acceptanceEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	v := make([][]float32, len(texts))
	for i := range texts {
		v[i] = []float32{1, .1, .2, .3}
	}
	return v, nil
}
func (acceptanceEmbedder) Ping(context.Context) error { return nil }

// This is a synthetic acceptance scenario, not human approval of production
// knowledge and not evidence of real-model quality. PostgreSQL, CAS, the local
// verifier, Cerbos and Milvus are real; embeddings are deterministic fixtures.
func TestAgentReadyAcceptanceIntegration(t *testing.T) {
	db, address, policy := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_MILVUS_ADDRESS"), os.Getenv("TEST_CERBOS_ADDRESS")
	if db == "" || address == "" || policy == "" {
		t.Skip("requires disposable TEST_DATABASE_URL, TEST_MILVUS_ADDRESS and TEST_CERBOS_ADDRESS")
	}
	ctx := context.Background()
	r, err := postgres.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err = migrations.Apply(ctx, r.Pool()); err != nil {
		t.Fatal(err)
	}
	project, _ := domain.NewID()
	human := domain.PrincipalBootstrap{ID: "human:synthetic-acceptance-" + project, Human: true, Roles: []string{"development", "controller", "qa", "product_owner", "operations"}, ProjectIDs: []string{project}}
	executor := domain.PrincipalBootstrap{ID: "workload:synthetic-verifier-" + project, Roles: []string{"validation_executor"}, ProjectIDs: []string{project}}
	if err = r.BootstrapPrincipals(ctx, []domain.PrincipalBootstrap{human, executor}); err != nil {
		t.Fatal(err)
	}
	humanCtx := identity.WithPrincipal(ctx, human.Principal())
	executorCtx := identity.WithPrincipal(ctx, executor.Principal())
	vectors, err := milvus.Open(ctx, address, "", "", "acceptance_"+strings.ReplaceAll(project, "-", ""), 4)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = vectors.Close(ctx) }()
	if err = vectors.EnsureCollection(ctx); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git: %s %v", out, err)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet", "-b", "main")
	git("-c", "user.name=Synthetic Acceptance", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "--allow-empty", "-m", "fixture")
	revision := git("rev-parse", "HEAD")
	svc := New(r, artifacts.NewLocalStore(t.TempDir()), acceptanceEmbedder{}, vectors, true, false)
	svc.ConfigureSourceRoots([]string{root})
	auth, err := authorization.NewCerbos(policy, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ConfigureAuthorization(auth, true); err != nil {
		t.Fatal(err)
	}
	run, err := svc.CreateWorkflow(humanCtx, CreateWorkflowInput{ProjectID: project, Kind: "software-development", Risk: "low", DataClassification: "restricted", Request: "Synthetic local acceptance", IdempotencyKey: "acceptance"})
	if err != nil {
		t.Fatal(err)
	}
	begin := func(key string) domain.WorkflowTaskCheckpoint {
		t.Helper()
		v, _, err := svc.BeginWorkflowTask(humanCtx, BeginWorkflowTaskInput{WorkflowID: run.ID, TaskKey: key, Title: key, TaskType: "maintenance", RAGQuery: "synthetic validated lesson", IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	transition := func(task domain.WorkflowTaskCheckpoint, event, candidate, validation string) domain.WorkflowTaskCheckpoint {
		t.Helper()
		v, _, err := svc.TransitionWorkflowTask(humanCtx, TransitionWorkflowTaskInput{TaskID: task.ID, ExpectedVersion: task.Version, EventType: event, CandidateID: candidate, Provider: "ollama", Model: "synthetic-deterministic-fixture", Evidence: "synthetic acceptance event", IdempotencyKey: task.ID + ":" + event, Payload: map[string]any{"validation_id": validation}})
		if err != nil {
			t.Fatalf("%s: %v", event, err)
		}
		return v
	}
	capture := func(task domain.WorkflowTaskCheckpoint) domain.KnowledgeItem {
		t.Helper()
		v, err := svc.Capture(humanCtx, CaptureInput{ProjectID: project, WorkflowID: run.ID, Prompt: task.Title, Response: "synthetic validated lesson " + task.ID, Summary: "Synthetic local acceptance lesson", Procedure: []string{"Apply scoped patch", "Run local verifier"}, Provider: "ollama", Model: "synthetic-deterministic-fixture", RepositoryRevision: revision, TaskType: "maintenance"})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	verify := func(item domain.KnowledgeItem) domain.KnowledgeValidation {
		t.Helper()
		input := ValidationInput{KnowledgeID: item.ID, ExpectedVersion: item.Version, SourceManifest: domain.SourceManifest{SchemaVersion: "hybrid-ai/knowledge-source/v1", Sources: []domain.KnowledgeSource{{Kind: "repository", Reference: root, Revision: revision, Branch: "main"}}}, Criteria: []domain.ValidationCriterion{{Name: "fixture", Passed: true, Observation: "actual bounded check"}}}
		packet := workpacket.Packet{SchemaVersion: workpacket.SchemaVersion, ID: "acceptance", Goal: "Add synthetic fixture", Workspace: root, BaseRevision: revision, Mode: workpacket.ModePatch, TaskClass: workpacket.TaskDevelopment, DataClassification: workpacket.DataInternal, LocalOnly: true, AllowedFiles: []string{"fixture.txt"}, Rollback: []string{"Discard clone"}, Checks: []workpacket.Check{{Name: "diff", Argv: []string{"git", "diff", "--cached", "--check"}, TimeoutSeconds: 10}}, Limits: workpacket.Limits{MaxChangedFiles: 1, MaxDiffLines: 5, MaxPatchBytes: 10000}}
		patch := []byte("diff --git a/fixture.txt b/fixture.txt\nnew file mode 100644\n--- /dev/null\n+++ b/fixture.txt\n@@ -0,0 +1 @@\n+synthetic acceptance\n")
		v, err := svc.VerifyKnowledgePatch(executorCtx, input, packet, patch)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = svc.RecordManualValidation(executorCtx, input); err == nil {
			t.Fatal("workload forged human attestation")
		}
		return v
	}
	a := begin("task-a")
	lesson := capture(a)
	a = transition(a, "LOCAL_RESULT_RECORDED", lesson.ID, "")
	report := verify(lesson)
	if _, _, err = svc.TransitionWorkflowTask(humanCtx, TransitionWorkflowTaskInput{TaskID: a.ID, ExpectedVersion: a.Version, EventType: "VALIDATION_PASSED", IdempotencyKey: "missing-report", Provider: "ollama", Model: "fixture", Evidence: "pass"}); err == nil {
		t.Fatal("textual pass bypassed local receipt")
	}
	a = transition(a, "VALIDATION_PASSED", "", report.ID)
	if _, err = svc.DecideKnowledge(executorCtx, DecisionInput{KnowledgeID: lesson.ID, ExpectedVersion: lesson.Version, ValidationID: report.ID, Decision: "approve", Reason: "forbidden", IdempotencyKey: "forbidden"}); err == nil {
		t.Fatal("workload approved knowledge")
	}
	lesson, err = svc.DecideKnowledge(humanCtx, DecisionInput{KnowledgeID: lesson.ID, ExpectedVersion: lesson.Version, ValidationID: report.ID, Decision: "approve", Reason: "Synthetic test-only approval", IdempotencyKey: "fixture-approval"})
	if err != nil {
		t.Fatal(err)
	}
	a = transition(a, "LEARNING_PROMOTED", "", "")
	w := worker.New(r, acceptanceEmbedder{}, vectors, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second, 1000)
	for i := 0; i < 20; i++ {
		if _, err = w.ProcessOnce(ctx); err != nil {
			t.Fatal(err)
		}
		q, err := r.KnowledgeQuality(ctx, lesson.ID)
		if err == nil && q.ProjectionVerifiedAt != nil {
			break
		}
	}
	a = transition(a, "RAG_READBACK_VERIFIED", "", "")
	b := begin("task-b")
	if b.Route != domain.TaskRouteRAGHit {
		t.Fatalf("reuse route=%s", b.Route)
	}
	use := RecordTaskContextInput{TaskID: b.ID, ExpectedVersion: b.Version, Contexts: []domain.UsedContext{{KnowledgeID: lesson.ID, Version: lesson.Version, TargetRepository: root, TargetBranch: "main", TargetRevision: revision}}, IdempotencyKey: "explicit-use"}
	if err = svc.RecordTaskContext(humanCtx, use); err != nil {
		t.Fatal(err)
	}
	if err = svc.RecordTaskContext(humanCtx, use); err != nil {
		t.Fatalf("idempotent use: %v", err)
	}
	next := capture(b)
	b = transition(b, "LOCAL_RESULT_RECORDED", next.ID, "")
	nextReport := verify(next)
	b = transition(b, "VALIDATED_REUSE_COMPLETED", "", nextReport.ID)
	pending, err := r.GetKnowledge(ctx, next.ID, true)
	if err != nil || pending.Status != domain.CandidatePending {
		t.Fatal("reuse completion published another lesson")
	}
	evidence, err := svc.WorkflowTrace(humanCtx, run.ID)
	if err != nil || !evidence.Complete {
		t.Fatalf("trace incomplete: %v %v", evidence.Missing, err)
	}
	request := domain.MetricRequest{ProjectID: project, MetricID: "validated_reuse_rate", Version: 1, Start: time.Now().Add(-time.Hour).UTC(), End: time.Now().Add(time.Second).UTC()}
	if _, err = svc.PlatformMetric(humanCtx, request); err == nil {
		t.Fatal("unapproved definition was queryable")
	}
	registry, err := svc.ValidateContextRegistry(humanCtx, project)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := r.ContextDefinition(ctx, project, request.MetricID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.DecideContextDefinition(humanCtx, domain.DefinitionDecision{ProjectID: project, DefinitionID: definition.ID, ExpectedVersion: 1, ExpectedSHA256: definition.SHA256, ValidationID: registry.ID, Decision: "approve", Reason: "Synthetic metric fixture", IdempotencyKey: "fixture-metric"}); err != nil {
		t.Fatal(err)
	}
	result, err := svc.PlatformMetric(humanCtx, request)
	if err != nil || result.Value == nil || *result.Value != .5 || result.Numerator != 1 || result.Denominator != 2 {
		t.Fatalf("metric=%+v err=%v", result, err)
	}
	for i := 0; i < 20; i++ {
		if _, err = w.ProcessOnce(ctx); err != nil {
			t.Fatal(err)
		}
		d, _ := r.ContextDefinition(ctx, project, request.MetricID, 1)
		if d.ProjectionVerifiedAt != nil {
			break
		}
	}
	definitions, err := svc.SearchContextDefinitions(humanCtx, project, "validated reuse", 10)
	if err != nil || len(definitions) != 1 {
		t.Fatalf("semantic definitions=%v err=%v", definitions, err)
	}
	foreign := identity.WithPrincipal(ctx, domain.Principal{ID: "synthetic-foreign", Human: true, RoleBindings: map[string][]string{"another-project": {"operations"}}})
	if _, err = svc.PlatformMetric(foreign, request); err == nil {
		t.Fatal("cross-project metric escaped authorization")
	}
	// Exact documented 8/20 arithmetic fixture, explicitly synthetic rows.
	for i := 0; i < 18; i++ {
		id, _ := domain.NewID()
		fixtureItem := capture(domain.WorkflowTaskCheckpoint{ID: id, Title: "Arithmetic fixture"})
		_, err = r.Pool().Exec(ctx, `INSERT INTO workflow_task_checkpoints(id,workflow_id,ordinal,task_key,title,task_type,state,route,execution_mode,version,rag_query,rag_backend,rag_hit_ids,rag_max_score,match_threshold,request_artifact_sha256,created_by,candidate_id,completed_at) SELECT $1,workflow_id,ordinal+$2,$3,title,task_type,state,route,execution_mode,version,rag_query,rag_backend,rag_hit_ids,rag_max_score,match_threshold,request_artifact_sha256,created_by,$5,now() FROM workflow_task_checkpoints WHERE id=$4`, id, i+1, fmt.Sprint("arithmetic-", i), b.ID, fixtureItem.ID)
		if err != nil {
			t.Fatal(err)
		}
		fixtureReport := verify(fixtureItem)
		if _, err = r.Pool().Exec(ctx, `INSERT INTO workflow_task_local_validations(task_id,validation_id,candidate_version) VALUES($1,$2,1)`, id, fixtureReport.ID); err != nil {
			t.Fatal(err)
		}
		if i < 7 {
			if _, err = r.Pool().Exec(ctx, `INSERT INTO workflow_task_used_context SELECT $1,knowledge_id,candidate_version,validation_id,source_manifest_sha256,target_repository,target_branch,target_revision,actor,idempotency_key,recorded_at FROM workflow_task_used_context WHERE task_id=$2`, id, b.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	request.End = time.Now().Add(time.Second).UTC()
	result, err = svc.PlatformMetric(humanCtx, request)
	if err != nil || result.Value == nil || *result.Value != .4 || result.Numerator != 8 || result.Denominator != 20 {
		t.Fatalf("8/20 metric=%+v err=%v", result, err)
	}
	for _, metric := range []string{"candidate_validation_rate", "candidate_approval_rate", "pending_index_age_seconds"} {
		definition, err := r.ContextDefinition(ctx, project, metric, 1)
		if err != nil {
			t.Fatal(err)
		}
		decision := domain.DefinitionDecision{ProjectID: project, DefinitionID: metric, ExpectedVersion: 1, ExpectedSHA256: definition.SHA256, ValidationID: registry.ID, Decision: "approve", Reason: "Synthetic metric fixture", IdempotencyKey: "fixture-" + metric}
		if _, err = svc.DecideContextDefinition(humanCtx, decision); err != nil {
			t.Fatal(err)
		}
		if _, err = svc.DecideContextDefinition(humanCtx, decision); err != nil {
			t.Fatal("definition retry", err)
		}
		query := request
		query.MetricID = metric
		value, err := svc.PlatformMetric(humanCtx, query)
		if err != nil || value.Value == nil {
			t.Fatalf("%s: %+v %v", metric, value, err)
		}
		if metric == "pending_index_age_seconds" {
			if *value.Value != 0 || value.Backlog != 0 {
				t.Fatal("empty pending snapshot", value)
			}
		} else {
			if *value.Value != 1 {
				t.Fatal("rate", value)
			}
			query.Start = time.Now().Add(-48 * time.Hour)
			query.End = time.Now().Add(-24 * time.Hour)
			empty, err := svc.PlatformMetric(humanCtx, query)
			if err != nil || empty.Value != nil || empty.Denominator != 0 {
				t.Fatal("empty denominator must be null", empty, err)
			}
		}
	}
	// SQL-looking dimension values remain literal parameters, never query text.
	injection := request
	injection.Dimensions = map[string]string{"task_type": "x' OR 1=1 --"}
	empty, err := svc.PlatformMetric(humanCtx, injection)
	if err != nil || empty.Value != nil {
		t.Fatalf("dimension parameterization: %+v %v", empty, err)
	}
	if _, err = r.Pool().Exec(ctx, `UPDATE context_definitions SET validated_at=now()-interval '31 days' WHERE project_id=$1 AND id='validated_reuse_rate'`, project); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.PlatformMetric(humanCtx, request); err == nil {
		t.Fatal("stale definition remained queryable")
	}
	if _, err = svc.SearchContextDefinitions(humanCtx, project, "validated reuse", 10); err != nil {
		t.Fatal(err)
	}
	request.Start = request.End.Add(time.Hour)
	request.End = request.Start.Add(time.Hour)
	if _, err = svc.PlatformMetric(humanCtx, request); err == nil {
		t.Fatal("future window accepted")
	}
	if git("status", "--porcelain") != "" {
		t.Fatal("verifier modified source")
	}
	raw, _ := json.Marshal(evidence)
	if strings.Contains(string(raw), "synthetic acceptance\n") {
		t.Fatal("raw command output leaked into trace")
	}
}
