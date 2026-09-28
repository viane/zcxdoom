// Command motionprobe measures what a still frame pair actually looks
// like on a running game, which is where perception's MotionThreshold
// and LightChangeTolerance come from.
//
// Connect it to a live instance while the player is standing still --
// stop the aiplay container first, or it will be walking around and
// every pixel will change -- and it prints, per sample, how much of the
// picture changed, how much the overall brightness moved, and where the
// change was. That last pair is the point: a blinking light sector and a
// monster walking past both change a lot of pixels, and what separates
// them is that the blink changes the room's brightness.
//
// Rerun this if the game, its resolution or the level changes, and move
// the thresholds to match what it reports.
//
//	docker stop windows-local-aiplay-1    # leave the game running
//	go run ./cmd/motionprobe -addr localhost:5901
//
// It lives here rather than in tools/ because it depends on perception,
// and tools/ is in the zcxdoom module that aiplay itself depends on.
package main

import (
	"flag"
	"fmt"
	"time"

	"aiplay/perception"

	"zcxdoom/tools/rfb"
)

func absd(p, q uint32) uint32 {
	if p > q {
		return p - q
	}
	return q - p
}

func main() {
	addr := flag.String("addr", "localhost:5901", "")
	password := flag.String("password", "idbehold", "")
	n := flag.Int("n", 20, "")
	gap := flag.Duration("gap", 300*time.Millisecond, "")
	press := flag.String("press", "", "tap this key before each sample, to measure while driving (e.g. -press up to walk into a wall)")
	settle := flag.Bool("settle", false, "tap -press once, then sample with no further input to see how long the view keeps drifting")
	flag.Parse()

	conn, err := rfb.Connect(*addr, *password)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	if *settle {
		sym, err := rfb.KeysymFor(*press)
		if err != nil {
			panic(err)
		}
		if err := conn.Tap(sym); err != nil {
			panic(err)
		}
		prev, _ := conn.Screenshot()
		for i := 0; i < *n; i++ {
			time.Sleep(*gap)
			cur, err := conn.Screenshot()
			if err != nil {
				panic(err)
			}
			st := perception.Extract(prev, cur, 0, "")
			fmt.Println(fmt.Sprintf("settle +%4dms diff=%.5f", (i+1)*int(gap.Milliseconds()), st.DiffScore))
			prev = cur
		}
		return
	}

	for i := 0; i < *n; i++ {
		if *press != "" {
			sym, err := rfb.KeysymFor(*press)
			if err != nil {
				panic(err)
			}
			if err := conn.Tap(sym); err != nil {
				panic(err)
			}
		}
		a, err := conn.Screenshot()
		if err != nil {
			panic(err)
		}
		time.Sleep(*gap)
		b, err := conn.Screenshot()
		if err != nil {
			panic(err)
		}

		st := perception.Extract(a, b, 0, "")
		sa := perception.Extract(nil, a, 0, "")
		sb := perception.Extract(nil, b, 0, "")

		minX, minY, maxX, maxY, changed := 640, 480, -1, -1, 0
		for y := 0; y < 403; y += 2 {
			for x := 0; x < 640; x += 2 {
				r1, g1, b1, _ := a.At(x, y).RGBA()
				r2, g2, b2, _ := b.At(x, y).RGBA()
				if absd(r1, r2) > 8000 || absd(g1, g2) > 8000 || absd(b1, b2) > 8000 {
					changed++
					if x < minX {
						minX = x
					}
					if x > maxX {
						maxX = x
					}
					if y < minY {
						minY = y
					}
					if y > maxY {
						maxY = y
					}
				}
			}
		}
		box, fill := "none", 0.0
		if maxX >= 0 {
			w, h := maxX-minX+1, maxY-minY+1
			box = fmt.Sprintf("x%d-%d y%d-%d %dx%d", minX, maxX, minY, maxY, w, h)
			fill = float64(changed) / (float64(w) * float64(h) / 4)
		}
		hud := "?"
		if st.Health != nil {
			hud = fmt.Sprintf("%d", *st.Health)
		}
		fmt.Printf("%2d diff=%.5f dBright=%+.4f health=%s fill=%.2f box=%s\n",
			i, st.DiffScore, sb.MeanBrightness-sa.MeanBrightness, hud, fill, box)
		time.Sleep(400 * time.Millisecond)
	}
}
