package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/config"
	"github.com/dhanuka84/hybrid-ai-platform/internal/logging"
	"github.com/dhanuka84/hybrid-ai-platform/internal/ollama"
	"github.com/dhanuka84/hybrid-ai-platform/internal/platform"
	"github.com/dhanuka84/hybrid-ai-platform/internal/telemetry"
	workerpkg "github.com/dhanuka84/hybrid-ai-platform/internal/worker"
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
	cfg, err := config.LoadCLI()
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
	embedder := ollama.New(cfg.OllamaURL, cfg.EmbeddingModel)
	worker := workerpkg.New(app.Repository, embedder, app.Vectors, logger, cfg.WorkerPollInterval, cfg.WorkerBatchSize)
	worker.ConfigureSourceVerifier(app.Service.RefreshKnowledgeSources)
	if cfg.TraceExportEndpoint != "" {
		exporter, err := telemetry.NewExporter(app.Repository, cfg.TraceExportEndpoint)
		if err != nil {
			return err
		}
		worker.ConfigureTraceExporter(exporter)
	}
	if app.Projector != nil {
		worker.ConfigureGraphProjector(app.Projector)
	}
	logger.Info("starting index worker", "batch_size", cfg.WorkerBatchSize, "poll_interval", cfg.WorkerPollInterval)
	if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
