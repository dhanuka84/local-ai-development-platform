package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/mcpserver"
	"github.com/dhanuka84/hybrid-ai-platform/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHealthIsPublicAndMCPRequiresToken(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := New("", "token", nil, domain.Principal{}, nil, logger, func(*http.Request) map[string]string {
		return map[string]string{"postgres": "ok"}
	})

	health := httptest.NewRecorder()
	server.Handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d", health.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	unauthorized := httptest.NewRecorder()
	server.Handler.ServeHTTP(unauthorized, request)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("MCP status without token = %d", unauthorized.Code)
	}
}

func TestReadyReportsDependencyFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := New("", "none", nil, domain.Principal{}, nil, logger, func(*http.Request) map[string]string {
		return map[string]string{"postgres": "connection refused"}
	})
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready status = %d", response.Code)
	}
}

type bearerTransport struct{ base http.RoundTripper }

type fakeAuthenticator struct{}

func (fakeAuthenticator) AuthenticateToken(context.Context, string) (domain.Principal, error) {
	return domain.Principal{ID: "human:test", Human: true, RoleBindings: map[string][]string{"*": {"development", "qa", "product_owner", "operations"}}}, nil
}

func (t bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer secret")
	return t.base.RoundTrip(request)
}

func TestStatelessStreamableHTTPListsTools(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := New("", "token", fakeAuthenticator{}, domain.Principal{}, mcpserver.New(&service.Service{}), logger, func(*http.Request) map[string]string {
		return map[string]string{"postgres": "ok"}
	})
	httpTestServer := httptest.NewServer(server.Handler)
	defer httpTestServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "http-test", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             httpTestServer.URL + "/mcp",
		HTTPClient:           &http.Client{Transport: bearerTransport{base: http.DefaultTransport}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 16 {
		t.Fatalf("tool count = %d", len(tools.Tools))
	}
}
