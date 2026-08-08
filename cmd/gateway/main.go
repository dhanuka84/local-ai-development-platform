package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/config"
	"github.com/dhanuka84/hybrid-ai-platform/internal/httpserver"
	"github.com/dhanuka84/hybrid-ai-platform/internal/logging"
	"github.com/dhanuka84/hybrid-ai-platform/internal/mcpserver"
	"github.com/dhanuka84/hybrid-ai-platform/internal/platform"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fail(err)
	}
	logger := logging.New(cfg.LogLevel)
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := platform.Open(ctx, cfg)
	if err != nil {
		fail(err)
	}
	defer func() { _ = app.Close(context.Background()) }()
	if err := app.Initialize(ctx); err != nil {
		fail(err)
	}
	server := mcpserver.New(app.Service)
	if cfg.MCPTransport == "stdio" {
		logger.Info("starting MCP gateway", "transport", "stdio")
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
			fail(err)
		}
		return
	}

	httpServer := httpserver.New(cfg.HTTPAddress, cfg.AuthMode, cfg.AuthToken, server, logger, func(r *http.Request) map[string]string {
		checkCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		return app.Service.Dependencies(checkCtx)
	})
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	logger.Info("starting MCP gateway", "transport", "streamable-http", "address", cfg.HTTPAddress, "endpoint", "/mcp")
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fail(err)
	}
}

func fail(err error) {
	slog.Error("fatal", "error", err)
	os.Exit(1)
}
