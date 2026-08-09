// Package gateway is the only local-ai component bound to a network-facing
// address. It authenticates callers against local-ai's own key store, then
// reverse-proxies everything to the loopback llama-server router untouched
// — no request/response translation, since llama-server already speaks both
// OpenAI's and Anthropic's APIs natively.
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"local-ai/internal/config"
	"local-ai/internal/keys"
)

// publicPaths mirror llama-server's own behavior: GET /v1/models and
// /health are not gated by --api-key, so the gateway doesn't require a
// local-ai key for them either (confirmed live against a real --api-key).
var publicPaths = map[string]bool{
	"/v1/models": true,
	"/health":    true,
}

// Gateway is the network-facing HTTP server.
type Gateway struct {
	store *keys.Store
	proxy *httputil.ReverseProxy
	srv   *http.Server
}

// New builds a Gateway proxying to cfg's internal llama-server, authorizing
// callers against store.
func New(cfg *config.Config, store *keys.Store) (*Gateway, error) {
	target, err := url.Parse(fmt.Sprintf("http://%s:%d", cfg.InternalHost, cfg.InternalPort))
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1 // stream SSE immediately, don't buffer
	baseDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		baseDirector(r)
		// Always forward with llama-server's own internal secret; the
		// caller's local-ai key was already checked in handle and never
		// reaches llama-server.
		r.Header.Set("Authorization", "Bearer "+cfg.InternalAPIKey)
		r.Header.Del("X-Api-Key")
	}
	proxy.ErrorLog = log.Default()

	g := &Gateway{store: store, proxy: proxy}

	mux := http.NewServeMux()
	mux.HandleFunc("/", g.handle)

	g.srv = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.GatewayHost, cfg.GatewayPort),
		Handler: mux,
	}
	return g, nil
}

func (g *Gateway) handle(w http.ResponseWriter, r *http.Request) {
	if !publicPaths[r.URL.Path] {
		key := extractKey(r)
		if key == "" || !g.store.Verify(key) {
			writeUnauthorized(w)
			return
		}
	}
	g.proxy.ServeHTTP(w, r)
}

// extractKey reads a caller's key from either OpenAI-style
// "Authorization: Bearer <key>" or Anthropic-style "x-api-key: <key>" —
// llama-server itself accepts both conventions against a single secret, so
// the gateway mirrors that rather than requiring one specific header.
func extractKey(r *http.Request) string {
	if v := r.Header.Get("X-Api-Key"); v != "" {
		return v
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"message": "invalid or missing API key"},
	})
}

// Run starts the gateway and blocks until ctx is cancelled, at which point
// it shuts down gracefully.
func (g *Gateway) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- g.srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return g.srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}
