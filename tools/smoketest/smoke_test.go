// Package smoketest contains end-to-end checks: does zcxdoom actually
// boot and respond to input over VNC?
//
// Each test runs in one of two modes, chosen by the SMOKE_VNC_ADDR
// environment variable, so the same test works whether the container is
// right here or on the other side of the world:
//
//   - SMOKE_VNC_ADDR set: connect to that address and test it. This is
//     "point the test at whatever is already running, wherever it is" --
//     a host, a VM, another container, a real cloud deployment. Nothing
//     here needs Docker, or even this repository, on the machine running
//     the test; only the compiled test binary and network access to that
//     address.
//   - SMOKE_VNC_ADDR unset: build the image and run a throwaway container
//     with `docker` (which must be on PATH), test it, then remove it.
//     This is the "just check my local checkout" / CI path.
package smoketest

import (
	"fmt"
	"image"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"zcxdoom/tools/rfb"
)

// testTarget resolves what to test against: an existing instance named by
// SMOKE_VNC_ADDR, or a fresh throwaway container this function starts and
// arranges to clean up when the test ends.
func testTarget(t *testing.T) (addr, password string) {
	t.Helper()
	password = envOr("SMOKE_VNC_PASSWORD", "idbehold")
	if addr = os.Getenv("SMOKE_VNC_ADDR"); addr != "" {
		return addr, password
	}
	addr = "localhost:55901"
	t.Cleanup(startLocalContainer(t, addr))
	return addr, password
}

func TestDoomBootsAndRespondsOverVNC(t *testing.T) {
	addr, password := testTarget(t)

	// x11vnc/psdoom take a couple of seconds to come up, and (as observed
	// first-hand while building this test) the very first connection or
	// input event right after startup can be silently dropped while
	// those processes are still settling. So: retry the connection...
	conn := connectWithRetry(t, addr, password, 30*time.Second)
	defer conn.Close()

	// ...and don't trust a single screenshot either: keep asking until we
	// see something other than a flat, uninitialized framebuffer.
	before := screenshotUntilNotBlank(t, conn, 10*time.Second)

	// ...and don't trust a single key press: DOOM's own menu toggling is
	// instant once the key lands, so if the screen hasn't changed a
	// moment later, the most likely explanation is that the key event
	// was dropped, not that the game is broken. Retry a few times before
	// concluding input isn't reaching the game at all.
	changed := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := conn.Tap(rfb.Keysyms["escape"]); err != nil {
			t.Fatalf("sending Escape: %v", err)
		}
		time.Sleep(400 * time.Millisecond)
		after, err := conn.Screenshot()
		if err != nil {
			t.Fatalf("screenshot after Escape: %v", err)
		}
		if !imagesRoughlyEqual(before, after) {
			changed = true
			break
		}
	}
	if !changed {
		t.Fatal("screen never changed after pressing Escape repeatedly -- input does not appear to reach the game")
	}
}

// TestWrongPasswordIsRejected guards the authentication path itself: a
// bad password must fail with a clear error, not hang or silently
// "succeed". Exercising this now is what would catch a mistake in the
// DES/challenge-response logic before it ever reaches something that
// matters.
func TestWrongPasswordIsRejected(t *testing.T) {
	addr, _ := testTarget(t)

	// Give the server the same startup grace period as the main test,
	// but expect a clean rejection rather than a connection.
	deadline := time.Now().Add(30 * time.Second)
	var err error
	for time.Now().Before(deadline) {
		var conn *rfb.Conn
		conn, err = rfb.Connect(addr, "definitely-the-wrong-password")
		if conn != nil {
			conn.Close()
		}
		if err != nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err == nil {
		t.Fatal("expected an error connecting with the wrong password, got none")
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func connectWithRetry(t *testing.T, addr, password string, timeout time.Duration) *rfb.Conn {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		conn, err := rfb.Connect(addr, password)
		if err == nil {
			return conn
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("could not connect to %s within %s: %v", addr, timeout, lastErr)
	return nil
}

func screenshotUntilNotBlank(t *testing.T, conn *rfb.Conn, timeout time.Duration) image.Image {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		img, err := conn.Screenshot()
		if err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		if !isBlank(img) {
			return img
		}
		if time.Now().After(deadline) {
			t.Fatal("framebuffer is still a single flat colour after waiting -- DOOM does not appear to be rendering")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// isBlank reports whether an image is a single flat colour (a strong
// signal that nothing has been drawn yet), sampling on a grid rather
// than every pixel since this only needs to be a cheap sanity check.
func isBlank(img image.Image) bool {
	b := img.Bounds()
	first := img.At(b.Min.X, b.Min.Y)
	for y := b.Min.Y; y < b.Max.Y; y += 7 {
		for x := b.Min.X; x < b.Max.X; x += 7 {
			if img.At(x, y) != first {
				return false
			}
		}
	}
	return true
}

// imagesRoughlyEqual compares on the same sampling grid as isBlank: fast,
// and plenty precise enough to tell "the menu appeared" from "nothing
// happened".
func imagesRoughlyEqual(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	bnd := a.Bounds()
	for y := bnd.Min.Y; y < bnd.Max.Y; y += 5 {
		for x := bnd.Min.X; x < bnd.Max.X; x += 5 {
			if a.At(x, y) != b.At(x, y) {
				return false
			}
		}
	}
	return true
}

// startLocalContainer builds the image and runs it under a disposable
// name, returning a cleanup function that stops and removes it. It shells
// out to `docker` rather than using a client library, which keeps this
// test dependency-free and working identically on Windows, macOS, and
// Linux -- wherever `docker` itself runs.
func startLocalContainer(t *testing.T, addr string) func() {
	t.Helper()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("parsing %q: %v", addr, err)
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("finding repo root: %v", err)
	}

	name := fmt.Sprintf("zcxdoom-smoke-%d", os.Getpid())

	build := exec.Command("docker", "build", "-t", "zcxdoom:smoke", ".")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("docker build: %v\n%s", err, out)
	}

	run := exec.Command("docker", "run", "-d", "--rm", "--name", name,
		"-p", port+":5900", "zcxdoom:smoke")
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("docker run: %v\n%s", err, out)
	}

	return func() {
		exec.Command("docker", "rm", "-f", name).Run()
	}
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no Dockerfile found above %s", dir)
		}
		dir = parent
	}
}
