package doompolicy

import (
	"context"
	"os"
	"testing"
	"time"

	"aiplay/perception"
	"aiplay/system1"
)

// What a live model makes of the instructions, for the states that
// matter. It is skipped unless a server is named, because it needs a real
// one -- there is no point asserting this against a stub, since what is
// being checked is exactly the thing a stub cannot have:
//
//	KEV_URL=http://localhost:3000 go test ./doompolicy -run Live -v
//
// This is a probe, not a gate. It prints what the model answered and only
// fails if the server does not answer at all, because the answers are a
// property of whichever model is behind it, not of this package -- and
// because the useful output is the shape of the whole table rather than
// any one row. Two readings worth having found this way:
//
//   - Phrasing moves the answer wholesale. Under combat-leaning wording
//     an earlier version of these instructions returned "fire" to every
//     state including ammo 0; under exploration-leaning wording, 17 of 17
//     live decisions were "forward".
//   - The model weighs some fields and not others. With the current
//     wording, qwen3.5:9b answers "fire" to a monster ahead, "back" to a
//     monster with no ammo left, and "turn_left" to something in the way
//     -- but answers "forward" to all four values of open_direction
//     alike. That measurement is why steering toward the open direction
//     is done by aiplay rather than asked for here; see steerToward in
//     cmd/aiplay.
func TestLiveSystem1AnswersEachSituation(t *testing.T) {
	url := os.Getenv("KEV_URL")
	if url == "" {
		t.Skip("set KEV_URL to a running Kev/Jev server to probe what it answers")
	}
	s1 := system1.New(url)

	ptr := func(v int) *int { return &v }
	yes, no := true, false
	for _, tc := range []struct {
		name  string
		state perception.State
	}{
		{"monster on the left", perception.State{Health: ptr(80), Ammo: ptr(40), MotionInView: &yes, MotionDirection: perception.MotionLeft, OpenDirection: perception.DirAhead}},
		{"monster straight ahead", perception.State{Health: ptr(80), Ammo: ptr(40), MotionInView: &yes, MotionDirection: perception.MotionAhead, OpenDirection: perception.DirAhead}},
		{"hurt, nothing in view", perception.State{Health: ptr(40), Ammo: ptr(40), MotionInView: &no, TakingDamage: true, OpenDirection: perception.DirAhead}},
		{"quiet, room ahead", perception.State{Health: ptr(100), Ammo: ptr(50), MotionInView: &no, OpenDirection: perception.DirAhead}},
		{"quiet, room to the left", perception.State{Health: ptr(100), Ammo: ptr(50), MotionInView: &no, OpenDirection: perception.DirLeft}},
		{"quiet, room to the right", perception.State{Health: ptr(100), Ammo: ptr(50), MotionInView: &no, OpenDirection: perception.DirRight}},
		{"quiet, room behind", perception.State{Health: ptr(100), Ammo: ptr(50), MotionInView: &no, OpenDirection: perception.DirBehind}},
		{"walked into something", perception.State{Health: ptr(100), Ammo: ptr(50), MotionInView: &no, WallAhead: &yes, OpenDirection: perception.DirLeft}},
		{"no ammo, monster ahead", perception.State{Health: ptr(30), Ammo: ptr(0), MotionInView: &yes, MotionDirection: perception.MotionAhead, OpenDirection: perception.DirBehind}},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		answers, err := s1.Ask(ctx, describeState(tc.state), map[string]system1.Question{
			questionID: {Type: "choice", Instructions: instructions, Criteria: criteria},
		})
		cancel()
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		a, ok := answers[questionID]
		if !ok {
			t.Fatalf("%s: no answer for %q", tc.name, questionID)
		}
		t.Logf("%-24s -> %-10s (confidence %.2f)", tc.name, a.Choice, a.Confidence)
		if !isValidChoice(a.Choice) {
			t.Errorf("%s: answered %q, which is not one of the offered actions", tc.name, a.Choice)
		}
	}
}
