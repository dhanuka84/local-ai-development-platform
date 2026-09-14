package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/execution"
	"github.com/dhanuka84/hybrid-ai-platform/internal/mcpclient"
	"github.com/dhanuka84/hybrid-ai-platform/internal/sdlcworker"
)

func main() {
	configPath := flag.String("config", "", "operator-owned worker JSON configuration")
	runID := flag.String("run", "", "execute one assigned stage of this run; omitted polls assigned runs")
	describe := flag.Bool("contract-digests", false, "print configured effect digests without opening credentials or connecting")
	flag.Parse()
	if *describe {
		raw, err := os.ReadFile(*configPath)
		var cfg sdlcworker.Config
		if err == nil && len(raw) > 65536 {
			err = domain.ErrBudgetExhausted
		}
		if err == nil {
			err = execution.DecodeProposal(raw, &cfg)
		}
		if err == nil {
			err = json.NewEncoder(os.Stdout).Encode(map[string]string{"delivery_sha256": cfg.Delivery.Digest(), "remediation_sha256": cfg.Remediation.Digest()})
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(*configPath, *runID); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(configPath, runID string) error {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read worker configuration: %w", err)
	}
	if len(raw) > 64*1024 {
		return fmt.Errorf("worker configuration exceeds 64 KiB")
	}
	var cfg sdlcworker.Config
	if err = execution.DecodeProposal(raw, &cfg); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	token, err := mcpclient.ReadToken(cfg.TokenFile)
	if err != nil {
		return err
	}
	client, err := mcpclient.Connect(ctx, cfg.MCPURL, token)
	if err != nil {
		return err
	}
	defer client.Close()
	worker, err := sdlcworker.New(cfg, client)
	if err != nil {
		return err
	}
	if runID != "" {
		result, err := worker.RunOne(ctx, runID)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		var listing struct {
			Runs []domain.ExecutionRun `json:"runs"`
		}
		if err = client.Call(ctx, "sdlc_run_list", map[string]any{"project_id": cfg.ProjectID, "limit": 100}, &listing); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintln(os.Stderr, err)
		} else {
			for _, item := range listing.Runs {
				if domain.ExecutionRole(item.Stage) != cfg.Role || (item.Status != "ready" && item.Status != "running" && item.Status != "reconciling") {
					continue
				}
				result, err := worker.RunOne(ctx, item.ID)
				if err == nil {
					_ = json.NewEncoder(os.Stdout).Encode(result)
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
