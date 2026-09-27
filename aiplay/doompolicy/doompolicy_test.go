package doompolicy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"aiplay/perception"
	"aiplay/system1"
)

func isOneOf(a Action, options ...Action) bool {
	for _, o := range options {
		if a == o {
			return true
		}
	}
	return false
}

func TestDecideUsesSystem1AnswerWhenValid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"answers": map[string]any{questionID: map[string]any{"type": "choice", "choice": string(Fire)}},
		})
	}))
	defer srv.Close()

	a := Decide(context.Background(), system1.New(srv.URL), perception.State{})
	if a != Fire {
		t.Errorf("Decide = %v, want Fire (what the mock server answered)", a)
	}
}

func TestDecideFallsBackOnSystem1Error(t *testing.T) {
	s1 := system1.New("http://127.0.0.1:1") // unreachable
	a := Decide(context.Background(), s1, perception.State{})
	if !isOneOf(a, Forward, Fire) {
		t.Errorf("Decide with unreachable System1 = %v, want a fallback action (Forward or Fire for a non-stuck state)", a)
	}
}

func TestDecideFallsBackOnInvalidChoice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"answers": map[string]any{questionID: map[string]any{"type": "choice", "choice": "moonwalk"}},
		})
	}))
	defer srv.Close()

	a := Decide(context.Background(), system1.New(srv.URL), perception.State{})
	if !isOneOf(a, Forward, Fire) {
		t.Errorf("Decide with an invalid choice = %v, want a fallback action", a)
	}
}

func TestDecideNilClientUsesFallback(t *testing.T) {
	a := Decide(context.Background(), nil, perception.State{})
	if !isOneOf(a, Forward, Fire) {
		t.Errorf("Decide with nil System1 client = %v, want a fallback action", a)
	}
}

func TestFallbackTurnsWhenStuck(t *testing.T) {
	a := fallback(perception.State{FramesSinceMove: 5})
	if !isOneOf(a, TurnLeft, TurnRight) {
		t.Errorf("fallback while stuck = %v, want TurnLeft or TurnRight", a)
	}
}

func TestActionKeysymMapping(t *testing.T) {
	want := map[Action]string{
		Forward: "up", Back: "down", TurnLeft: "left", TurnRight: "right",
		Fire: "ctrl", Use: "space",
	}
	for action, keysym := range want {
		if got := action.Keysym(); got != keysym {
			t.Errorf("%s.Keysym() = %q, want %q", action, got, keysym)
		}
	}
}
