// Command vncharness is a small, dependency-free VNC client for driving
// zcxdoom (or any other VNC server) from a script: take a screenshot, or
// send a sequence of key presses. It cross-compiles to a single static
// binary for Windows, macOS, or Linux -- see tools/README.md.
package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"strings"

	"zcxdoom/tools/rfb"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "screenshot":
		cmdScreenshot(os.Args[2:])
	case "key":
		cmdKey(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  vncharness screenshot -addr host:port -password pw -out file.png
  vncharness key        -addr host:port -password pw -keys "escape,down,return"`)
}

func cmdScreenshot(args []string) {
	fs := flag.NewFlagSet("screenshot", flag.ExitOnError)
	addr := fs.String("addr", "localhost:5901", "VNC server address")
	password := fs.String("password", "idbehold", "VNC password")
	out := fs.String("out", "screenshot.png", "output PNG path")
	fs.Parse(args)

	conn, err := rfb.Connect(*addr, *password)
	if err != nil {
		fatal(err)
	}
	defer conn.Close()

	img, err := conn.Screenshot()
	if err != nil {
		fatal(err)
	}
	f, err := os.Create(*out)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fatal(err)
	}
	b := img.Bounds()
	fmt.Printf("wrote %s (%dx%d, server name %q)\n", *out, b.Dx(), b.Dy(), conn.Name)
}

func cmdKey(args []string) {
	fs := flag.NewFlagSet("key", flag.ExitOnError)
	addr := fs.String("addr", "localhost:5901", "VNC server address")
	password := fs.String("password", "idbehold", "VNC password")
	keys := fs.String("keys", "", "comma-separated key names, e.g. escape,down,return")
	fs.Parse(args)

	if *keys == "" {
		fatal(fmt.Errorf("-keys is required"))
	}

	conn, err := rfb.Connect(*addr, *password)
	if err != nil {
		fatal(err)
	}
	defer conn.Close()

	for _, name := range strings.Split(*keys, ",") {
		name = strings.TrimSpace(name)
		sym, err := rfb.KeysymFor(name)
		if err != nil {
			fatal(err)
		}
		if err := conn.Tap(sym); err != nil {
			fatal(err)
		}
		fmt.Printf("sent %s\n", name)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
