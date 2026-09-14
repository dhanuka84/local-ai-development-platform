package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Client struct{ session *mcp.ClientSession }
type bearer struct {
	token string
	base  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

func ReadToken(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 8192 {
		return "", errors.New("credential file must be private, regular and bounded")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", errors.New("credential file is empty")
	}
	return token, nil
}
func Connect(ctx context.Context, endpoint, token string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || token == "" {
		return nil, errors.New("invalid MCP endpoint or credential")
	}
	if u.Scheme == "http" {
		ip := net.ParseIP(u.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return nil, errors.New("plaintext MCP is restricted to literal loopback addresses")
		}
	}
	httpClient := &http.Client{Timeout: 45 * time.Second, Transport: bearer{token: token, base: http.DefaultTransport}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("MCP redirects are forbidden") }}
	client := mcp.NewClient(&mcp.Implementation{Name: "hybrid-ai-sdlc-worker", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: httpClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return nil, errors.New("MCP connection failed")
	}
	return &Client{session: session}, nil
}
func (c *Client) Close() error { return c.session.Close() }

func (c *Client) RequireReadOnly(ctx context.Context, name string) error {
	listing, err := c.session.ListTools(ctx, nil)
	if err != nil {
		return errors.New("MCP source tool discovery failed")
	}
	for _, tool := range listing.Tools {
		if tool.Name == name && tool.Annotations != nil && tool.Annotations.ReadOnlyHint {
			return nil
		}
	}
	return errors.New("MCP source tool is absent or lacks the operator-required read-only contract")
}
func (c *Client) Call(ctx context.Context, name string, input, output any) error {
	result, err := c.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: input})
	if err != nil {
		return fmt.Errorf("MCP %s transport failed", name)
	}
	if result.IsError {
		return fmt.Errorf("MCP %s was denied or failed; inspect the authorized execution trace", name)
	}
	if output == nil {
		return nil
	}
	if result.StructuredContent != nil {
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, output)
	}
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			return json.Unmarshal([]byte(text.Text), output)
		}
	}
	return fmt.Errorf("MCP %s returned no structured result", name)
}
