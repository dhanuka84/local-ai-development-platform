// Package e2e exercises the shipped binaries over HTTP and the operator CLI.
// Every identity, repository, embedding and approval is a disposable fixture.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/postgres"
)

type runtime struct {
	t                                        *testing.T
	ctx                                      context.Context
	root, source, revision, bin              string
	project, operator, executor              string
	foreign, developer, controller, endpoint string
	env                                      map[string]string
	repo                                     *postgres.Repository
	generationResponses                      chan []byte
	stopGateway, stopWorker                  func()
}

func startRuntime(t *testing.T) *runtime {
	t.Helper()
	if os.Getenv("TEST_AGENT_READY_DISPOSABLE") != "true" {
		t.Skip("run make agent-ready-integration; requires explicitly disposable dependencies")
	}
	for _, name := range []string{"TEST_DATABASE_URL", "TEST_MILVUS_ADDRESS", "TEST_CERBOS_ADDRESS", "TEST_OTEL_ENDPOINT", "TEST_BINARY_DIR"} {
		if os.Getenv(name) == "" {
			t.Fatalf("missing %s in explicitly enabled E2E run", name)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	id, err := domain.NewID()
	if err != nil {
		t.Fatal(err)
	}
	f := &runtime{t: t, ctx: ctx, root: t.TempDir(), project: "pilot-e2e-" + id,
		operator: "synthetic-operator-" + id, executor: "synthetic-executor-" + id,
		developer: "synthetic-development-only-" + id, controller: "synthetic-controller-" + id,
		foreign: "synthetic-foreign-" + id, bin: os.Getenv("TEST_BINARY_DIR"), generationResponses: make(chan []byte, 8)}
	if err = os.Chmod(f.root, 0700); err != nil {
		t.Fatal(err)
	}
	f.source = filepath.Join(f.root, "source")
	if err = os.Mkdir(f.source, 0700); err != nil {
		t.Fatal(err)
	}
	f.git("init", "--quiet", "-b", "main")
	f.write("source/test_labels.py", []byte("import unittest\nfrom labels import normalize\nclass TestLabels(unittest.TestCase):\n    def test_behavior(self):\n        self.assertEqual(normalize('  StraßE \\t CAT  '), 'strasse cat')\n        self.assertEqual(normalize('  '), '')\n        self.assertEqual(normalize(normalize(' A   B ')), 'a b')\n"))
	f.write("source/test_keys.py", []byte("import unittest\nfrom keys import normalize\nclass TestKeys(unittest.TestCase):\n    def test_behavior(self):\n        self.assertEqual(normalize('  StraßE \\t CAT  '), 'strasse-cat')\n        self.assertEqual(normalize('  '), '')\n        self.assertEqual(normalize(normalize(' A   B ')), 'a-b')\n"))
	f.git("add", ".")
	f.git("-c", "user.name=Synthetic E2E", "-c", "user.email=e2e@example.invalid", "commit", "--quiet", "-m", "Synthetic read-only behavior tests")
	f.revision = f.git("rev-parse", "HEAD")
	admin, err := postgres.Open(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	dbName := "pilot_e2e_" + strings.ReplaceAll(id, "-", "")
	if _, err = admin.Pool().Exec(ctx, "CREATE DATABASE "+dbName+" TEMPLATE template0"); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		if _, err := admin.Pool().Exec(cleanupCtx, "DROP DATABASE "+dbName+" WITH (FORCE)"); err != nil {
			t.Error("remove owned synthetic database:", err)
		}
		admin.Close()
	})
	dbURL, err := url.Parse(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	dbURL.Path = "/" + dbName
	// Only the local Ollama protocol is substituted. Production adapters, the
	// verifier, PostgreSQL, Cerbos, Milvus and the collector execute normally.
	embeddings := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tags":
			_, _ = io.WriteString(w, `{"models":[]}`)
		case "/api/generate":
			select {
			case answer := <-f.generationResponses:
				_, _ = w.Write(answer)
			default:
				http.Error(w, "no synthetic generation scheduled", http.StatusBadRequest)
			}
		case "/api/embed":
			var request struct {
				Input []string `json:"input"`
			}
			if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Input) == 0 {
				http.Error(w, "invalid fixture embedding request", 400)
				return
			}
			vectors := make([][]float32, len(request.Input))
			for i := range vectors {
				vectors[i] = []float32{1, .1, .2, .3}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": vectors})
		default:
			http.Error(w, "fixture does not support model generation", 404)
		}
	}))
	t.Cleanup(embeddings.Close)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	f.endpoint = "http://" + address
	f.env = map[string]string{
		"PATH": os.Getenv("PATH"), "APP_ENV": "local", "LOG_LEVEL": "warn",
		"AUTH_MODE": "token", "AUTHORIZATION_MODE": "cerbos", "AUTH_TOKEN": f.operator,
		"CONTROLLER_AUTH_TOKEN": f.controller,
		"DATABASE_URL":          dbURL.String(), "CERBOS_ADDRESS": os.Getenv("TEST_CERBOS_ADDRESS"),
		"MILVUS_ADDRESS": os.Getenv("TEST_MILVUS_ADDRESS"), "MILVUS_DATABASE": "",
		"MILVUS_COLLECTION": dbName, "OLLAMA_URL": embeddings.URL,
		"OLLAMA_EMBEDDING_MODEL": "synthetic-e2e-fixture", "EMBEDDING_DIMENSION": "4",
		"ARTIFACTS_PATH": filepath.Join(f.root, "artifacts"), "GRAPH_BACKEND": "postgres",
		"CODEGRAPH_ENABLED": "false", "CODEGRAPH_ALLOWED_ROOTS": f.source,
		"AUTO_APPROVE_LOCAL": "false", "SEARCH_LEXICAL_FALLBACK": "true",
		"HTTP_ADDRESS": address, "WORKER_POLL_INTERVAL": "100ms", "WORKER_BATCH_SIZE": "100",
		"TRACE_EXPORT_ENDPOINT": os.Getenv("TEST_OTEL_ENDPOINT"), "PYTHONDONTWRITEBYTECODE": "1",
	}
	f.cli(f.operator, true, "migrate")
	f.repo, err = postgres.Open(ctx, dbURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.repo.Close)
	// Exercise the real default AUTH_TOKEN and CONTROLLER_AUTH_TOKEN bootstrap.
	// Only the additional least-privilege and foreign test identities are custom.
	if err = f.repo.BootstrapPrincipals(ctx, []domain.PrincipalBootstrap{
		{ID: "workload:" + id, Token: f.executor, Roles: []string{"validation_executor", "development", "controller"}, ProjectIDs: []string{f.project}},
		{ID: "human:development-only-" + id, Token: f.developer, Human: true, Roles: []string{"development"}, ProjectIDs: []string{f.project}},
		{ID: "human:foreign-" + id, Token: f.foreign, Human: true, Roles: []string{"development", "qa", "product_owner", "operations"}, ProjectIDs: []string{f.project + "-other"}},
	}); err != nil {
		t.Fatal(err)
	}
	f.stopGateway = f.start("gateway")
	f.wait("gateway readiness", func() bool {
		response, err := http.Get(f.endpoint + "/readyz")
		if err != nil {
			return false
		}
		defer response.Body.Close()
		return response.StatusCode == http.StatusOK
	})
	f.stopWorker = f.start("worker")
	return f
}

