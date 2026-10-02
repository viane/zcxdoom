// Automap reading. See ReadAutomap for what a glance at the map is for;
// this file is the geometry and the colour table behind it.

package perception

import (
	"image"
	"math"
)

// Doom's automap is a line drawing in a handful of fixed palette
// entries on black, and -- unlike the 3D view, where monsters and walls
// share essentially the whole palette -- each entry means exactly one
// thing. From dockerdoom/trunk/src/am_map.c:
//
//	WHITE        (209)  the player arrow
//	REDS         (176)  one-sided line: a solid wall, nothing behind it
//	YELLOWS      (231)  two-sided line whose ceiling height changes: a door
//	BROWNS        (64)  two-sided line whose floor height changes: a step,
//	                    a ledge, or the lift the player has to ride
//
// The colours those indices actually reach the screen as are not quite
// the palette values: Doom runs every colour through its gamma table on
// the way out, which for the default gamma level adds one to the low
// end (black arrives as rgb(1,1,1)), and the 320x200 picture is stretched
// to 640x480, which blends line pixels into the background. Blending
// toward black scales all three channels together, so it costs
// brightness but leaves the ratios between channels alone -- which is why
// these tests are written as ratios with a brightness floor rather than
// as exact values.
type amColor int

const (
	amNothing amColor = iota
	amWall            // one-sided: impassable
	amDoor            // ceiling change: a door, and the way on
	amStep            // floor change: step, ledge or lift
	amArrow           // the player
)

// amMinBrightness is how much r+g+b a pixel needs before its colour is
// taken seriously. The dimmest real line pixel measured on a live frame
// was rgb(68,1,1), summing to 70; the stretch blends lines down to
// nothing at their edges, and reading those faint fringes just thickens
// every line by a pixel.
const amMinBrightness = 150

func classifyAutomap(r, g, b int) amColor {
	if r+g+b < amMinBrightness {
		return amNothing
	}
	switch {
	case r > 170 && g > 150 && b > 140 && r-b < 70: // rgb(255,235,219)
		return amArrow
	case r > 120 && g > 120 && b*3 < r: // rgb(255,255,0)
		return amDoor
	case r > 100 && g*3 < r && b*3 < r: // rgb(255,0,0)
		return amWall
	case r > 100 && g*2 > r && g*3 < r*2 && b*2 < r && b > g/3: // rgb(191,123,75)
		return amStep
	}
	return amNothing
}

// The automap is drawn into the same area as the 3D view, with the
// status bar left alone underneath it -- so the HUD reader keeps working
// during a glance. The level's name is printed across the bottom of the
// map area in the same reddish font as the status bar, which would read
// as a wall, so the scan stops above it: HU_TITLEY is 167 minus the font
// height (hu_stuff.c), about row 159 of 200, scaled here to 640x480.
const amBottom = 378

// amScreenX, amScreenY are screen pixels per Doom pixel. The game draws
// 320x200 and it arrives here stretched to 640x480, so the two axes are
// not the same and anything angular has to divide them out first.
const (
	amScreenX = 2.0
	amScreenY = 2.4
)

// arrowSpan is how long the player arrow is along its own axis, in map
// units. The shape is player_arrow in am_map.c, which runs from -1.125R
// to +1R with R = 8*PLAYERRADIUS/7, and PLAYERRADIUS is 16. Measuring
// the drawn arrow against this is what gives the map's current scale,
// which otherwise varies: Doom picks the starting zoom from the size of
// the level's bounding box, so it differs from map to map.
const arrowSpan = 2.125 * (8 * 16.0 / 7)

// amRange is how far out from the player the rays look, in map units.
// Far enough to find the far wall of a hall, short enough that the
// answer is about where the player is standing rather than the whole
// level.
const amRange = 640

// amRays is how many bearings are cast. 64 is a 5.6-degree spacing,
// comfortably finer than the 90-degree buckets the answer is reduced to.
const amRays = 64

