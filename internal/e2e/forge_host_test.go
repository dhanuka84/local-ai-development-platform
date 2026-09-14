//go:build sdlc_host

package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
)

type forgeFixture struct {
	configs  map[string]execution.DeliveryConfig
	written  chan struct{}
	released chan struct{}
}

func setupForgeFixture(t *testing.T, f *runtime) forgeFixture {
	t.Helper()
	endpoint := os.Getenv("TEST_FORGE_ENDPOINT")
	if endpoint == "" {
		t.Fatal("missing disposable forge")
	}
	request := func(user, method, path string, body any) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, e := http.NewRequestWithContext(f.ctx, method, endpoint+"/api/v1"+path, bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		req.SetBasicAuth(user, "synthetic-forge-only")
		req.Header.Set("Content-Type", "application/json")
		r, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		if r.StatusCode < 200 || r.StatusCode >= 300 {
			t.Fatalf("disposable forge %s %s: %d %s", method, path, r.StatusCode, data)
		}
		return data
	}
	tokens := map[string]string{}
	for _, user := range []string{"synthetic-delivery", "synthetic-evaluator"} {
		raw := request(user, "POST", "/users/"+user+"/tokens", map[string]any{"name": f.project, "scopes": []string{"write:repository"}})
		var token struct {
			SHA1 string `json:"sha1"`
		}
		if json.Unmarshal(raw, &token) != nil || token.SHA1 == "" {
			t.Fatal("fixture token missing")
		}
		tokens[user] = token.SHA1
	}
	name := "release-" + strings.TrimPrefix(f.project, "pilot-e2e-")
	repository := "synthetic-delivery/" + name
	request("synthetic-delivery", "POST", "/user/repos", map[string]any{"name": name, "private": true, "default_branch": "main", "auto_init": false})
	request("synthetic-delivery", "PUT", "/repos/"+repository+"/collaborators/synthetic-evaluator", map[string]string{"permission": "write"})
	command := exec.CommandContext(f.ctx, "git", "-c", "core.hooksPath=/dev/null", "push", "--quiet", endpoint+"/"+repository+".git", "HEAD:refs/heads/main")
	command.Dir = f.source
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("sdlc:"+tokens["synthetic-delivery"]))}
	if raw, e := command.CombinedOutput(); e != nil {
		t.Fatalf("seed disposable forge: %v %s", e, raw)
	}
	// The Git receive hook updates the forge's repository metadata separately.
	// Wait for its API read-back before submitting an executable target.
	f.wait("native forge seeded branch read-back", func() bool {
		req, err := http.NewRequestWithContext(f.ctx, "GET", endpoint+"/api/v1/repos/"+repository+"/branches/main", nil)
		if err != nil {
			return false
		}
		req.Header.Set("Authorization", "token "+tokens["synthetic-delivery"])
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer r.Body.Close()
		var out struct {
			Commit struct {
				ID string `json:"id"`
			} `json:"commit"`
		}
		return r.StatusCode == 200 && json.NewDecoder(r.Body).Decode(&out) == nil && out.Commit.ID == f.revision
	})
	fixture := forgeFixture{configs: map[string]execution.DeliveryConfig{}, written: make(chan struct{}), released: make(chan struct{})}
	var once sync.Once
	// Forward to the actual forge and hold the first successful PR response.
	// The test kills the worker here, after the write but before acknowledgement.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := url.Parse(endpoint)
		copy := r.Clone(r.Context())
		copy.URL.Scheme = u.Scheme
		copy.URL.Host = u.Host
		copy.Host = u.Host
		copy.RequestURI = ""
		response, e := http.DefaultTransport.RoundTrip(copy)
		if e != nil {
			http.Error(w, "fixture upstream unavailable", 502)
			return
		}
		defer response.Body.Close()
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/pulls") && response.StatusCode == 201 {
			once.Do(func() {
				close(fixture.written)
				select {
				case <-fixture.released:
				case <-r.Context().Done():
				}
			})
		}
		for name, values := range response.Header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	t.Cleanup(proxy.Close)
	for _, role := range []string{"sdlc_delivery", "sdlc_evaluator"} {
		user := "synthetic-delivery"
		if role == "sdlc_evaluator" {
			user = "synthetic-evaluator"
		}
		fixture.configs[role] = execution.DeliveryConfig{ForgeURL: proxy.URL, Repository: repository, BaseBranch: "main", TokenFile: f.write(role+"-forge.token", []byte(tokens[user])), ArtifactRoot: filepath.Join(f.root, "release-artifacts"), StagingRoot: filepath.Join(f.root, "staging")}
	}
	return fixture
}
