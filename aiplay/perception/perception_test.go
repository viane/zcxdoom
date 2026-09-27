package perception

import (
	"image"
	"image/color"
	"testing"
)

func solid(c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestExtractFirstFrameHasNoBaseline(t *testing.T) {
	s := Extract(nil, solid(color.Black), 0, "")
	if s.DiffScore != 1 {
		t.Errorf("DiffScore on first frame = %v, want 1 (no baseline to compare against)", s.DiffScore)
	}
}

func TestExtractIdenticalFramesDontDiffer(t *testing.T) {
	frame := solid(color.RGBA{R: 100, G: 120, B: 140, A: 255})
	s := Extract(frame, frame, 0, "")
	if s.DiffScore != 0 {
		t.Errorf("DiffScore between identical frames = %v, want 0", s.DiffScore)
	}
}

func TestExtractBlackToWhiteIsFullyChanged(t *testing.T) {
	s := Extract(solid(color.Black), solid(color.White), 0, "")
	if s.DiffScore != 1 {
		t.Errorf("DiffScore black->white = %v, want 1", s.DiffScore)
	}
}

func TestMeanBrightnessOrdering(t *testing.T) {
	black := Extract(nil, solid(color.Black), 0, "").MeanBrightness
	white := Extract(nil, solid(color.White), 0, "").MeanBrightness
	if !(black < 0.01 && white > 0.99) {
		t.Errorf("MeanBrightness black=%v white=%v, want ~0 and ~1", black, white)
	}
}

func TestFramesSinceMoveAccumulatesWhileStuckAndResetsOnChange(t *testing.T) {
	still := solid(color.RGBA{R: 50, G: 50, B: 50, A: 255})
	moving := solid(color.RGBA{R: 200, G: 200, B: 200, A: 255})

	s := Extract(still, still, 0, "")
	if s.FramesSinceMove != 1 {
		t.Fatalf("after 1 static frame, FramesSinceMove = %d, want 1", s.FramesSinceMove)
	}
	s = Extract(still, still, s.FramesSinceMove, "")
	if s.FramesSinceMove != 2 {
		t.Fatalf("after 2 static frames, FramesSinceMove = %d, want 2", s.FramesSinceMove)
	}
	s = Extract(still, moving, s.FramesSinceMove, "")
	if s.FramesSinceMove != 0 {
		t.Fatalf("after a changed frame, FramesSinceMove = %d, want 0 (reset)", s.FramesSinceMove)
	}
}

func TestTacticPassesThroughUnchanged(t *testing.T) {
	s := Extract(nil, solid(color.Black), 0, "retreat and find ammo")
	if s.Tactic != "retreat and find ammo" {
		t.Errorf("Tactic = %q, want it passed through unchanged", s.Tactic)
	}
}