// Automap is what one glance at the automap says about where the player
// is standing and what is around them.
type Automap struct {
	// Known is false when the arrow could not be found (the map was not
	// up, or the glance caught a level change) or its direction could
	// not be resolved. Nothing else here means anything unless this is
	// true.
	Known bool

	// Facing is the player's heading in degrees, 0 east and increasing
	// counter-clockwise, which is Doom's own angle convention.
	Facing float64

	// Scale is Doom pixels per map unit, measured off the arrow.
	Scale float64

	// OpenDirection is the relative direction with the most room to move
	// in -- where to head when there is nothing else to do -- and
	// OpenDistance is how much room that is, in map units, averaged over
	// the bearings pointing that way.
	//
	// "Room" means distance to the nearest solid wall, and on a map that
	// only draws what the player has already seen, a direction with
	// nothing drawn in it measures as wide open. That is the intended
	// reading: somewhere unvisited and a long clear hall are both places
	// worth walking into, and neither is the corner the player is
	// currently nosing into.
	OpenDirection string
	OpenDistance  float64
}

// Relative bearings. Four buckets rather than eight because the actions
// are four -- forward, back, turn left, turn right -- and a bearing the
// player cannot act on is noise.
const (
	DirAhead  = "ahead"
	DirLeft   = "left"
	DirRight  = "right"
	DirBehind = "behind"
)

// ReadAutomap turns a glance at the automap into relative directions.
//
// Why bother, when the 3D view is right there: the view says what is in
// front of the player and nothing else, and the thing that actually
// stops a run is not combat but not knowing where to go next. The
// automap is the game's own answer to that, it is drawn in four
// unambiguous colours on black, and -- because Doom only draws the lines
// the player has already seen -- the parts of it that are still blank
// are exactly the parts worth walking into.
//
// a and b are two frames of the map taken either side of a short forward
// press. Both are needed, and this is the one part that cannot be read
// out of a single frame: the arrow gives its own axis precisely -- fitted
// across a live turn it tracked Doom's own turn rate to within a few
// degrees every time -- but not which end is the point. Its head and
// tail differ by about two pixels at the scale the map is drawn at,
// which measurably is not enough: picking the end by shape got the
// direction backwards on half of fourteen live samples. So the sign
// comes from the picture instead. The map scrolls under the player as
// they walk, and whichever end of the axis lies within 90 degrees of the
// way it scrolled is the front. A forward press can be deflected by a
// wall, but never by as much as 90 degrees, so the test survives the
// player sliding along one.
//
// If b is nil, or the player did not move between the two, Known is
// false: better to say nothing this glance than to say left when it is
// right.
func ReadAutomap(a, b image.Image) Automap {
	if a == nil || b == nil {
		return Automap{}
	}
	arrow, lines := scanAutomap(a)
	axis, px, py, span, ok := arrowAxis(arrow)
	if !ok {
		return Automap{}
	}
	_, after := scanAutomap(b)
	dx, dy, moved := automapPan(lines, after)
	if !moved {
		return Automap{}
	}

	// The geometry scrolls the opposite way to the player, and screen y
	// grows downward where Doom's y grows north.
	travel := math.Atan2(float64(dy)/amScreenY, -float64(dx)/amScreenX)
	facing := axis
	if math.Abs(angleDelta(axis, travel)) > math.Pi/2 {
		facing = axis + math.Pi
	}

	if span <= 0 {
		return Automap{}
	}
	scale := span / arrowSpan
	m := Automap{
		Known:  true,
		Facing: math.Mod(facing*180/math.Pi+360, 360),
		Scale:  scale,
	}
	m.OpenDirection, m.OpenDistance = castRays(a, px, py, facing, scale)
	return m
}

// scanAutomap splits one frame into the player arrow and everything else
// that was drawn. The lines come back as pixels rather than as geometry
// because that is all they are wanted for: ray casting and lining two
// frames up against each other.
func scanAutomap(img image.Image) (arrow, lines []image.Point) {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < amBottom && y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			switch classifyAutomap(int(r>>8), int(g>>8), int(b>>8)) {
			case amArrow:
				arrow = append(arrow, image.Pt(x, y))
			case amNothing:
			default:
				lines = append(lines, image.Pt(x, y))
			}
		}
	}
	return arrow, lines
}

