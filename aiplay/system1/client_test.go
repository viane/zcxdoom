package system1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// There's no live Kev/Jev instance in CI or this sandbox, so these tests
// stand up a fake server matching this client's assumed wire format. They
// verify the Client <-> server round trip this code controls; they cannot
// verify that a real Kev server actually speaks this exact format -- see
// the wire format caveat on Client.

func TestAskSendsStateAndQuestionsAndParsesAnswers(t *testing.T) {
	var gotReq wireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" {
			t.Errorf("request path = %q, want /v1/systemone", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("server: decode request: %v", err)
		}
		json.NewEncoder(w).Encode(wireResponse{Answers: []Answer{
			{ID: "next_action", Choice: "forward", Probabilities: map[string]float64{"forward": 0.9, "fire": 0.1}},
		}})
	}))
	defer srv.Close()

	c := New(srv.URL)
	state := map[string]any{"diff_score": 0.02}
	questions := []Question{{ID: "next_action", Kind: "choice", Prompt: "what next?", Choices: []string{"forward", "fire"}}}

	answers, err := c.Ask(context.Background(), state, questions)
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if len(answers) != 1 || answers[0].Choice != "forward" {
		t.Fatalf("answers = %+v, want one answer with Choice=forward", answers)
	}
	if len(gotReq.Questions) != 1 || gotReq.Questions[0].ID != "next_action" {
		t.Fatalf("server saw questions = %+v, want the one we sent", gotReq.Questions)
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
