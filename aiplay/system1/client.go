// Package system1 talks to a System-1-style fast decision server: typed
// choice/score/bool questions in, typed answers with probabilities out, no
// free-text generation. TypeSafe's hosted Jev is one such server; Kev
// (github.com/jaredpalmer/kev) is a self-hostable, API-compatible one, and
// the point of this package is that either works through the same client.
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
type Question struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"` // "choice", "score", or "bool"
	Prompt  string   `json:"prompt"`
	Choices []string `json:"choices,omitempty"` // required when Kind == "choice"
}

// Answer is the typed response to one Question.
type Answer struct {
	ID            string             `json:"id"`
	Choice        string             `json:"choice,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Bool          bool               `json:"bool,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// Client is a System-1 HTTP client.
//
// The exact request/response field names in request/response below are
// this project's best-effort mapping onto the publicly described
// "/v1/systemone" contract (state + typed questions in, typed answers
// out) -- the server's own OpenAPI schema wasn't available to check this
// against directly while building this. Treat this wire format as
// provisional: before depending on this against a real Kev/Jev instance,
// verify request/response here against that server's actual docs and
// adjust just those two types if they differ. Everything else in aiplay
// depends only on Client.Ask's Go signature, not this wire format, so a
// correction stays contained to this one file.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// New returns a Client for the System-1 server at baseURL (e.g.
// "http://localhost:8000" for a local Kev/Ollama instance, or TypeSafe's
// hosted endpoint).
func New(baseURL string) *Client {
	return &Client{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}
}

type wireRequest struct {
	State     any        `json:"state"`
	Questions []Question `json:"questions"`
}

type wireResponse struct {
	Answers []Answer `json:"answers"`
}

// Ask sends the current shared state plus a batch of typed questions and
// returns the model's typed answers.
func (c *Client) Ask(ctx context.Context, state any, questions []Question) ([]Answer, error) {
	body, err := json.Marshal(wireRequest{State: state, Questions: questions})
	if err != nil {
		return nil, fmt.Errorf("system1: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("system1: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

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
