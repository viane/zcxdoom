// Command automapprobe runs perception's automap glance against a live
// game and prints what it read, which is where the glance's timings and
// perception's automap testdata both come from.
//
// It is the same sequence aiplay uses: show the map, grab a frame, nudge
// forward, grab another, hide the map again. The nudge is not optional
// -- see perception.ReadAutomap for why the arrow alone cannot say which
// way the player is facing.
//
// Stop the aiplay container first, or it will be driving at the same time:
//
//	docker stop windows-local-aiplay-1
//	go run ./cmd/automapprobe -addr localhost:5901 -n 8
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"time"

	"aiplay/perception"

	"zcxdoom/tools/rfb"
)

func main() {
	addr := flag.String("addr", "localhost:5901", "")
	password := flag.String("password", "idbehold", "")
	n := flag.Int("n", 5, "number of glances")
	gap := flag.Duration("gap", 1500*time.Millisecond, "wait between glances")
	turn := flag.Duration("turn", 0, "hold the turn key this long between glances, to sample several headings")
	save := flag.String("save", "", "also write each glance's frame pair to PREFIX-NN-a.png and -b.png")
	flag.Parse()

	conn, err := rfb.Connect(*addr, *password)
	if err != nil {
		fatal(err)
	}
	defer conn.Close()

	for i := 0; i < *n; i++ {
		if i > 0 {
			if *turn > 0 {
				hold(conn, "right", *turn)
			}
			time.Sleep(*gap)
		}
		m, pair, err := glance(conn)
		if err != nil {
			fatal(err)
		}
		if !m.Known {
			fmt.Printf("%2d: no reading (arrow missing, or the player did not move)\n", i)
			continue
		}
		fmt.Printf("%2d: facing %5.1f deg  scale %.3f px/unit  most open: %-6s at %.0f units\n",
			i, m.Facing, m.Scale, m.OpenDirection, m.OpenDistance)

		if *save == "" {
			continue
		}
		for suffix, img := range map[string]image.Image{"a": pair[0], "b": pair[1]} {
			path := fmt.Sprintf("%s-%02d-%s.png", *save, i, suffix)
			if err := writePNG(path, img); err != nil {
				fatal(err)
			}
			fmt.Println("    wrote", path)
		}
	}
}

// nudge is how long the glance holds a movement key to make the map
// scroll. Long enough to shift it well past the few pixels automapPan
// needs, short enough not to walk the player into anything while the 3D
// view is hidden.
const nudge = 250 * time.Millisecond

// settle is how long the view keeps drifting after a movement key comes
// back up -- measured with motionprobe, which found the picture already
// identical 100ms later.
const settle = 120 * time.Millisecond

// glance is the sequence aiplay runs: map up, frame, nudge, frame, map
// down. The waits are what a live game needed -- the map is drawn on the
// next frame after the key, and the view keeps drifting for about a
// tenth of a second after a movement key comes back up.
//
// Two things here are not obvious. The map is put up by checking rather
// than by pressing once, because Doom turns it off by itself on every
// respawn. And a glance that finds the player wedged tries again
// backwards: pressing forward against a wall moves nobody, and a glance
// with no movement in it cannot say which way the player is facing --
// which is exactly the moment the policy most wants to know.
func glance(conn *rfb.Conn) (perception.Automap, [2]image.Image, error) {
	var none [2]image.Image
	a, err := showAutomap(conn)
	if err != nil {
		return perception.Automap{}, none, err
	}
	defer tap(conn, "tab")
	if a == nil {
		return perception.Automap{}, none, nil
	}

	hold(conn, "up", nudge)
	time.Sleep(settle)
	b, err := conn.Screenshot()
	if err != nil {
		return perception.Automap{}, none, err
	}
	if m := perception.ReadAutomap(a, b); m.Known {
		return m, [2]image.Image{a, b}, nil
	}

	// Wedged. Stepping back from b lands where a forward step from there
	// would return to b, so the pair reads the same way round.
	hold(conn, "down", nudge)
	time.Sleep(settle)
	c, err := conn.Screenshot()
	if err != nil {
		return perception.Automap{}, none, err
	}
	return perception.ReadAutomap(c, b), [2]image.Image{c, b}, nil
}

// showAutomap leaves the automap up and returns the first frame of it,
// or nil if it would not come up.
const mapDrawDelay = 250 * time.Millisecond

func showAutomap(conn *rfb.Conn) (image.Image, error) {
	for try := 0; try < 2; try++ {
		if err := tap(conn, "tab"); err != nil {
			return nil, err
		}
		time.Sleep(mapDrawDelay)
		img, err := conn.Screenshot()
		if err != nil {
			return nil, err
		}
		if perception.IsAutomap(img) {
			return img, nil
		}
	}
	return nil, nil
}

func tap(conn *rfb.Conn, name string) error {
	sym, err := rfb.KeysymFor(name)
	if err != nil {
		return err
	}
	return conn.Tap(sym)
}

func hold(conn *rfb.Conn, name string, d time.Duration) {
	sym, err := rfb.KeysymFor(name)
	if err != nil {
		fatal(err)
	}
	if err := conn.SendKey(sym, true); err != nil {
		fatal(err)
	}
	time.Sleep(d)
	if err := conn.SendKey(sym, false); err != nil {
		fatal(err)
	}
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "automapprobe:", err)
	os.Exit(1)
}
