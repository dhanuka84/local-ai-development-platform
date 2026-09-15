// Package httpserver owns HTTP transport, authentication, and request logging.
// Application dependencies remain owned by the calling command.
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
	"github.com/dhanuka84/hybrid-ai-platform/internal/identity"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type DependencyCheck func(r *http.Request) map[string]string
type Authenticator interface {
	AuthenticateToken(context.Context, string) (domain.Principal, error)
}

// Serve owns listener and server, joining the serving goroutine and graceful
// shutdown before returning. On timeout it closes remaining connections and
// returns an error; handlers must observe their request's cancellation.
func Serve(ctx context.Context, server *http.Server, listener net.Listener, shutdownTimeout time.Duration) (err error) {
	defer func() { err = errors.Join(err, server.Close()) }()
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()

	select {
	case err = <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		// Request cancellation starts shutdown; draining needs its own deadline.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		shutdownErr := server.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			shutdownErr = errors.Join(shutdownErr, server.Close())
		}
		serveErr := <-served
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		return errors.Join(serveErr, shutdownErr)
	}
}

func New(address, authMode string, authenticator Authenticator, localPrincipal domain.Principal, mcpServer *mcp.Server, logger *slog.Logger, check DependencyCheck) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		dependencies := check(r)
		status := http.StatusOK
		for _, value := range dependencies {
			if value != "ok" {
				status = http.StatusServiceUnavailable
				break
			}
		}
		writeJSON(w, status, map[string]any{"dependencies": dependencies})
	})
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpServer }, &mcp.StreamableHTTPOptions{
		Logger: logger, Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 4 << 20,
		PropagateRequestCancellation: true,
	})
	protected := http.NewCrossOriginProtection().Handler(mcpHandler)
	mux.Handle("/mcp", authenticate(authMode, authenticator, localPrincipal, protected))
	return &http.Server{
		Addr: address, Handler: requestLog(logger, mux),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 5 * time.Minute,
		WriteTimeout: 20 * time.Minute, IdleTimeout: 2 * time.Minute,
	}
}

func authenticate(mode string, authenticator Authenticator, localPrincipal domain.Principal, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deny := func() {
			if auditor, ok := authenticator.(interface{ AuditAuthenticationFailure(context.Context) error }); ok {
				if err := auditor.AuditAuthenticationFailure(r.Context()); err != nil {
					writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "authentication audit unavailable"})
					return
				}
			}
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}
		if mode == "none" {
			if localPrincipal.ID != "" {
				r = r.WithContext(identity.WithPrincipal(r.Context(), localPrincipal))
			}
			next.ServeHTTP(w, r)
			return
		}
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") || authenticator == nil {
			deny()
			return
		}
		principal, err := authenticator.AuthenticateToken(r.Context(), strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			deny()
			return
		}
		r = r.WithContext(identity.WithPrincipal(r.Context(), principal))
		next.ServeHTTP(w, r)
	})
}

func requestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		// Headers are committed, so the remaining action is to report the failure.
		slog.Error("write HTTP response", "error", err)
	}
}
