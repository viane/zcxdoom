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

	a := Decide(context.Background(), system1.New(srv.URL), perception.State{}, 1)
	if a != Fire {
		t.Errorf("Decide = %v, want Fire (what the mock server answered)", a)
	}
}

func TestDecideFallsBackOnSystem1Error(t *testing.T) {
	s1 := system1.New("http://127.0.0.1:1") // unreachable
	a := Decide(context.Background(), s1, perception.State{}, 1)
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

	a := Decide(context.Background(), system1.New(srv.URL), perception.State{}, 1)
	if !isOneOf(a, Forward, Fire) {
		t.Errorf("Decide with an invalid choice = %v, want a fallback action", a)
	}
}

func TestDecideNilClientUsesFallback(t *testing.T) {
	a := Decide(context.Background(), nil, perception.State{}, 1)
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

// The reflex exists because a dead player only responds to use; see
// ReviveInterval. These lock in that it fires on schedule and that it
// overrides System 1 rather than merely competing with it.

func TestDecidePressesUseOnScheduleEvenWhenSystem1SaysOtherwise(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"answers": map[string]any{questionID: map[string]any{"type": "choice", "choice": string(Forward)}},
		})
	}))
	defer srv.Close()

	// A live, moving player: nothing here looks stuck, which is exactly the
	// state a dead player's screen still produces.
	state := perception.State{DiffScore: 0.09, FramesSinceMove: 0}
	if a := Decide(context.Background(), system1.New(srv.URL), state, ReviveInterval); a != Use {
		t.Errorf("Decide on the revive tick = %v, want Use even though System 1 answered forward", a)
	}
	if a := Decide(context.Background(), system1.New(srv.URL), state, ReviveInterval+1); a != Forward {
		t.Errorf("Decide off the revive tick = %v, want System 1's answer (Forward)", a)
	}
}

func TestReflexFiresAtLeastOncePerReviveInterval(t *testing.T) {
	// Whatever the cadence, a dead player must not be able to go a long
	// stretch without a use press.
	state := perception.State{DiffScore: 0.09}
	for start := 1; start <= ReviveInterval; start++ {
		fired := false
		for tick := start; tick < start+ReviveInterval; tick++ {
			if a, ok := reflex(state, tick); ok && a == Use {
				fired = true
				break
			}
		}
		if !fired {
			t.Errorf("no use press in %d ticks starting at %d", ReviveInterval, start)
		}
	}
}

func TestReflexRevivesFasterWhenScreenIsFrozen(t *testing.T) {
	frozen := perception.State{FramesSinceMove: StuckReviveAfter}
	fired := 0
	for tick := 1; tick <= ReviveInterval; tick++ {
		if a, ok := reflex(frozen, tick); ok && a == Use {
			fired++
		}
	}
	if want := ReviveInterval / StuckReviveInterval; fired < want {
		t.Errorf("use presses while frozen = %d over %d ticks, want at least %d", fired, ReviveInterval, want)
	}
}

func TestReflexStaysOutOfTheWayOnTickZero(t *testing.T) {
	if _, ok := reflex(perception.State{}, 0); ok {
		t.Error("reflex fired on tick 0; the first decision should be the policy's, not a use press")
	}
}
