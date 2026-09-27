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
// What's here is a deliberately modest first slice: frame-to-frame change
// and overall brightness, both verified against real captured gameplay
// frames. Richer facts (exact health/ammo via HUD digit reading,
// enemy-visible) are a natural next step, but need calibration against the
// game's actual rendered layout (Doom draws its HUD inside a scaled,
// bordered 640x480 canvas, not at fixed pixel offsets) that hasn't been
// done yet -- see tools/README.md before adding to State.
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
