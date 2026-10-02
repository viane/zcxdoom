// Command snap captures frames off a running game and writes them as PNG
// files, optionally pressing keys first.
//
// It is the calibration companion to motionprobe: where that one measures
// how much a frame changed, this one gets the actual picture out so a
// perception reader can be written against it and the frame kept as
// testdata. Every testdata file under perception/ was captured this way.
//
// Stop the aiplay container first, or it will be pressing its own keys
// while this one presses these:
//
//	docker stop windows-local-aiplay-1
//	go run ./cmd/snap -addr localhost:5901 -keys tab -out automap.png
//
// Keys are the rfb.KeysymFor names, comma-separated, tapped in order
// before the delay and the capture.
package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"strings"
	"time"

	"zcxdoom/tools/rfb"
)

func main() {
	addr := flag.String("addr", "localhost:5901", "")
	password := flag.String("password", "idbehold", "")
	keys := flag.String("keys", "", "comma-separated keys to tap before capturing (e.g. tab)")
	hold := flag.String("hold", "", "comma-separated key:duration to hold down before capturing (e.g. equal:1s to zoom the automap in)")
	out := flag.String("out", "snap.png", "file to write; with -n>1, a -NN suffix is added")
	n := flag.Int("n", 1, "number of frames to capture")
	gap := flag.Duration("gap", 300*time.Millisecond, "wait between frames when -n>1")
	delay := flag.Duration("delay", 500*time.Millisecond, "wait after the key presses before the first capture")
	flag.Parse()

	conn, err := rfb.Connect(*addr, *password)
	if err != nil {
		fatal(err)
	}
	defer conn.Close()

	for _, name := range strings.Split(*keys, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		sym, err := rfb.KeysymFor(name)
		if err != nil {
			fatal(err)
		}
		if err := conn.Tap(sym); err != nil {
			fatal(err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, spec := range strings.Split(*hold, ",") {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		name, dur, ok := strings.Cut(spec, ":")
		if !ok {
			fatal(fmt.Errorf("-hold %q: want key:duration", spec))
		}
		d, err := time.ParseDuration(dur)
		if err != nil {
			fatal(err)
		}
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
	time.Sleep(*delay)

	for i := 0; i < *n; i++ {
		if i > 0 {
			time.Sleep(*gap)
		}
		img, err := conn.Screenshot()
		if err != nil {
			fatal(err)
		}
		path := *out
		if *n > 1 {
			path = fmt.Sprintf("%s-%02d.png", strings.TrimSuffix(*out, ".png"), i)
		}
		f, err := os.Create(path)
		if err != nil {
			fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			fatal(err)
		}
		if err := f.Close(); err != nil {
			fatal(err)
		}
		b := img.Bounds()
		fmt.Printf("wrote %s (%dx%d)\n", path, b.Dx(), b.Dy())
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "snap:", err)
	os.Exit(1)
}
