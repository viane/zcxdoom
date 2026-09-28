package perception

import (
	"image"
	"image/color"
	_ "image/png"
	"os"
	"testing"
)

// The testdata frames are real 640x480 captures from a running game,
// taken with tools/vncharness, and their expected values were read off
// the frames by hand. They are the calibration this package's HUD
// geometry was derived against, so they are what stops a change to that
// geometry from silently going wrong.
func loadFrame(t *testing.T, name string) image.Image {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestReadHUDOnRealFrames(t *testing.T) {
	for _, tc := range []struct {
		file                string
		health, ammo, armor int
	}{
		{"hud_health100.png", 100, 50, 0},
		{"hud_health48.png", 48, 50, 0},
		{"hud_health26.png", 26, 50, 0},
	} {
		hud, ok := ReadHUD(loadFrame(t, tc.file))
		if !ok {
			t.Errorf("%s: ReadHUD reported failure, want a reading", tc.file)
			continue
		}
		if hud.Health != tc.health || hud.Ammo != tc.ammo || hud.Armor != tc.armor {
			t.Errorf("%s: got health=%d ammo=%d armor=%d, want health=%d ammo=%d armor=%d",
				tc.file, hud.Health, hud.Ammo, hud.Armor, tc.health, tc.ammo, tc.armor)
		}
	}
}

// Doom tints the whole palette red while the player is taking damage,
// which lifts the dark interior of a 0 or 8 toward the brightness of the
// stroke around it. An earlier version thresholded on a fraction of each
// cell's peak and lost the hole entirely, so the digit matched nothing
// and health became unreadable exactly while the player was being hurt
// -- the moment it matters most. This frame is one of those.
func TestReadHUDDuringDamageFlash(t *testing.T) {
	hud, ok := ReadHUD(loadFrame(t, "hud_health88_damageflash.png"))
	if !ok {
		t.Fatal("ReadHUD reported failure on a damage-flash frame, want a reading")
	}
	if hud.Health != 88 {
		t.Errorf("health = %d, want 88", hud.Health)
	}
}

func TestReadHUDFailsClosedOnNonGameFrame(t *testing.T) {
	// A blank framebuffer has no digits anywhere. Reporting "health 0"
	// here would tell System 1 the player is dead, so this has to fail
	// rather than fall back to a zero value.
	blank := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for y := 0; y < 480; y++ {
		for x := 0; x < 640; x++ {
			blank.Set(x, y, color.RGBA{20, 20, 20, 255})
		}
	}
	if hud, ok := ReadHUD(blank); ok {
		t.Errorf("ReadHUD on a blank frame = %+v, ok=true; want ok=false", hud)
	}
}

func TestExtractPopulatesHUDFieldsFromARealFrame(t *testing.T) {
	img := loadFrame(t, "hud_health100.png")
	s := Extract(nil, img, 0, "")
	if s.Health == nil || s.Ammo == nil || s.Armor == nil {
		t.Fatalf("Extract left HUD fields nil (health=%v ammo=%v armor=%v), want them populated",
			s.Health, s.Ammo, s.Armor)
	}
	if *s.Health != 100 {
		t.Errorf("state health = %d, want 100", *s.Health)
	}
}

func TestExtractLeavesHUDNilWhenUnreadable(t *testing.T) {
	// Nil, not zero: see the comment on State's HUD fields.
	blank := image.NewRGBA(image.Rect(0, 0, 640, 480))
	s := Extract(nil, blank, 0, "")
	if s.Health != nil || s.Ammo != nil || s.Armor != nil {
		t.Errorf("Extract on an unreadable frame set HUD fields (health=%v ammo=%v armor=%v), want all nil",
			s.Health, s.Ammo, s.Armor)
	}
}

// The numbers here are measured, not invented: they come from sampling a
// live game with the player standing still, and are recorded in the
// comment on MotionThreshold. A blinking light sector changes more of the
// picture than a monster does, so DiffScore alone cannot tell them apart
// and every blink would be reported as something moving.
func TestMotionSeenDistinguishesMovementFromLighting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state State
		want  bool
	}{
		{"perfectly static view", State{DiffScore: 0.0, BrightnessDelta: 0}, false},
		{"animated wall panel", State{DiffScore: 0.00776, BrightnessDelta: 0}, false},
		{"animated wall panel, larger", State{DiffScore: 0.00891, BrightnessDelta: 0}, false},
		{"blinking light sector", State{DiffScore: 0.04104, BrightnessDelta: -0.0179}, false},
		{"blink the other way", State{DiffScore: 0.04104, BrightnessDelta: 0.0179}, false},
		{"something walking through view", State{DiffScore: 0.05182, BrightnessDelta: 0.0004}, true},
		{"smaller movement", State{DiffScore: 0.02, BrightnessDelta: -0.0002}, true},
	} {
		if got := MotionSeen(tc.state); got != tc.want {
			t.Errorf("%s: MotionSeen(diff=%.5f, dBright=%+.4f) = %v, want %v",
				tc.name, tc.state.DiffScore, tc.state.BrightnessDelta, got, tc.want)
		}
	}
}

func TestExtractReportsBrightnessDelta(t *testing.T) {
	dark := loadFrame(t, "hud_health26.png")
	bright := loadFrame(t, "hud_health100.png")
	if d := Extract(dark, bright, 0, "").BrightnessDelta; d == 0 {
		t.Error("BrightnessDelta = 0 between two visibly different frames, want non-zero")
	}
	if d := Extract(bright, bright, 0, "").BrightnessDelta; d != 0 {
		t.Errorf("BrightnessDelta between a frame and itself = %v, want 0", d)
	}
}
