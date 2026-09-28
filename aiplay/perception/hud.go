package perception

import "image"

// Doom's status bar geometry, in the game's own 320x200 coordinates,
// taken from dockerdoom/trunk/src/st_stuff.c: ST_AMMOX, ST_HEALTHX,
// ST_ARMORX and ST_*Y, with ST_*WIDTH digits each.
//
// STlib_drawNum (st_lib.c) draws a number right to left from its x,
// advancing by a constant width -- the width of the '0' patch -- for
// every digit regardless of which digit it is, and simply doesn't draw
// leading zeros. So each field is a fixed grid of cells, and a blank
// cell means "no more digits", not "zero". The one exception is a value
// of exactly 0, which draws a single '0' in the rightmost cell.
const (
	ammoRightX   = 44
	healthRightX = 90
	armorRightX  = 221
	numberTopY   = 171
	numberDigits = 3

	glyphW = 13 // width of an STTNUM patch, and the per-digit advance
	glyphH = 16
)

// The framebuffer is 640x480 (see the Xvfb geometry in the repository
// root's main.go) while the game renders 320x200, so the picture is
// doubled horizontally and stretched 2.4x vertically. Verified against
// captured frames: the health field's '%' begins exactly at x=180
// (ST_HEALTHX 90 * 2) and its digits' top row at y=410 (ST_HEALTHY 171
// * 2.4).
const (
	scaleX = 2.0
	scaleY = 2.4
)

// Ink thresholds, in "redness" (red minus the larger of green and blue).
// Redness rather than brightness because the digits are red on a brown
// bar, which are close in luminance but far apart here.
//
// Both thresholds are relative to the contrast within a single cell
// rather than to absolute values, because Doom tints the whole palette
// red while taking damage (and gold on a pickup). A fraction of the
// cell's peak is not enough on its own: under the red tint the dark
// interior of a 0 or 8 lifts far enough to cross it, the hole fills in,
// and the glyph stops matching any digit -- which meant health went
// unreadable at exactly the moment it mattered, while taking damage.
// Measuring from the cell's own floor instead keeps the hole below the
// line, because the tint raises the floor along with everything else.
const (
	minContrast  = 35  // a cell flatter than this holds no digit at all
	bodyFraction = 0.5 // share of a cell's contrast range that counts as lit
)

// maxGlyphMismatch is how many of a glyph's 13x16 cells may disagree
// before a match is rejected as "not a digit". Chosen well below the
// closest distance between two different Doom digits, so a bad frame
// reads as unknown rather than as the wrong number.
const maxGlyphMismatch = 45

// HUD is what the status bar says, read off the rendered frame.
type HUD struct {
	Health int
	Ammo   int
	Armor  int
}

// ReadHUD reads the three status-bar numbers. ok is false if any of them
// could not be read with confidence -- a partially drawn frame, or the
// status bar obscured -- in which case callers should report nothing
// rather than guess, since a wrong health reading is worse than none.
func ReadHUD(img image.Image) (hud HUD, ok bool) {
	health, okH := readNumber(img, healthRightX)
	ammo, okA := readNumber(img, ammoRightX)
	armor, okR := readNumber(img, armorRightX)
	if !okH || !okA || !okR {
		return HUD{}, false
	}
	return HUD{Health: health, Ammo: ammo, Armor: armor}, true
}

// readNumber reads one right-aligned field whose rightmost edge is at
// rightX in game coordinates.
func readNumber(img image.Image, rightX int) (int, bool) {
	value, place := 0, 1
	for i := 0; i < numberDigits; i++ {
		digit, state := readDigit(img, rightX-(i+1)*glyphW)
		switch state {
		case cellBlank:
			// Leading zeros are never drawn, so the first blank ends
			// the number -- unless nothing at all was drawn, which is
			// not a state the game produces and means we misread.
			if i == 0 {
				return 0, false
			}
			return value, true
		case cellUnknown:
			return 0, false
		}
		value += digit * place
		place *= 10
	}
	return value, true
}

type cellState int

const (
	cellDigit cellState = iota
	cellBlank
	cellUnknown
)

// readDigit classifies the single glyph cell whose left edge is at
// leftX in game coordinates.
func readDigit(img image.Image, leftX int) (int, cellState) {
	var lit [glyphH][glyphW]bool
	var red [glyphH][glyphW]int
	peak, floor := 0, 1<<30

	for ty := 0; ty < glyphH; ty++ {
		for tx := 0; tx < glyphW; tx++ {
			// Sample the middle of the screen area each glyph pixel was
			// stretched across, so the non-integer vertical scale never
			// lands on a boundary row.
			x := int(float64(leftX)*scaleX + (float64(tx)+0.5)*scaleX)
			y := int(float64(numberTopY)*scaleY + (float64(ty)+0.5)*scaleY)
			v := redness(img, x, y)
			red[ty][tx] = v
			if v > peak {
				peak = v
			}
			if v < floor {
				floor = v
			}
		}
	}
	if peak-floor < minContrast {
		return 0, cellBlank
	}

	cut := floor + int(float64(peak-floor)*bodyFraction)
	for ty := 0; ty < glyphH; ty++ {
		for tx := 0; tx < glyphW; tx++ {
			lit[ty][tx] = red[ty][tx] > cut
		}
	}

	best, bestScore := -1, maxGlyphMismatch+1
	for n, glyph := range digitGlyphs {
		score := 0
		for ty := 0; ty < glyphH; ty++ {
			row := glyph[ty]
			for tx := 0; tx < glyphW; tx++ {
				if (row[tx] == '#') != lit[ty][tx] {
					score++
				}
			}
		}
		if score < bestScore {
			best, bestScore = n, score
		}
	}
	if best < 0 {
		return 0, cellUnknown
	}
	return best, cellDigit
}

// redness is how strongly a pixel reads as Doom's red digit font.
func redness(img image.Image, x, y int) int {
	r, g, b, _ := img.At(x, y).RGBA()
	R, G, B := int(r>>8), int(g>>8), int(b>>8)
	other := G
	if B > other {
		other = B
	}
	return R - other
}
