// Package perception turns a raw VNC screenshot into the small, typed
// summary that System 1 reasons over.
//
// This is deliberately not vision: even TypeSafe's own official Doom demo
// feeds Jev structured game state rather than pixels, and Kev's typed
// choice/score/bool question contract is built around exactly that kind of
// shared state blob, not an image. So this package's job is the
// translation step -- pixels in, plain facts out -- and it's the one place
// that needs to change if the game, its resolution, or its HUD layout ever
// does.
//
// What's here: frame-to-frame change, overall brightness, and the three
// status-bar numbers (health, ammo, armor) read off the rendered HUD --
// all verified against real captured gameplay frames. See hud.go for the
// HUD geometry and how it was calibrated.
//
// Reading the HUD matters more than it might look: without it the state
// is three numbers that say nothing about the player's situation, and
// System 1 answers "forward" to every one of them, because with no
// health, no ammo and no enemies in the state there is nothing else the
// state could justify. Measured before this existed: 17 of 17 decisions
// were "forward".
//
// Still missing, and the natural next step: enemy-visible. That one does
// need work this doesn't -- monsters are drawn at varying scale, eight
// rotations and several animation frames, where the HUD font is fixed
// and pixel-exact -- see tools/README.md.
package perception

import "image"

// State is the structured, typed snapshot of one game frame.
type State struct {
	Width           int     `json:"width,omitempty"`
	Height          int     `json:"height,omitempty"`
	DiffScore       float64 `json:"diff_score"`        // 0..1: fraction of sampled pixels that changed since the previous frame
	MeanBrightness  float64 `json:"mean_brightness"`   // 0..1
	FramesSinceMove int     `json:"frames_since_move"` // consecutive ticks with DiffScore below StuckThreshold
	Tactic          string  `json:"tactic,omitempty"`  // System 2's most recent high-level instruction, if any

	// Read from the status bar; nil when the HUD could not be read from
	// this frame. Pointers rather than plain ints so that an unreadable
	// frame omits them from the state entirely: 0 is a real, meaningful
	// health value (it means dead), so sending 0 for "don't know" would
	// tell System 1 the exact opposite of the truth.
	Health *int `json:"health,omitempty"`
	Ammo   *int `json:"ammo,omitempty"`
	Armor  *int `json:"armor,omitempty"`

	// True when something in view moved on its own -- see ProbeMotion.
	// nil until the first motion probe has completed, since "nothing is
	// moving" and "we have not looked yet" are different claims.
	MotionInView *bool `json:"something_moving_in_view,omitempty"`

	// Which way the movement was, when there was any: "left", "ahead" or
	// "right". Turning toward it is what lets the player shoot back.
	MotionDirection string `json:"moving_direction,omitempty"`

	// True when health dropped since the previous frame: something is
	// hurting the player right now. Exact, since it comes from the HUD
	// reading rather than from the picture.
	TakingDamage bool `json:"taking_damage"`

	// True when pressing forward has stopped getting the player
	// anywhere -- see ForwardBlocked. nil until enough forward presses
	// have been seen to judge.
	WallAhead *bool `json:"wall_directly_ahead,omitempty"`
}

// ForwardProgress is how much of the view a step forward is expected to
// change when the way is actually clear.
//
// Measured by walking a live game into a wall: while the way was clear,
// consecutive frames differed by 0.26 to 0.53; once against the wall,
// by 0.007 to 0.024. The gap between those is enormous, so this sits
// well inside it.
//
// Note how badly FramesSinceMove handles this case, which is why it
// needs its own signal: 0.007 to 0.024 straddles StuckThreshold, so
// pressed against a wall the stuck counter climbs and resets and never
// gets anywhere. The player is not still -- Doom slides them along the
// wall, so the view keeps changing -- they are just not getting
// anywhere, and those are different things.
const ForwardProgress = 0.05

// ForwardSamples is how many recent forward presses ForwardBlocked
// judges over. More than one because a single step can legitimately
// change little (facing a blank wall across a room), and too many would
// be slow to notice.
const ForwardSamples = 4

// ForwardBlocked reports whether the last few forward presses moved the
// view enough to count as progress. diffs are the DiffScores observed
// after each of those presses, most recent last.
func ForwardBlocked(diffs []float64) (blocked, known bool) {
	if len(diffs) < ForwardSamples {
		return false, false
	}
	var sum float64
	for _, d := range diffs[len(diffs)-ForwardSamples:] {
		sum += d
	}
	return sum/ForwardSamples < ForwardProgress, true
}

// Thresholds for MotionSeen, measured against a live game with the
// player standing still (see ProbeMotion for why that matters):
//
//	perfectly static view      diff 0.00000   brightness delta  0.0000
//	animated wall panel        diff 0.00776   brightness delta  0.0000
//	              "            diff 0.00891   brightness delta  0.0000
//	blinking light sector      diff 0.04104   brightness delta -0.0179
//
// E1M1 blinks its lights, and a blink changes far more of the picture
// than a monster does -- so DiffScore alone calls every blink a monster.
// What separates them is that a blink changes how bright the room is and
// a monster walking across it does not, which is the second measurement
// MotionSeen takes.
const (
	MotionThreshold      = 0.015 // above the animated panels, below a blink
	LightChangeTolerance = 0.004 // a blink moves brightness four times this
)

// Where in the view something moved. Doom's autoaim covers the vertical,
// so left/right is all a player needs to bring a target into the line of
// fire.
const (
	MotionAhead = "ahead"
	MotionLeft  = "left"
	MotionRight = "right"
)

