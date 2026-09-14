// Package agentmodel invokes only a digest-pinned local Ollama model. Cloud
// models, public inference endpoints, redirects and fallback routes are absent.
package agentmodel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
)

type Client struct {
	endpoint string
	http     *http.Client
}
type Result struct {
	Request  []byte
	Response []byte
	Output   execution.ModelResponse
	Duration time.Duration
}

func New(endpoint string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("local Ollama endpoint must be an HTTP origin without credentials")
	}
	transport := &http.Transport{DialContext: localDial, MaxIdleConns: 2, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second}
	return &Client{endpoint: strings.TrimRight(endpoint, "/"), http: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("model redirects are forbidden") }}}, nil
}

func localDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, errors.New("local model address could not be resolved")
	}
	if len(addresses) == 0 {
		return nil, errors.New("local model address is empty")
	}
	for _, a := range addresses {
		if !a.IP.IsLoopback() && !a.IP.IsPrivate() {
			return nil, errors.New("public model endpoints are forbidden")
		}
	}
	var last error
	for _, a := range addresses {
		conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(a.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}

func (c *Client) request(ctx context.Context, method, path string, raw []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("local Ollama request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local Ollama returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 2*1024*1024 {
		return nil, errors.New("local model response exceeds 2 MiB")
	}
	return body, nil
}

func (c *Client) CheckModel(ctx context.Context, p domain.AgentPackage) error {
	raw, err := c.request(ctx, http.MethodGet, "/api/tags", nil)
	if err != nil {
		return err
	}
	var result struct {
		Models []struct {
			Name        string `json:"name"`
			Model       string `json:"model"`
			Digest      string `json:"digest"`
			RemoteHost  string `json:"remote_host"`
			RemoteModel string `json:"remote_model"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return errors.New("invalid local model inventory")
	}
	for _, m := range result.Models {
		if m.Name == p.Model || m.Model == p.Model {
			if strings.TrimPrefix(m.Digest, "sha256:") != p.ModelSHA256 || m.RemoteHost != "" || m.RemoteModel != "" {
				return errors.New("model digest changed or inventory identifies remote inference")
			}
			return nil
		}
	}
	return errors.New("pinned local model is unavailable; no fallback is permitted")
}

func (c *Client) Generate(ctx context.Context, p domain.AgentPackage, prompt string) (out Result, err error) {
	return c.generate(ctx, p, prompt, json.RawMessage(`"json"`))
}

func (c *Client) GenerateForExecution(ctx context.Context, p domain.AgentPackage, prompt string) (Result, error) {
	return c.generate(ctx, p, prompt, execution.ProposalFormat(p.Role))
}

func (c *Client) generate(ctx context.Context, p domain.AgentPackage, prompt string, format json.RawMessage) (out Result, err error) {
	if err = p.Validate(); err != nil {
		return out, err
	}
	if len(prompt) > p.MaxInputBytes || p.Model == "" {
		return out, domain.ErrBudgetExhausted
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutSeconds)*time.Second)
	defer cancel()
	if err = c.CheckModel(ctx, p); err != nil {
		return out, err
	}
	req := execution.ModelRequest{Model: p.Model, System: p.Instructions, Prompt: prompt, Format: format, Stream: false, Think: false, Options: execution.ModelOptions{NumPredict: p.MaxOutputTokens, Temperature: 0, Seed: 1}, KeepAlive: "0"}
	out.Request, err = json.Marshal(req)
	if err != nil {
		return out, err
	}
	start := time.Now()
	out.Response, err = c.request(ctx, http.MethodPost, "/api/generate", out.Request)
	out.Duration = time.Since(start)
	if err != nil {
		return out, err
	}
	if json.Unmarshal(out.Response, &out.Output) != nil || !out.Output.Done || out.Output.DoneReason == "length" || out.Output.Model != p.Model || out.Output.EvalCount < 1 || out.Output.EvalCount > p.MaxOutputTokens || out.Output.PromptEvalCount < 0 || out.Output.PromptEvalCount > p.MaxInputBytes {
		return out, errors.New("local model did not produce a complete bounded response")
	}
	if err = c.CheckModel(ctx, p); err != nil {
		return out, err
	}
	return out, nil
}