func (f *runtime) write(name string, data []byte) string {
	f.t.Helper()
	path := filepath.Join(f.root, name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		f.t.Fatal(err)
	}
	return path
}

func (f *runtime) writeJSON(name string, value any) string {
	f.t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		f.t.Fatal(err)
	}
	return f.write(name, raw)
}

func (f *runtime) git(args ...string) string {
	f.t.Helper()
	command := exec.CommandContext(f.ctx, "git", append([]string{"-C", f.source}, args...)...)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null"}
	out, err := command.CombinedOutput()
	if err != nil {
		f.t.Fatalf("synthetic git: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func (f *runtime) command(binary, token string, args ...string) *exec.Cmd {
	command := exec.CommandContext(f.ctx, filepath.Join(f.bin, binary), args...)
	command.Dir = f.root
	for key, value := range f.env {
		if key == "AUTH_TOKEN" {
			value = token
		}
		command.Env = append(command.Env, key+"="+value)
	}
	return command
}

func (f *runtime) cli(token string, success bool, args ...string) []byte {
	f.t.Helper()
	return f.invoke("admin", token, success, args...)
}

func (f *runtime) invoke(binary, token string, success bool, args ...string) []byte {
	f.t.Helper()
	command := f.command(binary, token, args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if (err == nil) != success {
		f.t.Fatalf("%s: success=%v, err=%v, diagnostics=%s", binary, success, err, stderr.String())
	}
	return out
}

func (f *runtime) start(binary string) func() {
	f.t.Helper()
	test := f.t
	command := f.command(binary, f.operator)
	log, err := os.CreateTemp(f.root, binary+"-*.log")
	if err != nil {
		f.t.Fatal(err)
	}
	command.Stdout, command.Stderr = log, log
	if err = command.Start(); err != nil {
		_ = log.Close()
		f.t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = command.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(20 * time.Second):
				_ = command.Process.Kill()
				<-done
				test.Error(binary + " did not shut down gracefully")
			}
			_ = log.Close()
			if test.Failed() {
				data, _ := os.ReadFile(log.Name())
				test.Logf("%s synthetic runtime diagnostics: %s", binary, data)
			}
		})
	}
	test.Cleanup(stop)
	return stop
}

func (f *runtime) wait(label string, predicate func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) && f.ctx.Err() == nil {
		if predicate() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	f.t.Fatal("timed out: " + label)
}

func (f *runtime) rpc(token, method string, params any) (int, []byte) {
	f.t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		f.t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.endpoint+"/mcp", bytes.NewReader(body))
	if err != nil {
		f.t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		f.t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		f.t.Fatal(err)
	}
	return response.StatusCode, raw
}

func (f *runtime) call(token, name string, input any, success bool, output any) {
	f.t.Helper()
	status, raw := f.rpc(token, "tools/call", map[string]any{"name": name, "arguments": input})
	var envelope struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			IsError    bool            `json:"isError"`
			Structured json.RawMessage `json:"structuredContent"`
			Content    []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		f.t.Fatalf("%s: invalid JSON: %v", name, err)
	}
	ok := status == http.StatusOK && len(envelope.Error) == 0 && !envelope.Result.IsError
	if !success && len(envelope.Result.Content) > 0 && strings.Contains(envelope.Result.Content[0].Text, `validating "arguments"`) {
		f.t.Fatalf("%s: malformed test input cannot prove a policy denial: %s", name, raw)
	}
	if ok != success {
		f.t.Fatalf("%s: wanted success=%v, HTTP %d: %s", name, success, status, raw)
	}
	if !ok || output == nil {
		return
	}
	data := envelope.Result.Structured
	if len(data) == 0 && len(envelope.Result.Content) > 0 {
		data = []byte(envelope.Result.Content[0].Text)
	}
	if err := json.Unmarshal(data, output); err != nil {
		f.t.Fatalf("%s: decode result: %v (%s)", name, err, data)
	}
}

func decode[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(fmt.Errorf("decode CLI result: %w", err))
	}
	return value
}
