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

// ReviveInterval is how often Decide presses "use" no matter what System
// 1 would rather do, counted in decision ticks.
//
// A dead player in Doom ignores every input except use: P_DeathThink
// (dockerdoom/trunk/src/p_user.c) sets PST_REBORN only on BT_USE, and
// key_use is space (g_game.c). Nothing else -- moving, turning, firing --
// can end the death state, so a policy that never presses use stays dead
// until someone restarts the container.
//
// This is deliberately a fixed heartbeat rather than a "have we stopped
// moving?" check. Being dead does not reliably look like a frozen screen:
// the damage flash fades over several tics and whatever killed you is
// usually still moving in view, so DiffScore stays well above
// StuckThreshold and FramesSinceMove sits at 0. Observed directly on a
// real run -- HUD reading HEALTH 0% while the log showed diff=0.089
// stuck=0 -- so a stasis-based detector would never have fired.
//
// Pressing use on a live player is harmless and mildly useful: it is the
// open-door/activate-switch key, which the fallback policy would
// otherwise never send at all.
const ReviveInterval = 16

// StuckReviveInterval is the faster use cadence once the screen really
// has stopped changing (FramesSinceMove >= StuckReviveAfter), which is
// the other way to get wedged: standing against a door that only use
// opens. Recovering from that is worth more than one tick's chosen action.
const StuckReviveInterval = 3

// StuckReviveAfter is how many unchanged frames count as wedged.
const StuckReviveAfter = 9

// instructions tells System 1 what the state's fields mean and how they
// bear on the decision.
//
// Spelling that out is not decoration, and it is also not sufficient.
// Both halves of that were measured against a live Kev on qwen3.5:9b:
//
//   - With only "what should the player do next?", every state returned
//     "forward" -- health 100, health 12 and ammo 0 alike, with identical
//     confidence. 17 of 17 live decisions were "forward".
//   - Naming the fields and their consequences does move the answer:
//     under combat-leaning wording, ammo 0 came back as "back" rather
//     than a shot that could not have fired.
//   - But the model then collapses onto whichever action the wording
//     leans towards: that same combat wording produced 11 "fire" and no
//     "forward" in live play, standing still and shooting nothing.
//
// The reason is that the state still cannot say whether an enemy is on
// screen, so nothing in it distinguishes "shoot" from "explore" and the
// model falls back on tone. This wording therefore leans the safer way --
// keep exploring -- and is explicit that an enemy should not be assumed.
// The health and ammo clauses are here because they are correct guidance
// and the values are now real (see perception's HUD reader); they will
// start earning their place once perception can also report an enemy
// being visible.
const instructions = "You are playing Doom, exploring a level. Your goal is to make progress through " +
	"the level, so moving is the default; the state does not tell you whether an enemy is on screen, " +
	"so do not assume one is. diff_score is how much the screen changed since the last frame and " +
	"frames_since_move counts frames where nothing changed: when frames_since_move is above a few, " +
	"you are stuck against a wall and should turn. health is 0-100 (0 means dead) and ammo is shots " +
	"left; when health is low prefer retreating, and never fire with ammo 0. " +
	"What should the player do next?"

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
func Decide(ctx context.Context, s1 *system1.Client, state perception.State, tick int) Action {
	if a, ok := reflex(state, tick); ok {
		return a
	}
	if s1 != nil {
		answers, err := s1.Ask(ctx, state, map[string]system1.Question{
			questionID: {
				Type:         "choice",
				Instructions: instructions,
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

// reflex returns the action that has to happen on this tick regardless of
// what System 1 or the fallback would choose, and whether there is one.
// It is the one piece of policy that cannot be delegated: see
// ReviveInterval for why pressing use has to be guaranteed rather than
// merely likely.
func reflex(state perception.State, tick int) (Action, bool) {
	if tick <= 0 {
		return "", false
	}
	if state.FramesSinceMove >= StuckReviveAfter {
		return Use, tick%StuckReviveInterval == 0
	}
	return Use, tick%ReviveInterval == 0
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
