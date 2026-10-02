package perception

import (
	"image"
	"image/png"
	"math"
	"os"
	"testing"
)

// The two pairs under testdata are real glances at a live game, captured
// by cmd/automapprobe a second or so apart while the player stood in the
// same spot. Each pair is the frame before a short forward press and the
// frame after it, which is what ReadAutomap takes.
//
// The headings they are checked against are not hand-labelled, which
// would be guesswork: they come from reading the map while the player
// turned at Doom's own rate. Held right for 400ms a step, eight
// consecutive glances came back 34 to 47 degrees apart, every one of them
// in the turning direction, against the 49 degrees that rate predicts.
// Standing still, four consecutive glances agreed to within 5 degrees.
const (
	corridorFacing = 320.5
	openFacing     = 325.3
)

func loadAutomap(t *testing.T, name string) image.Image {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestReadAutomapReadsFacingAndOpenDirection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		facing float64
		open   string
	}{
		{"corridor", corridorFacing, DirLeft},
		{"open", openFacing, DirAhead},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := loadAutomap(t, "automap_"+tc.name+"_a.png")
			b := loadAutomap(t, "automap_"+tc.name+"_b.png")

			m := ReadAutomap(a, b)
			if !m.Known {
				t.Fatal("ReadAutomap could not read a real glance at the map")
			}
			if d := math.Abs(math.Mod(m.Facing-tc.facing+540, 360) - 180); d > 5 {
				t.Errorf("facing = %.1f deg, want %.1f (off by %.1f)", m.Facing, tc.facing, d)
			}
			if m.OpenDirection != tc.open {
				t.Errorf("OpenDirection = %q, want %q", m.OpenDirection, tc.open)
			}
			if m.OpenDistance <= 0 {
				t.Errorf("OpenDistance = %.1f, want a positive distance", m.OpenDistance)
			}
			// Doom picks the starting zoom from the level's bounding
			// box, so this is not a constant -- but it is measured off
			// an arrow of known size, so it should land near the 0.18 to
			// 0.20 Doom pixels per map unit E1M1 is drawn at.
			if m.Scale < 0.1 || m.Scale > 0.3 {
				t.Errorf("Scale = %.3f px/unit, want something near E1M1's 0.19", m.Scale)
			}
		})
	}
}

// The player barely moved between the two captures, so the two glances
// have to agree about which way they were facing. This is the check that
// would fail if the head/tail test ever started picking the wrong end of
// the arrow, which is the one failure mode that quietly turns every
// direction in the state into its opposite.
func TestReadAutomapAgreesBetweenGlancesFromTheSameSpot(t *testing.T) {
	first := ReadAutomap(loadAutomap(t, "automap_corridor_a.png"), loadAutomap(t, "automap_corridor_b.png"))
	second := ReadAutomap(loadAutomap(t, "automap_open_a.png"), loadAutomap(t, "automap_open_b.png"))
	if !first.Known || !second.Known {
		t.Fatal("both glances should be readable")
	}
	if d := math.Abs(math.Mod(first.Facing-second.Facing+540, 360) - 180); d > 10 {
		t.Errorf("two glances from the same spot read %.1f and %.1f deg, %.1f apart", first.Facing, second.Facing, d)
	}
}

// A glance that cannot be trusted has to say so rather than guess: a
// wrong facing turns every relative direction in the state into its
// opposite, which is worse than no direction at all.
func TestReadAutomapRefusesWhatItCannotResolve(t *testing.T) {
	a := loadAutomap(t, "automap_corridor_a.png")
	b := loadAutomap(t, "automap_corridor_b.png")

	if m := ReadAutomap(a, nil); m.Known {
		t.Error("ReadAutomap with no second frame should not claim a reading")
	}
	if m := ReadAutomap(nil, b); m.Known {
		t.Error("ReadAutomap with no first frame should not claim a reading")
	}
	if m := ReadAutomap(a, a); m.Known {
		t.Error("ReadAutomap should not claim a reading when the player did not move")
	}
	if m := ReadAutomap(loadAutomap(t, "hud_health100.png"), b); m.Known {
		t.Error("ReadAutomap should not claim a reading from a frame of the 3D view")
	}
}

func TestIsAutomapTellsTheMapFromTheView(t *testing.T) {
	if !IsAutomap(loadAutomap(t, "automap_corridor_a.png")) {
		t.Error("IsAutomap said a frame of the automap was not the automap")
	}
	if IsAutomap(loadAutomap(t, "hud_health100.png")) {
		t.Error("IsAutomap said a frame of the 3D view was the automap")
	}
	if IsAutomap(nil) {
		t.Error("IsAutomap said a missing frame was the automap")
	}
}

func TestRelativeDirectionBuckets(t *testing.T) {
	const facing = math.Pi / 2 // north
	for _, tc := range []struct {
		name    string
		bearing float64
		want    string
	}{
		{"straight on", math.Pi / 2, DirAhead},
		{"just off to one side", math.Pi/2 + math.Pi/8, DirAhead},
		{"ninety degrees left", math.Pi, DirLeft},
		{"ninety degrees right", 0, DirRight},
		{"straight back", 3 * math.Pi / 2, DirBehind},
	} {
		if got := relativeDirection(tc.bearing, facing); got != tc.want {
			t.Errorf("%s: relativeDirection = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestClassifyAutomapSeparatesTheFourColours(t *testing.T) {
	// The palette entries am_map.c draws with, as they arrive on screen
	// after Doom's gamma table and the stretch to 640x480, plus the same
	// colours dimmed by the blending that stretch does at a line's edge.
	for _, tc := range []struct {
		name    string
		r, g, b int
		want    amColor
	}{
		{"background", 1, 1, 1, amNothing},
		{"one-sided wall", 255, 1, 1, amWall},
		{"wall, blended", 203, 1, 1, amWall},
		{"wall, nearly faded out", 68, 1, 1, amNothing},
		{"ceiling change", 255, 255, 1, amDoor},
		{"floor change", 191, 124, 76, amStep},
		{"player arrow", 255, 235, 219, amArrow},
	} {
		if got := classifyAutomap(tc.r, tc.g, tc.b); got != tc.want {
			t.Errorf("%s: classifyAutomap(%d,%d,%d) = %v, want %v", tc.name, tc.r, tc.g, tc.b, got, tc.want)
		}
	}
}
