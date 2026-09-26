package rfb

import (
	"fmt"
	"strings"
)

// Keysyms maps friendly names to X11 key symbols (from X11's
// keysymdef.h). These values are part of the X11 protocol itself, not
// this project, so they're stable across every VNC server.
var Keysyms = map[string]uint32{
	"escape":    0xff1b,
	"return":    0xff0d,
	"enter":     0xff0d,
	"space":     0x0020,
	"tab":       0xff09,
	"backspace": 0xff08,
	"shift":     0xffe1,
	"ctrl":      0xffe3,
	"control":   0xffe3,
	"alt":       0xffe9,
	"up":        0xff52,
	"down":      0xff54,
	"left":      0xff51,
	"right":     0xff53,
}

// KeysymFor resolves a key name to its X11 keysym. It accepts the names
// in Keysyms (case-insensitive) or a single printable ASCII character
// (e.g. "a", "5"), whose keysym is just its ASCII code.
func KeysymFor(name string) (uint32, error) {
	if k, ok := Keysyms[strings.ToLower(name)]; ok {
		return k, nil
	}
	if r := []rune(name); len(r) == 1 && r[0] >= 0x20 && r[0] < 0x7f {
		return uint32(r[0]), nil
	}
	return 0, fmt.Errorf("unknown key %q", name)
}
