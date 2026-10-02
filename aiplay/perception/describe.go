package perception

import (
	"fmt"
	"strings"
)

// stuckWorthMentioning is how many unchanged frames are worth a sentence.
// Below it this is noise: a probe tick holds the view still by design and
// produces a couple of unchanged frames on its own.
const stuckWorthMentioning = 4

// Describe renders a State as plain English.
//
// Structured fields are what this package produces and what a JSON-shaped
// System-1 server consumes, but they are not what every decision model
// takes: a classifier reads a passage, not a blob, so for those the
// English is the interface rather than a convenience. It is worth having
// in either case, because a bare number carries nothing on its own --
// measured on a live Kev, health 100 and health 12 produced identical
// answers until the instructions explained what health was.
//
// Each clause states one fact, in the order that matters for the next
// decision: what is threatening the player, then what is in the way,
// then what they have left to fight with.
func Describe(s State) string {
	var parts []string

	switch {
	case s.MotionInView == nil:
		parts = append(parts, "Nothing has been checked for movement yet.")
	case *s.MotionInView:
		where := "nearby"
		switch s.MotionDirection {
		case MotionLeft:
			where = "on the left"
		case MotionRight:
			where = "on the right"
		case MotionAhead:
			where = "straight ahead"
		}
		parts = append(parts, fmt.Sprintf("A monster is moving %s.", where))
	default:
		parts = append(parts, "Nothing is moving nearby.")
	}

	if s.TakingDamage {
		parts = append(parts, "Something is attacking the player right now.")
	}

	switch {
	case s.WallAhead == nil:
	case *s.WallAhead:
		parts = append(parts, "Walking forward is blocked by something directly in front.")
	default:
		parts = append(parts, "The way ahead is clear.")
	}

	if s.FramesSinceMove >= stuckWorthMentioning {
		parts = append(parts, fmt.Sprintf("Nothing on screen has changed for %d frames.", s.FramesSinceMove))
	}

	if s.Health != nil {
		switch h := *s.Health; {
		case h == 0:
			parts = append(parts, "The player is dead.")
		case h <= 25:
			parts = append(parts, fmt.Sprintf("The player has only %d health left and is close to dying.", h))
		default:
			parts = append(parts, fmt.Sprintf("The player has %d health.", h))
		}
	}

	switch {
	case s.Ammo == nil:
		parts = append(parts, "The player is holding a melee weapon, which needs no ammunition.")
	case *s.Ammo == 0:
		parts = append(parts, "The player has no ammunition left and cannot shoot.")
	default:
		parts = append(parts, fmt.Sprintf("The player has %d shots left.", *s.Ammo))
	}

	if s.Tactic != "" {
		parts = append(parts, "The current plan is to "+strings.TrimSuffix(s.Tactic, ".")+".")
	}
	return strings.Join(parts, " ")
}
