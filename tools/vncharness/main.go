// Command vncharness is a small, dependency-free VNC client for driving
// zcxdoom (or any other VNC server) from a script: take a screenshot, or
// send a sequence of key presses. It cross-compiles to a single static
// binary for Windows, macOS, or Linux -- see tools/README.md.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

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
	case "record":
		cmdRecord(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  vncharness screenshot -addr host:port -password pw -out file.png
  vncharness key        -addr host:port -password pw -keys "escape,down,return"
  vncharness record     -addr host:port -password pw -out file.mp4 -fps 10 -duration 30s`)
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

// cmdRecord captures a live sequence of screenshots and turns them into a
// video. It shells out to ffmpeg if it's on PATH (ubiquitous, but not part
// of this project's stdlib-only, dependency-free design), and falls back to
// writing a plain PNG sequence otherwise so the command still fully works
// with nothing extra installed -- just with one more manual step to finish.
func cmdRecord(args []string) {
	fs := flag.NewFlagSet("record", flag.ExitOnError)
	addr := fs.String("addr", "localhost:5901", "VNC server address")
	password := fs.String("password", "idbehold", "VNC password")
	out := fs.String("out", "recording.mp4", "output video path (or frame directory if ffmpeg isn't available)")
	fps := fs.Float64("fps", 10, "frames captured per second")
	duration := fs.Duration("duration", 0, "how long to record (0 = until interrupted with Ctrl-C)")
	fs.Parse(args)

	if *fps <= 0 {
		fatal(fmt.Errorf("-fps must be positive"))
	}

	conn, err := rfb.Connect(*addr, *password)
	if err != nil {
		fatal(err)
	}
	defer conn.Close()

	if ffmpegPath, err := exec.LookPath("ffmpeg"); err == nil {
		recordWithFfmpeg(conn, ffmpegPath, *out, *fps, *duration)
	} else {
		recordFrameSequence(conn, *out, *fps, *duration)
	}
}

func recordWithFfmpeg(conn *rfb.Conn, ffmpegPath, out string, fps float64, duration time.Duration) {
	cmd := exec.Command(ffmpegPath,
		"-y",
		"-f", "image2pipe",
		"-vcodec", "png",
		"-framerate", fmt.Sprintf("%g", fps),
		"-i", "-",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		out,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fatal(err)
	}

	start := time.Now()
	n, captureErr := captureLoop(conn, fps, duration, func(img image.Image) error {
		return png.Encode(stdin, img)
	})
	elapsed := time.Since(start)
	stdin.Close()
	waitErr := cmd.Wait()

	if captureErr != nil {
		fatal(captureErr)
	}
	if waitErr != nil {
		fatal(fmt.Errorf("ffmpeg: %w", waitErr))
	}
	fmt.Printf("wrote %s (%d frames, %.1fs, avg %.1f fps)\n", out, n, elapsed.Seconds(), float64(n)/elapsed.Seconds())
}

func recordFrameSequence(conn *rfb.Conn, dir string, fps float64, duration time.Duration) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fatal(err)
	}
	fmt.Fprintln(os.Stderr, "ffmpeg not found on PATH; writing a PNG sequence instead")

	i := 0
	start := time.Now()
	n, err := captureLoop(conn, fps, duration, func(img image.Image) error {
		i++
		f, ferr := os.Create(filepath.Join(dir, fmt.Sprintf("frame-%06d.png", i)))
		if ferr != nil {
			return ferr
		}
		defer f.Close()
		return png.Encode(f, img)
	})
	elapsed := time.Since(start)
	if err != nil {
		fatal(err)
	}
	pattern := filepath.Join(dir, "frame-%06d.png")
	fmt.Printf("wrote %d frames to %s (%.1fs, avg %.1f fps)\n", n, dir, elapsed.Seconds(), float64(n)/elapsed.Seconds())
	fmt.Printf("assemble with: ffmpeg -framerate %g -i %s -pix_fmt yuv420p out.mp4\n", fps, pattern)
}

// captureLoop takes screenshots at fps until duration elapses (or forever,
// if duration is 0, until interrupted), handing each frame to emit in order.
func captureLoop(conn *rfb.Conn, fps float64, duration time.Duration, emit func(image.Image) error) (int, error) {
	interval := time.Duration(float64(time.Second) / fps)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	var deadline <-chan time.Time
	if duration > 0 {
		timer := time.NewTimer(duration)
		defer timer.Stop()
		deadline = timer.C
	}

	n := 0
	for {
		select {
		case <-ticker.C:
			img, err := conn.Screenshot()
			if err != nil {
				return n, fmt.Errorf("screenshot %d: %w", n, err)
			}
			if err := emit(img); err != nil {
				return n, fmt.Errorf("frame %d: %w", n, err)
			}
			n++
		case <-deadline:
			return n, nil
		case <-sigCh:
			return n, nil
		}
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