// arrowMinPixels is how much white has to be there before it is called
// an arrow. Live captures ran 40 to 62 pixels; well under the smallest
// of those, but far enough above zero to reject a stray highlight.
const arrowMinPixels = 12

// arrowAxis fits the player arrow's long axis, and returns it along with
// the arrow's centre in screen pixels and its length in Doom pixels. The
// axis is only determined up to a half turn -- see ReadAutomap.
func arrowAxis(pts []image.Point) (angle, cx, cy, span float64, ok bool) {
	if len(pts) < arrowMinPixels {
		return 0, 0, 0, 0, false
	}
	n := float64(len(pts))
	for _, p := range pts {
		cx += float64(p.X)
		cy += float64(p.Y)
	}
	cx, cy = cx/n, cy/n

	// Fit in Doom pixels, not screen pixels: the two axes are stretched
	// differently, and an angle fitted on the stretched picture is wrong
	// everywhere except the four cardinals.
	var sxx, syy, sxy float64
	for _, p := range pts {
		dx := (float64(p.X) - cx) / amScreenX
		dy := (float64(p.Y) - cy) / amScreenY
		sxx += dx * dx
		syy += dy * dy
		sxy += dx * dy
	}
	theta := 0.5 * math.Atan2(2*sxy, sxx-syy)
	ux, uy := math.Cos(theta), math.Sin(theta)

	lo, hi := 0.0, 0.0
	for _, p := range pts {
		dx := (float64(p.X) - cx) / amScreenX
		dy := (float64(p.Y) - cy) / amScreenY
		t := dx*ux + dy*uy
		lo, hi = math.Min(lo, t), math.Max(hi, t)
	}
	// Screen y grows downward, Doom's y grows north.
	return math.Atan2(-uy, ux), cx, cy, hi - lo, true
}

// automapPanSearch is how far automapPan looks, in screen pixels. A
// forward press of a couple of hundred milliseconds moves the map by
// well under half this.
const automapPanSearch = 40

// automapPanStride thins the pixel set the search scores against. The
// lines are two to three screen pixels thick, so every third pixel still
// lands on all of them, and the search is a brute-force one.
const automapPanStride = 3

// automapPanMoved is the smallest shift counted as movement, in screen
// pixels. Below this the player is wedged against something and the
// direction of the shift is noise.
const automapPanMoved = 3

// automapPan finds the offset that best lines up two frames' worth of
// drawn lines. Pixels rather than features because the automap moves as
// a rigid translation: Doom draws it north-up, so walking shifts it and
// turning does not change it at all.
func automapPan(before, after []image.Point) (dx, dy int, moved bool) {
	if len(before) == 0 || len(after) == 0 {
		return 0, 0, false
	}
	set := make(map[image.Point]bool, len(before))
	for _, p := range before {
		set[p] = true
	}
	best := -1
	for oy := -automapPanSearch; oy <= automapPanSearch; oy++ {
		for ox := -automapPanSearch; ox <= automapPanSearch; ox++ {
			hit := 0
			for i := 0; i < len(after); i += automapPanStride {
				if set[image.Pt(after[i].X+ox, after[i].Y+oy)] {
					hit++
				}
			}
			if hit > best {
				best, dx, dy = hit, ox, oy
			}
		}
	}
	return dx, dy, dx*dx+dy*dy >= automapPanMoved*automapPanMoved
}

// stayAheadMargin keeps the player going forwards when forwards is
// nearly as open as the best option. Without it they turn on every small
// difference and never cover any ground -- and the way back always
// measures open, because they just walked down it.
const stayAheadMargin = 0.8

