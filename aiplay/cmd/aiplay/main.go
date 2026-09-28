// Command aiplay plays zcxdoom over VNC: a fast System-1 loop decides the
// next key to press many times a second, while a slower System-2 call
// (a real multimodal LLM) periodically looks at the screen and sets the
// tactic System 1 steers by in between. See aiplay/README.md.
//
// It opens two independent VNC connections -- one for each system -- which
// only works because the game's x11vnc server runs with -shared. Splitting
// them this way, rather than sharing one *rfb.Conn behind a mutex, means
// System 2's occasional slow screenshot can never block or interleave with
// System 1's fast decision loop.
package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"log"
	"os"
	"os/signal"
	"sync"
	"time"

	"aiplay/doompolicy"
	"aiplay/perception"
	"aiplay/system1"
	"aiplay/system2"

	"zcxdoom/tools/rfb"
)

func main() {
	addr := flag.String("addr", "localhost:5901", "VNC server address")
	password := flag.String("password", "idbehold", "VNC password")
	system1URL := flag.String("system1-url", "", "base URL of a Kev/Jev-compatible System-1 server (e.g. http://localhost:8000); empty uses the built-in fallback policy only")
	system1Interval := flag.Duration("system1-interval", 300*time.Millisecond, "how often System 1 decides the next action")
	geminiKey := flag.String("system2-apikey", os.Getenv("GEMINI_API_KEY"), "Gemini API key for System 2 (defaults to $GEMINI_API_KEY); empty disables System 2")
	system2Model := flag.String("system2-model", "", "Gemini model for System 2 (default gemini-2.5-flash)")
	system2Interval := flag.Duration("system2-interval", 30*time.Second, "how often System 2 re-evaluates tactics")
	connectTimeout := flag.Duration("connect-timeout", 30*time.Second, "how long to keep retrying the initial VNC connection before giving up (the game container may still be starting)")
	flag.Parse()

	fastConn, err := connectWithRetry(*addr, *password, *connectTimeout)
	if err != nil {
		log.Fatalf("connect (system1): %v", err)
	}
	defer fastConn.Close()

	var s1 *system1.Client
	if *system1URL != "" {
		s1 = system1.New(*system1URL)
		log.Printf("System 1: %s", *system1URL)
	} else {
		log.Print("System 1: no -system1-url given, using the built-in fallback policy only")
	}

	var tacticMu sync.RWMutex
	tactic := "explore and fight anything you see"

	if *geminiKey != "" {
		slowConn, err := connectWithRetry(*addr, *password, *connectTimeout)
		if err != nil {
			log.Fatalf("connect (system2): %v", err)
		}
		defer slowConn.Close()
		s2 := system2.New(*geminiKey, *system2Model)
		log.Printf("System 2: Gemini model %s, every %s", s2.Model, *system2Interval)
		go runSystem2(slowConn, s2, *system2Interval, &tacticMu, &tactic)
	} else {
		log.Print("System 2: no Gemini API key given (-system2-apikey or $GEMINI_API_KEY), disabled")
	}

	runSystem1(fastConn, s1, *system1Interval, &tacticMu, &tactic)
}

// connectWithRetry keeps trying to connect until it succeeds or timeout
// elapses. A single failed attempt is expected, not exceptional: in any
// orchestrated deployment (Docker Compose, Kubernetes, or just starting
// aiplay slightly too eagerly by hand) the game container can easily still
// be booting -- Xvfb and x11vnc take a couple of seconds -- when aiplay's
// own container starts. Retrying here means aiplay doesn't need Compose
// health-check ordering or any other coordination to work correctly.
func connectWithRetry(addr, password string, timeout time.Duration) (*rfb.Conn, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	attempt := 0
	for {
		attempt++
		conn, err := rfb.Connect(addr, password)
		if err == nil {
			if attempt > 1 {
				log.Printf("connected to %s after %d attempts", addr, attempt)
			}
			return conn, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("giving up after %d attempts over %s: %w", attempt, timeout, lastErr)
		}
		if attempt == 1 {
			log.Printf("waiting for %s to accept connections: %v", addr, err)
		}
		time.Sleep(time.Second)
	}
}

func runSystem1(conn *rfb.Conn, s1 *system1.Client, interval time.Duration, tacticMu *sync.RWMutex, tactic *string) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var prev image.Image
	framesSinceMove := 0
	n := 0

	for {
		select {
		case <-sigCh:
			log.Print("interrupted, stopping")
			return
		case <-ticker.C:
			curr, err := conn.Screenshot()
			if err != nil {
				log.Printf("system1: screenshot: %v", err)
				continue
			}

			tacticMu.RLock()
			currentTactic := *tactic
			tacticMu.RUnlock()

			state := perception.Extract(prev, curr, framesSinceMove, currentTactic)
			framesSinceMove = state.FramesSinceMove
			prev = curr

			n++

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			action := doompolicy.Decide(ctx, s1, state, n)
			cancel()

			sym, err := rfb.KeysymFor(action.Keysym())
			if err != nil {
				log.Printf("system1: unknown keysym for action %s: %v", action, err)
				continue
			}
			if err := conn.Tap(sym); err != nil {
				log.Printf("system1: send key: %v", err)
				continue
			}

			if n%20 == 0 {
				log.Printf("tick %d: diff=%.3f brightness=%.2f stuck=%d -> %s", n, state.DiffScore, state.MeanBrightness, state.FramesSinceMove, action)
			}
		}
	}
}

func runSystem2(conn *rfb.Conn, s2 *system2.Client, interval time.Duration, tacticMu *sync.RWMutex, tactic *string) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	history := "(just started)"
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		img, err := conn.Screenshot()
		if err != nil {
			log.Printf("system2: screenshot: %v", err)
			cancel()
			continue
		}

		newTactic, err := s2.Tactic(ctx, img, history)
		cancel()
		if err != nil {
			log.Printf("system2: %v (keeping previous tactic)", err)
			continue
		}

		tacticMu.Lock()
		*tactic = newTactic
		tacticMu.Unlock()
		history = newTactic
		log.Printf("system2: new tactic: %s", newTactic)
	}
}
