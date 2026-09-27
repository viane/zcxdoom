// Package system2 is the slow, deliberate half of aiplay: an occasional
// call to a real multimodal LLM (Gemini, by default) that looks at an
// actual screenshot and sets overall tactics, rather than reacting to
// every frame the way System 1 does.
package system2

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"time"
)

// Client talks to the Gemini API's generateContent endpoint.
// https://ai.google.dev/api/generate-content documents the current
// request/response shape and available model names.
type Client struct {
	APIKey     string
	Model      string // e.g. "gemini-2.5-flash"
	BaseURL    string // default "https://generativelanguage.googleapis.com/v1beta"
	HTTPClient *http.Client
}

// New returns a Client for the given API key and model. An empty model
// defaults to "gemini-2.5-flash" -- check
// https://ai.google.dev/gemini-api/docs/models for the current model
// names if that default stops working.
func New(apiKey, model string) *Client {
	if model == "" {
		model = "gemini-2.5-flash"
	}
	return &Client{
		APIKey:     apiKey,
		Model:      model,
		BaseURL:    "https://generativelanguage.googleapis.com/v1beta",
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type part struct {
	Text       string      `json:"text,omitempty"`
	InlineData *inlineData `json:"inline_data,omitempty"`
}

type inlineData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

type content struct {
	Parts []part `json:"parts"`
}

type generateRequest struct {
	Contents []content `json:"contents"`
}

type generateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []part `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	PromptFeedback *struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
}

const promptTemplate = `You are directing a fast, simple reflex agent playing classic DOOM.
Look at this screenshot and the recent history below, then reply with ONE short
tactical instruction (one sentence, imperative -- e.g. "retreat down the corridor
behind you and look for ammo" or "push forward, the room ahead looks clear").
Do not describe the image, just give the instruction.

Recent history: %s`

// Tactic sends a screenshot plus a short text history to Gemini and returns
// its one-sentence tactical instruction, meant to steer System 1 until the
// next call.
func (c *Client) Tactic(ctx context.Context, frame image.Image, recentHistory string) (string, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, frame, &jpeg.Options{Quality: 85}); err != nil {
		return "", fmt.Errorf("system2: encode frame: %w", err)
	}

	reqBody := generateRequest{Contents: []content{{Parts: []part{
		{Text: fmt.Sprintf(promptTemplate, recentHistory)},
		{InlineData: &inlineData{MimeType: "image/jpeg", Data: base64.StdEncoding.EncodeToString(buf.Bytes())}},
	}}}}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("system2: encode request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:generateContent", c.BaseURL, c.Model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("system2: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("system2: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("system2: server returned %s: %s", resp.Status, b)
	}

	var out generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("system2: decode response: %w", err)
	}
	if out.PromptFeedback != nil && out.PromptFeedback.BlockReason != "" {
		return "", fmt.Errorf("system2: blocked: %s", out.PromptFeedback.BlockReason)
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("system2: empty response")
	}
	return out.Candidates[0].Content.Parts[0].Text, nil
}
