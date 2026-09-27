package system1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// These tests stand up a fake server matching the real Kev wire format
// (verified against a live github.com/arjun988/kev server's /openapi.json
// and a real /v1/systemone call using its mock backend -- see client.go).
// They verify the Client <-> server round trip this code controls.

func TestAskSendsStateAndQuestionsAndParsesAnswers(t *testing.T) {
	var gotReq wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" {
			t.Errorf("request path = %q, want /v1/systemone", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("server: decode request: %v", err)
		}
		json.NewEncoder(w).Encode(wireResponse{Answers: map[string]Answer{
			"next_action": {Type: "choice", Choice: "forward", Confidence: 0.4,
				Probabilities: map[string]float64{"forward": 0.9, "fire": 0.1}},
		}})
	}))
	defer srv.Close()

	c := New(srv.URL)
	state := map[string]any{"diff_score": 0.02}
	questions := map[string]Question{
		"next_action": {Type: "choice", Instructions: "what next?", Criteria: map[string]string{"forward": "", "fire": ""}},
	}

	answers, err := c.Ask(context.Background(), state, questions)
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if a, ok := answers["next_action"]; !ok || a.Choice != "forward" {
		t.Fatalf("answers = %+v, want next_action.Choice=forward", answers)
	}
	if gotReq.Model != "kev-latest" {
		t.Errorf("request model = %q, want default kev-latest", gotReq.Model)
	}
	if q, ok := gotReq.Questions["next_action"]; !ok || q.Type != "choice" {
		t.Fatalf("server saw questions = %+v, want the one we sent", gotReq.Questions)
	}
}

func TestAskSendsBearerTokenWhenAPIKeySet(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(wireResponse{Answers: map[string]Answer{}})
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.APIKey = "secret"
	if _, err := c.Ask(context.Background(), map[string]any{}, nil); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer secret")
	}
}

func TestAskReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("boom"))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.Ask(context.Background(), map[string]any{}, nil)
	if err == nil {
		t.Fatal("Ask succeeded against a 500 response, want an error")
	}
}

func TestAskReturnsErrorWhenServerUnreachable(t *testing.T) {
	c := New("http://127.0.0.1:1") // nothing listens here
	_, err := c.Ask(context.Background(), map[string]any{}, nil)
	if err == nil {
		t.Fatal("Ask succeeded against an unreachable server, want an error")
	}
}
