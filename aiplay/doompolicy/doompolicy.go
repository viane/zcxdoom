// Package doompolicy is the Doom-specific glue between perception's
// structured state and system1's generic typed-question protocol: which
// questions to ask, and which key each possible answer maps to. Keeping it
// out of system1 keeps that package reusable for any other System-1 task.
package doompolicy

import (
	"context"
	"math/rand"

	"aiplay/perception"
	"aiplay/system1"
)

// Action is a single game input, named after the psdoom keyboard control
// it corresponds to.
type Action string

const (
	Forward   Action = "forward"
	Back      Action = "back"
	TurnLeft  Action = "turn_left"
	TurnRight Action = "turn_right"
	Fire      Action = "fire"
	Use       Action = "use"
)

// Keysym is the rfb.KeysymFor name Action should be sent as, matching
// psdoom's default keyboard bindings (arrow keys to move/turn, Ctrl to
// fire, Space to use).
func (a Action) Keysym() string {
	switch a {
	case Forward:
		return "up"
	case Back:
		return "down"
	case TurnLeft:
		return "left"
	case TurnRight:
		return "right"
	case Fire:
		return "ctrl"
	case Use:
		return "space"
	default:
		return "up"
	}
}

const questionID = "next_action"

// criteria describes each option to System 1, per the "choice" question
// type's contract (option name -> description).
var criteria = map[string]string{
	string(Forward):   "move forward, toward whatever is in view",
	string(Back):      "move backward, away from whatever is in view",
	string(TurnLeft):  "turn left in place without moving forward or back",
	string(TurnRight): "turn right in place without moving forward or back",
	string(Fire):      "fire the current weapon",
	string(Use):       "open a door or activate a switch directly ahead",
}

// Decide asks System 1 what to do next given the current perceived state.
// If s1 is nil, or the call fails or comes back without a usable answer,
// Decide falls back to a simple, self-contained heuristic instead of
// stalling the loop -- this keeps aiplay runnable for development and
// testing without a live Kev/Jev instance, and keeps it playing through a
// transient System-1 outage in production.
func Decide(ctx context.Context, s1 *system1.Client, state perception.State) Action {
	if s1 != nil {
		answers, err := s1.Ask(ctx, state, map[string]system1.Question{
			questionID: {
				Type:         "choice",
				Instructions: "Given the current game state, what should the player do next?",
				Criteria:     criteria,
			},
		})
		if err == nil {
			if a, ok := answers[questionID]; ok && isValidChoice(a.Choice) {
				return Action(a.Choice)
			}
		}
	}
	return fallback(state)
}

func isValidChoice(choice string) bool {
	_, ok := criteria[choice]
	return ok
}

// fallback is a deliberately simple, dependency-free policy: move forward
// most of the time, turn to break out of being stuck, and fire
// periodically. It has no notion of enemies or health -- perception
// doesn't extract that yet (see perception's package doc) -- so it's meant
// to keep the game moving, not to play well.
func fallback(state perception.State) Action {
	switch {
	case state.FramesSinceMove >= 3:
		if rand.Intn(2) == 0 {
			return TurnLeft
		}
		return TurnRight
	case rand.Intn(10) == 0:
		return Fire
	default:
		return Forward
	}
}