// viewportBottom is the first row of the status bar: ST_Y (168) in game
// coordinates, scaled. Below it is HUD, not world.
const viewportBottom = 403 // int(168 * scaleY), rounded down

// Motion is what a probe saw.
type Motion struct {
	Seen bool
	// Where the movement was, one of MotionAhead/Left/Right. Only
	// meaningful when Seen.
	Direction string
}

// ProbeMotion compares two frames taken a moment apart with the player's
// keys released, and reports whether anything moved and roughly where.
//
// Direction is the point of it. "Something is moving" tells the player
// there is a monster but not which way to face, and facing it is the
// whole problem: Doom aims vertically by itself, so a player who knows
// left from right can bring a target into the line of fire and one who
// does not can only walk into it.
func ProbeMotion(a, b image.Image) Motion {
	bounds := b.Bounds()
	third := bounds.Dx() / 3
	var counts [3]int
	total, n := 0, 0

	for y := bounds.Min.Y; y < viewportBottom && y < bounds.Max.Y; y += sampleStride {
		for x := bounds.Min.X; x < bounds.Max.X; x += sampleStride {
			n++
			pr, pg, pb, _ := a.At(x, y).RGBA()
			cr, cg, cb, _ := b.At(x, y).RGBA()
			if absDiff(pr, cr) > perChannelThreshold || absDiff(pg, cg) > perChannelThreshold || absDiff(pb, cb) > perChannelThreshold {
				total++
				switch i := (x - bounds.Min.X) / third; {
				case i <= 0:
					counts[0]++
				case i == 1:
					counts[1]++
				default:
					counts[2]++
				}
			}
		}
	}
	if n == 0 {
		return Motion{}
	}

	if !MotionSeen(float64(total)/float64(n), meanBrightness(b)-meanBrightness(a)) {
		return Motion{}
	}

	dir := MotionAhead
	switch {
	case counts[0] > counts[1] && counts[0] > counts[2]:
		dir = MotionLeft
	case counts[2] > counts[1] && counts[2] > counts[0]:
		dir = MotionRight
	}
	return Motion{Seen: true, Direction: dir}
}

// MotionSeen applies the thresholds above to one pair of measurements:
// how much of the view changed, and how much its overall brightness did.
// Kept separate from ProbeMotion so the numbers that decide this can be
// tested directly against the ones that were measured.
func MotionSeen(diffScore, brightnessDelta float64) bool {
	if diffScore <= MotionThreshold {
		return false
	}
	if brightnessDelta < 0 {
		brightnessDelta = -brightnessDelta
	}
	return brightnessDelta < LightChangeTolerance
}

// StuckThreshold is the DiffScore below which a frame counts as
// "nothing changed" for FramesSinceMove.
const StuckThreshold = 0.01

// sampleStride keeps this fast enough to run every decision tick: Doom's
// 640x480 frame is ~300k pixels, so scan a grid instead of every one.
const sampleStride = 4

// perChannelThreshold is how far a sampled pixel's R, G, or B has to move
// (out of 65535, RGBA's per-channel range) to count as "changed". Raw RFB
// encoding is lossless, so there's no compression noise to filter here;
// this just tolerates Doom's own dithering between near-identical frames.
const perChannelThreshold = 8000

// Extract compares curr against prev (nil for the very first frame) and
// returns the facts System 1 is asked about. framesSinceMove carries the
// running "stuck" counter forward from the previous call, and tactic is
// System 2's latest instruction, passed through unchanged.
func Extract(prev, curr image.Image, framesSinceMove int, tactic string) State {
	b := curr.Bounds()
	s := State{Width: b.Dx(), Height: b.Dy(), Tactic: tactic}

	s.MeanBrightness = meanBrightness(curr)
	if prev == nil {
		s.DiffScore = 1 // no baseline yet
	} else {
		s.DiffScore = diffScore(prev, curr)
	}

	if s.DiffScore < StuckThreshold {
		s.FramesSinceMove = framesSinceMove + 1
	} else {
		s.FramesSinceMove = 0
	}

	if hud, ok := ReadHUD(curr); ok {
		s.Health, s.Armor = &hud.Health, &hud.Armor
		s.Ammo = hud.Ammo // nil for a weapon that uses no ammunition
	}
	return s
}

// meanBrightness returns the average luma (ITU-R BT.601 weights) over a
// sampled grid, scaled to 0..1.
func meanBrightness(img image.Image) float64 {
	b := img.Bounds()
	var sum float64
	var n int
	for y := b.Min.Y; y < b.Max.Y; y += sampleStride {
		for x := b.Min.X; x < b.Max.X; x += sampleStride {
			r, g, bl, _ := img.At(x, y).RGBA()
			luma := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)
			sum += luma / 65535
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// diffScore returns the fraction of sampled grid points whose color moved
// by more than perChannelThreshold in any channel between prev and curr.
func diffScore(prev, curr image.Image) float64 {
	b := curr.Bounds()
	var changed, n int
	for y := b.Min.Y; y < b.Max.Y; y += sampleStride {
		for x := b.Min.X; x < b.Max.X; x += sampleStride {
			pr, pg, pb, _ := prev.At(x, y).RGBA()
			cr, cg, cb, _ := curr.At(x, y).RGBA()
			if absDiff(pr, cr) > perChannelThreshold || absDiff(pg, cg) > perChannelThreshold || absDiff(pb, cb) > perChannelThreshold {
				changed++
			}
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return float64(changed) / float64(n)
}

func absDiff(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}
