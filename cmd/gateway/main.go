package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/config"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/httpserver"
	"github.com/dhanuka84/hybrid-ai-platform/internal/logging"
	"github.com/dhanuka84/hybrid-ai-platform/internal/mcpserver"
	"github.com/dhanuka84/hybrid-ai-platform/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) (err error) {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := logging.New(cfg.LogLevel)
	slog.SetDefault(logger)
	app, err := platform.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		err = errors.Join(err, app.Close(closeCtx))
	}()
	if err := app.Initialize(ctx); err != nil {
		return err
	}
	var localPrincipal domain.Principal
	for _, bootstrap := range cfg.AuthPrincipals {
		if bootstrap.Human {
			localPrincipal = bootstrap.Principal()
			break
		}
	}
	if localPrincipal.ID == "" && len(cfg.AuthPrincipals) > 0 {
		localPrincipal = cfg.AuthPrincipals[0].Principal()
	}
	server := mcpserver.New(app.Service, localPrincipal)
	if cfg.MCPTransport == "stdio" {
		logger.Info("starting MCP gateway", "transport", "stdio")
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	}

	httpServer := httpserver.New(cfg.HTTPAddress, cfg.AuthMode, app.Service, localPrincipal, server, logger, func(r *http.Request) map[string]string {
		checkCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		return app.Service.Dependencies(checkCtx)
	})
	listener, err := net.Listen("tcp", cfg.HTTPAddress)
	if err != nil {
		return err
	}
	logger.Info("starting MCP gateway", "transport", "streamable-http", "address", cfg.HTTPAddress, "endpoint", "/mcp")
	return httpserver.Serve(ctx, httpServer, listener, 15*time.Second)
}
