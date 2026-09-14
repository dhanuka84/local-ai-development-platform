package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/sourceadapter"
)

func main() {
	path := flag.String("config", "", "operator-owned native source configuration")
	describe := flag.Bool("contract-digests", false, "print source IDs and backend digests without opening credentials or connecting")
	flag.Parse()
	if *describe {
		cfg, err := sourceadapter.Load(*path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		out := map[string]string{}
		for _, v := range cfg.Views {
			out[v.Contract.ID] = v.Backend.Digest()
		}
		if err = json.NewEncoder(os.Stdout).Encode(out); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(*path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(path string) error {
	cfg, err := sourceadapter.Load(path)
	if err != nil {
		return err
	}
	adapter, err := sourceadapter.New(cfg.Views)
	if err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(cfg.Address)
	if err != nil {
		return err
	}
	if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return errors.New("source adapter requires TLS except on an explicit loopback listener")
		}
	}
	server := &http.Server{Addr: cfg.Address, Handler: adapter.Handler(cfg.TokenFile), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		if cfg.TLSCertFile != "" {
			done <- server.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			done <- server.ListenAndServe()
		}
	}()
	select {
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		return server.Shutdown(shutdown)
	}
}