// castRays walks outward from the player along amRays bearings, measures
// how far each one gets before meeting a solid wall, and reduces that to
// the direction with the most room in it.
//
// Only one-sided lines stop a ray. The other two colours are two-sided
// -- a step, a ledge, a change in ceiling height, a doorway, a closed
// door -- and the player can walk through most of them, so treating them
// as walls would wall the player in: the frame this was first checked
// against had the player standing directly on a ceiling-change line that
// ran the full width of the room, which blocked every ray in every
// direction. The ones that do block, closed doors especially, are not
// worth special-casing here, because walking into one is how the policy
// finds out it is there and pressing use is what it does about it.
func castRays(img image.Image, px, py, facing, scale float64) (openDir string, openDist float64) {
	type bucket struct {
		free  float64
		count int
	}
	buckets := map[string]*bucket{
		DirAhead: {}, DirLeft: {}, DirRight: {}, DirBehind: {},
	}

	for i := 0; i < amRays; i++ {
		bearing := 2 * math.Pi * float64(i) / amRays
		b := buckets[relativeDirection(bearing, facing)]
		b.free += traceRay(img, px, py, bearing, scale)
		b.count++
	}

	mean := func(dir string) float64 {
		b := buckets[dir]
		if b.count == 0 {
			return 0
		}
		return b.free / float64(b.count)
	}
	best, bestFree := DirAhead, 0.0
	for _, dir := range []string{DirAhead, DirLeft, DirRight, DirBehind} {
		if m := mean(dir); m > bestFree {
			best, bestFree = dir, m
		}
	}
	if mean(DirAhead) >= stayAheadMargin*bestFree {
		best, bestFree = DirAhead, mean(DirAhead)
	}
	return best, bestFree
}

// traceRay steps outward until it meets a solid wall and returns how far
// away it was, in map units, or amRange if it got that far without
// meeting one.
func traceRay(img image.Image, px, py, bearing, scale float64) float64 {
	bounds := img.Bounds()
	dx := math.Cos(bearing) * amScreenX
	dy := -math.Sin(bearing) * amScreenY
	norm := math.Hypot(dx, dy)
	if norm == 0 || scale <= 0 {
		return 0
	}
	dx, dy = dx/norm, dy/norm // one screen pixel per step

	// Start clear of the arrow itself.
	for t := 6.0; ; t++ {
		x := int(px + dx*t)
		y := int(py + dy*t)
		if x < bounds.Min.X+1 || x >= bounds.Max.X-1 || y < bounds.Min.Y+1 || y >= amBottom-1 {
			break
		}
		// A line one screen pixel wide can fall between consecutive
		// steps of a diagonal ray, so look at the step's neighbours too.
		for oy := -1; oy <= 1; oy++ {
			for ox := -1; ox <= 1; ox++ {
				r, g, b, _ := img.At(x+ox, y+oy).RGBA()
				if classifyAutomap(int(r>>8), int(g>>8), int(b>>8)) == amWall {
					return mapDistance(dx*t, dy*t, scale)
				}
			}
		}
		if mapDistance(dx*t, dy*t, scale) >= amRange {
			break
		}
	}
	return amRange
}

func mapDistance(sx, sy, scale float64) float64 {
	return math.Hypot(sx/amScreenX, sy/amScreenY) / scale
}

// relativeDirection buckets an absolute bearing by where it falls
// relative to the way the player is facing.
func relativeDirection(bearing, facing float64) string {
	d := angleDelta(bearing, facing)
	switch {
	case math.Abs(d) <= math.Pi/4:
		return DirAhead
	case math.Abs(d) >= 3*math.Pi/4:
		return DirBehind
	case d > 0:
		return DirLeft
	default:
		return DirRight
	}
}

// angleDelta is a-b wrapped into (-pi, pi].
func angleDelta(a, b float64) float64 {
	return math.Mod(a-b+3*math.Pi, 2*math.Pi) - math.Pi
}

// IsAutomap reports whether a frame is showing the automap rather than
// the 3D view, by looking for the player arrow.
//
// Callers need this because the automap is a toggle and the game turns
// it off behind their back: Doom clears automapactive whenever it loads
// a level (g_game.c), which includes every respawn after a death. A
// glance that assumes its own key press left the map up spends the rest
// of the run reading the 3D view and finding no arrow in it.
func IsAutomap(img image.Image) bool {
	if img == nil {
		return false
	}
	arrow, _ := scanAutomap(img)
	return len(arrow) >= arrowMinPixels
}
