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

	// Wait sends no key at all. It is never offered to System 1 as a
	// choice -- letting the model decide to do nothing invites it to
	// stall -- and exists only so aiplay can hold the view still for a
	// motion probe. See ProbeInterval.
	Wait Action = "wait"
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
	case Wait:
		return "" // no key
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

// Monsters are the one thing in a Doom frame that moves on its own, and
// that is the only handle there is on them: they share essentially the
// whole palette with the level's own walls and floors, so no colour or
// sprite-colour test can separate the two. Measured against this repo's
// own WAD -- across every light level, the set of palette entries used by
// monsters but by no wall, flat, pickup or weapon sprite in E1M1 is
// empty, and at full brightness it is a single entry.
//
// But "it moved" only means something while the player's own view is
// still. During ordinary play every pixel moves, so aiplay stops for
// ProbeStillTicks consecutive ticks every ProbeInterval, which makes the
// frames either side of the last one directly comparable. Two ticks
// rather than one so that both frames are equally still: the player's
// weapon bobs while walking, and a frame captured mid-bob would register
// as motion on its own.
const (
	ProbeInterval   = 12
	ProbeStillTicks = 2
)

// IsProbeTick reports whether the action for this tick should be Wait to
// hold the view still for a motion probe.
func IsProbeTick(tick int) bool {
	if tick <= 0 {
		return false
	}
	return tick%ProbeInterval < ProbeStillTicks
}

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
const instructions = "You are playing Doom, exploring a level. The state describes your situation. " +
	"something_moving_in_view is true when something alive is moving in front of you, which in this " +
	"game means a monster: that is when firing is worth it. taking_damage is true when a monster is " +
	"hurting you right now. health is 0-100 and ammo is shots left; never fire with ammo 0, and when " +
	"health is low prefer backing away. frames_since_move counts frames where nothing changed, so " +
	"above a few you are stuck against a wall and should turn. When nothing is moving and you are not " +
	"being hurt, keep exploring by moving forward. What should the player do next?"

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
//
// Order matters here, and it is priority order rather than convenience:
// being dead outranks being stuck, and both outrank standing still to
// look for movement. An earlier version ran the motion probe first, which
// meant a probe tick could swallow the use press that a dead player was
// waiting on.
func reflex(state perception.State, tick int) (Action, bool) {
	if tick <= 0 {
		return "", false
	}

	// Dead. Now that health is read from the HUD this is exact, so press
	// use every tick until it takes rather than waiting for a cadence.
	if state.Health != nil && *state.Health == 0 {
		return Use, true
	}

	// Wedged. Nothing on screen has changed for a while, which also
	// means nothing is attacking, so getting unstuck is the only thing
	// worth doing. Handled here rather than left to System 1 because it
	// is mechanical recovery, and because the model does not do it:
	// asked with frames_since_move well past this line, it still
	// answered "forward", i.e. keep pressing into the wall. Alternate
	// between use (in case it is a door) and a consistent turn, which
	// sweeps the view around rather than dithering in place.
	if state.FramesSinceMove >= StuckReviveAfter {
		if tick%StuckReviveInterval == 0 {
			return Use, true
		}
		return TurnRight, true
	}

	// Hold still to let the next frame comparison mean something.
	if IsProbeTick(tick) {
		return Wait, true
	}

	// Blind heartbeat, for when the HUD cannot be read and the death
	// above therefore never fires.
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
