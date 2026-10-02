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

	// Glance hands the tick over to aiplay's automap glance, which drives
	// the keyboard itself for about a second. Like Wait it is never
	// offered to System 1. See GlanceInterval.
	Glance Action = "glance"
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
	case Wait, Glance:
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
// still. During ordinary play every pixel moves, so aiplay stops on
// every ProbeInterval-th tick and takes its own pair of frames a moment
// apart, with the keys released -- see the probe in cmd/aiplay.
//
// One tick is enough because the view settles fast: measured on a live
// game, it is already identical 100ms after the keys come up. An earlier
// version spent two consecutive ticks on this and compared the ordinary
// per-tick frames, which cost twice as much and left the answer up to
// twelve ticks old by the time it was used.
const ProbeInterval = 6

// IsProbeTick reports whether the action for this tick should be Wait to
// hold the view still for a motion probe.
func IsProbeTick(tick int) bool {
	if tick <= 0 {
		return false
	}
	return tick%ProbeInterval == 0
}

// GlanceInterval is how often the player checks the level map, counted in
// decision ticks.
//
// Everything else perception reports is about what is directly in front
// of the player, which is enough to fight with and not enough to get
// anywhere: a run that has cleared the monsters out still wanders,
// because nothing in the state says where it has not been yet. The map
// does say that, and reading it is the one thing here that costs real
// time -- about a second, with the 3D view hidden for all of it -- so it
// happens on its own slow cadence rather than every tick.
//
// Prime, and deliberately not a multiple of ProbeInterval: the two would
// otherwise land on the same tick regularly, and whichever lost would
// never run.
const GlanceInterval = 17

// IsGlanceTick reports whether this tick should be spent reading the map.
func IsGlanceTick(tick int) bool {
	if tick <= 0 {
		return false
	}
	return tick%GlanceInterval == 0
}

// WallUseInterval is how often a player who has walked into something
// tries use on it, counted in decision ticks.
//
// Pressing use is what opens a door, and from in front there is nothing
// to tell a door from a wall: both stop the player dead and both fill the
// view with a flat texture. So walking into something is reason enough to
// try the handle. Every third tick rather than every tick because most
// walls really are walls, and the ticks in between are left to System 1,
// which is told to turn away.
const WallUseInterval = 3

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
// The reason was that the state could not then say whether an enemy was
// on screen, so nothing in it distinguished "shoot" from "explore" and
// the model fell back on tone. It can now -- see perception's motion
// probe and HUD reader -- but the lesson stands, so this wording still
// leans the safer way: it says what each field means and what follows
// from it, and leaves the conclusion to the values rather than to the
// prose.
const instructions = "You are playing Doom. Your goal is to get out of this level and on to the next " +
	"one, which means exploring it until you find the way out, so when nothing is threatening you, " +
	"cover ground. " +
	"something_moving_in_view is true when something alive is moving nearby, which in this game means " +
	"a monster. moving_direction says where it is: if it is left or right, turn that way to face it, " +
	"and once it is ahead, fire. If taking_damage is true but nothing is moving in view, whatever is " +
	"hurting you is behind you or out of sight: turn to find it rather than shooting straight ahead " +
	"at nothing. Do not fire when nothing is moving in view, because there is nothing in front to " +
	"hit. taking_damage is true when a monster is " +
	"hurting you right now. health is 0-100 and ammo is shots left; never fire with ammo 0, and when " +
	"health is low prefer backing away. frames_since_move counts frames where nothing changed, so " +
	"above a few you are stuck against a wall and should turn. wall_directly_ahead is true when " +
	"pressing forward has stopped getting you anywhere because something is in the way: moving " +
	"forward again will not help. What is in the way may be a closed door, which use opens, so try " +
	"use once and turn away if that does not help. use also presses the switches that open the way " +
	"on. open_direction is where the level map shows the most room to move, and it is the best " +
	"guess at where you have not been yet: when nothing is moving and you are not being hurt, head " +
	"that way -- forward if it is ahead, otherwise turn that way first. When ammo is missing " +
	"entirely you are holding a melee weapon, which needs none. What should the player do next?"

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
		answers, err := s1.Ask(ctx, describeState(state), map[string]system1.Question{
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

	// Walked into something. Try the handle before giving up on it.
	if state.WallAhead != nil && *state.WallAhead && tick%WallUseInterval == 0 {
		return Use, true
	}

	// Hold still to let the next frame comparison mean something.
	if IsProbeTick(tick) {
		return Wait, true
	}

	// Check the map. After the probe, because a monster in the room is
	// more urgent than where to go next, and both are more urgent than
	// the blind heartbeat below.
	if IsGlanceTick(tick) {
		return Glance, true
	}

	// Blind heartbeat, for when the HUD cannot be read and the death
	// above therefore never fires.
	return Use, tick%ReviveInterval == 0
}

// statePayload is what actually goes over the wire: the structured
// fields, plus the same situation written out in English.
//
// Both, rather than one or the other, because System-1 servers do not
// agree on what a state is. A server that prompts a language model gets
// more out of the sentence than the blob -- bare numbers changed nothing
// until the instructions explained them -- while a classifier reads a
// passage and nothing else. Sending both keeps aiplay working against
// either without a flag to get wrong.
type statePayload struct {
	perception.State
	Situation string `json:"situation"`
}

func describeState(s perception.State) statePayload {
	return statePayload{State: s, Situation: perception.Describe(s)}
}

func isValidChoice(choice string) bool {
	_, ok := criteria[choice]
	return ok
}

// fallback is a deliberately simple, dependency-free policy: move forward
// most of the time, turn to break out of being stuck, and fire
// periodically. It ignores everything the state knows about enemies,
// health and where to go, because its job is to keep the game moving
// while System 1 is unreachable, not to play well.
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
