package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/dhanuka84/hybrid-ai-platform/internal/config"
	"github.com/dhanuka84/hybrid-ai-platform/internal/logging"
	"github.com/dhanuka84/hybrid-ai-platform/internal/ollama"
	"github.com/dhanuka84/hybrid-ai-platform/internal/platform"
	workerpkg "github.com/dhanuka84/hybrid-ai-platform/internal/worker"
)

func main() {
	cfg, err := config.LoadCLI()
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
	embedder := ollama.New(cfg.OllamaURL, cfg.EmbeddingModel)
	worker := workerpkg.New(app.Repository, embedder, app.Vectors, logger, cfg.WorkerPollInterval, cfg.WorkerBatchSize)
	if app.Projector != nil {
		worker.ConfigureGraphProjector(app.Projector)
	}
	logger.Info("starting index worker", "batch_size", cfg.WorkerBatchSize, "poll_interval", cfg.WorkerPollInterval)
	if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fail(err)
	}
}

func fail(err error) {
	slog.Error("fatal", "error", err)
	os.Exit(1)
}
