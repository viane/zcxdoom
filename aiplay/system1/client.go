// Package system1 talks to a System-1-style fast decision server: typed
// choice/score/noul questions over one shared state, answered with typed
// values and calibrated probabilities in a single request, no free-text
// generation. TypeSafe's hosted Jev is one such server; Kev
// (github.com/jaredpalmer/kev, github.com/arjun988/kev) is a self-hostable,
// API-compatible one, and the point of this package is that either works
// through the same client.
//
// The wire format below was verified directly against a running Kev
// server (its /openapi.json and a live /v1/systemone call), not just
// documentation -- see the request/response comments for the exact shape.
package system1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Question is one typed decision question posed over a shared state blob.
//
// Criteria's shape depends on Type:
//   - "choice": map[string]string, option name -> description (description
//     may be empty). 1-255 options.
//   - "score":  []string, level descriptions ordered lowest to highest.
//   - "noul":   optional map[string]string with "true"/"false" descriptions,
//     or nil.
type Question struct {
	Type         string `json:"type"` // "choice", "score", or "noul"
	Instructions string `json:"instructions,omitempty"`
	Criteria     any    `json:"criteria,omitempty"`
}

// Answer is the typed response to one Question. Which fields are set
// depends on Type, matching Question.Type: "choice" sets Choice and
// Probabilities; "noul" sets Noul; "score" sets Score, Legend, and
// Probabilities. Confidence is set for "choice" and "score".
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Noul          float64            `json:"noul,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

// Client is a System-1 HTTP client.
type Client struct {
	BaseURL    string
	Model      string // sent as the request's "model" field; default "kev-latest"
	APIKey     string // optional: sent as "Authorization: Bearer <key>" if set (matches KEV_API_KEY)
	HTTPClient *http.Client
}

// New returns a Client for the System-1 server at baseURL (e.g.
// "http://localhost:3000" for a local Kev instance, or TypeSafe's hosted
// endpoint).
func New(baseURL string) *Client {
	return &Client{
		BaseURL:    baseURL,
		Model:      "kev-latest",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}
}

type wireRequest struct {
	Model     string              `json:"model,omitempty"`
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`
}

type wireResponse struct {
	Model   string            `json:"model,omitempty"`
	Answers map[string]Answer `json:"answers"`
}

// Ask sends the current shared state plus a batch of typed questions
// (keyed by an id you choose) and returns the model's typed answers, keyed
// by the same ids.
func (c *Client) Ask(ctx context.Context, state any, questions map[string]Question) (map[string]Answer, error) {
	body, err := json.Marshal(wireRequest{Model: c.Model, State: state, Questions: questions})
	if err != nil {
		return nil, fmt.Errorf("system1: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("system1: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("system1: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("system1: server returned %s: %s", resp.Status, b)
	}

	var out wireResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("system1: decode response: %w", err)
	}
	return out.Answers, nil
}
