// Package llamaclient is a minimal HTTP client for talking to the loopback
// llama-server router instance local-ai supervises. GET /v1/models is
// confirmed to be public (not gated by --api-key), so no auth is needed here.
package llamaclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client talks to a llama-server router instance over HTTP.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New creates a Client for the given host:port.
func New(host string, port int) *Client {
	return &Client{
		BaseURL: fmt.Sprintf("http://%s:%d", host, port),
		HTTP:    &http.Client{Timeout: 5 * time.Second},
	}
}

// ModelMeta is populated once a model has actually been loaded.
type ModelMeta struct {
	NCtx      int64  `json:"n_ctx"`
	NCtxTrain int64  `json:"n_ctx_train"`
	NEmbd     int64  `json:"n_embd"`
	NParams   int64  `json:"n_params"`
	Size      int64  `json:"size"`
	FType     string `json:"ftype"`
}

// ModelStatus is the router's live state for one model.
type ModelStatus struct {
	// Value is one of "unloaded", "loading", "loaded", "sleeping" (confirmed
	// live; not the "ready" value an earlier design note assumed).
	Value string `json:"value"`
}

// Model is one entry from GET /v1/models. Meta is only populated once the
// model has actually been loaded at least once (confirmed live: it's a
// top-level field on the model object, not nested under status).
type Model struct {
	ID      string      `json:"id"`
	Aliases []string    `json:"aliases"`
	Status  ModelStatus `json:"status"`
	Meta    *ModelMeta  `json:"meta,omitempty"`
}

type modelsResponse struct {
	Data []Model `json:"data"`
}

// ListModels returns the router's current view of available models and
// their load state.
func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /v1/models: unexpected status %s", resp.Status)
	}

	var out modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding /v1/models response: %w", err)
	}
	return out.Data, nil
}

// Healthy reports whether the router is reachable at all.
func (c *Client) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
