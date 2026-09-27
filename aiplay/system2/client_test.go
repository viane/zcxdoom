package system2

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testFrame() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 20, B: 30, A: 255})
		}
	}
	return img
}

func TestTacticSendsImageAndPromptAndReturnsText(t *testing.T) {
	var gotReq generateRequest
	var gotPath, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-goog-api-key")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("server: decode request: %v", err)
		}
		json.NewEncoder(w).Encode(generateResponse{Candidates: []struct {
			Content struct {
				Parts []part `json:"parts"`
			} `json:"content"`
		}{{Content: struct {
			Parts []part `json:"parts"`
		}{Parts: []part{{Text: "push forward, the room ahead looks clear"}}}}}})
	}))
	defer srv.Close()

	c := New("test-key", "gemini-test-model")
	c.BaseURL = srv.URL

	tactic, err := c.Tactic(context.Background(), testFrame(), "just started")
	if err != nil {
		t.Fatalf("Tactic: %v", err)
	}
	if tactic != "push forward, the room ahead looks clear" {
		t.Errorf("tactic = %q, want the server's text", tactic)
	}
	if gotKey != "test-key" {
		t.Errorf("x-goog-api-key = %q, want test-key", gotKey)
	}
	if gotPath != "/models/gemini-test-model:generateContent" {
		t.Errorf("path = %q, want /models/gemini-test-model:generateContent", gotPath)
	}
	if len(gotReq.Contents) != 1 || len(gotReq.Contents[0].Parts) != 2 {
		t.Fatalf("request parts = %+v, want [text, inline_data]", gotReq.Contents)
	}
	if !strings.Contains(gotReq.Contents[0].Parts[0].Text, "just started") {
		t.Errorf("prompt text missing recentHistory: %q", gotReq.Contents[0].Parts[0].Text)
	}
	img := gotReq.Contents[0].Parts[1].InlineData
	if img == nil || img.MimeType != "image/jpeg" || img.Data == "" {
		t.Fatalf("inline_data = %+v, want a non-empty image/jpeg payload", img)
	}
	if _, err := base64.StdEncoding.DecodeString(img.Data); err != nil {
		t.Errorf("inline_data.Data isn't valid base64: %v", err)
	}
}

func TestTacticReturnsErrorOnBlockedPrompt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"promptFeedback": map[string]any{"blockReason": "SAFETY"},
		})
	}))
	defer srv.Close()

	c := New("k", "")
	c.BaseURL = srv.URL
	_, err := c.Tactic(context.Background(), testFrame(), "")
	if err == nil || !strings.Contains(err.Error(), "SAFETY") {
		t.Fatalf("err = %v, want an error mentioning the block reason", err)
	}
}

func TestTacticReturnsErrorOnEmptyCandidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(generateResponse{})
	}))
	defer srv.Close()

	c := New("k", "")
	c.BaseURL = srv.URL
	_, err := c.Tactic(context.Background(), testFrame(), "")
	if err == nil {
		t.Fatal("Tactic succeeded with zero candidates, want an error")
	}
}

func TestNewDefaultsModel(t *testing.T) {
	c := New("k", "")
	if c.Model != "gemini-2.5-flash" {
		t.Errorf("default Model = %q, want gemini-2.5-flash", c.Model)
	}
}
