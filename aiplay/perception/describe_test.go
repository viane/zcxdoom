package perception

import (
	"strings"
	"testing"
)

func ptrInt(v int) *int    { return &v }
func ptrBool(v bool) *bool { return &v }

func TestDescribeSaysWhatMatters(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state State
		want  []string
	}{
		{
			"monster on the left while hurt",
			State{
				MotionInView: ptrBool(true), MotionDirection: MotionLeft,
				TakingDamage: true, WallAhead: ptrBool(false),
				Health: ptrInt(18), Ammo: ptrInt(12),
			},
			[]string{"moving on the left", "attacking the player right now", "close to dying", "12 shots"},
		},
		{
			"quiet corridor",
			State{
				MotionInView: ptrBool(false), WallAhead: ptrBool(false),
				Health: ptrInt(100), Ammo: ptrInt(50),
			},
			[]string{"Nothing is moving", "way ahead is clear", "100 health", "50 shots"},
		},
		{
			"blocked, melee weapon",
			State{
				MotionInView: ptrBool(false), WallAhead: ptrBool(true),
				Health: ptrInt(60),
			},
			[]string{"blocked by something directly in front", "melee weapon"},
		},
		{
			"dead",
			State{MotionInView: ptrBool(false), Health: ptrInt(0), Ammo: ptrInt(0)},
			[]string{"player is dead", "no ammunition"},
		},
	} {
		got := Describe(tc.state)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: Describe() = %q, missing %q", tc.name, got, want)
			}
		}
	}
}

// Unknown is not the same as false, and saying "nothing is moving" when
// no probe has run yet would be asserting something never measured.
func TestDescribeDoesNotInventFactsItDoesNotHave(t *testing.T) {
	got := Describe(State{})
	if strings.Contains(got, "Nothing is moving") {
		t.Errorf("Describe() on an empty state claimed nothing is moving: %q", got)
	}
	if !strings.Contains(got, "checked for movement yet") {
		t.Errorf("Describe() on an empty state = %q, want it to say movement is unchecked", got)
	}
	for _, forbidden := range []string{"health", "shots left"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("Describe() on an empty state mentioned %q with no reading: %q", forbidden, got)
		}
	}
}

func TestDescribeSaysWhichWayThereIsRoomToGo(t *testing.T) {
	for _, tc := range []struct {
		dir  string
		want string
	}{
		{DirAhead, "straight ahead"},
		{DirLeft, "to the left"},
		{DirRight, "to the right"},
		{DirBehind, "back the way the player came"},
	} {
		got := Describe(State{OpenDirection: tc.dir})
		if !strings.Contains(got, tc.want) {
			t.Errorf("Describe() with open_direction %q = %q, want it to mention %q", tc.dir, got, tc.want)
		}
	}
	if got := Describe(State{}); strings.Contains(got, "level map") {
		t.Errorf("Describe() mentioned the level map with no reading from it: %q", got)
	}
}
