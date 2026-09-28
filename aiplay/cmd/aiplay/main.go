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

	dial := dialer{addr: *addr, password: *password, timeout: *connectTimeout}

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
		go runSystem2(slowConn, dial, s2, *system2Interval, &tacticMu, &tactic)
	} else {
		log.Print("System 2: no Gemini API key given (-system2-apikey or $GEMINI_API_KEY), disabled")
	}

	runSystem1(fastConn, dial, s1, *system1Interval, &tacticMu, &tactic)
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

// dialer reopens a dropped VNC connection. Both loops hold their own
// connection to the game (see this file's package doc), and each needs to
// be able to rebuild it independently.
type dialer struct {
	addr     string
	password string
	timeout  time.Duration
}

// redial closes the dead connection and keeps trying to replace it,
// returning only once it has one.
//
// Retrying the initial connection is not enough on its own: the game
// container can legitimately restart underneath a running aiplay, and
// every screenshot after that fails with "broken pipe" forever. The loop
// kept going and kept logging, so the process stayed up and healthy
// looking while the AI had in fact stopped playing entirely -- observed
// after a `docker restart` of the game container.
//
// This retries indefinitely rather than giving up, matching how the rest
// of aiplay handles a dependency being away: fall back or wait, but keep
// running. connectWithRetry already paces its own attempts, so each pass
// here blocks for up to timeout rather than spinning.
func (d dialer) redial(label string, dead *rfb.Conn) *rfb.Conn {
	if dead != nil {
		dead.Close()
	}
	for {
		conn, err := connectWithRetry(d.addr, d.password, d.timeout)
		if err == nil {
			log.Printf("%s: reconnected to %s", label, d.addr)
			return conn
		}
		log.Printf("%s: reconnect failed, still trying: %v", label, err)
	}
}

func runSystem1(conn *rfb.Conn, d dialer, s1 *system1.Client, interval time.Duration, tacticMu *sync.RWMutex, tactic *string) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var prev image.Image
	framesSinceMove := 0
	n := 0

	// Last two actions, so the loop can tell when the frame it is holding
	// was taken with the view held still at both ends (see
	// doompolicy.IsProbeTick), and lastHealth so a drop can be spotted.
	var prevAction, prevPrevAction doompolicy.Action
	var motionInView *bool
	lastHealth := -1

	for {
		select {
		case <-sigCh:
			log.Print("interrupted, stopping")
			return
		case <-ticker.C:
			curr, err := conn.Screenshot()
			if err != nil {
				log.Printf("system1: screenshot: %v", err)
				conn = d.redial("system1", conn)
				prev = nil // the new connection's first frame has no baseline
				continue
			}

			tacticMu.RLock()
			currentTactic := *tactic
			tacticMu.RUnlock()

			state := perception.Extract(prev, curr, framesSinceMove, currentTactic)
			framesSinceMove = state.FramesSinceMove
			prev = curr

			// Both of the frames just compared were taken while the view
			// was held still, so whatever changed between them moved by
			// itself. Any other tick's DiffScore is dominated by the
			// player's own movement and says nothing about monsters.
			if prevAction == doompolicy.Wait && prevPrevAction == doompolicy.Wait {
				moving := perception.MotionSeen(state)
				motionInView = &moving
			}
			// Sticky: report the last probe's answer until the next one
			// replaces it, rather than dropping the field on every
			// ordinary tick.
			state.MotionInView = motionInView

			if state.Health != nil {
				if lastHealth >= 0 && *state.Health < lastHealth {
					state.TakingDamage = true
				}
				lastHealth = *state.Health
			}

			n++

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			action := doompolicy.Decide(ctx, s1, state, n)
			cancel()

			if action != doompolicy.Wait {
				sym, err := rfb.KeysymFor(action.Keysym())
				if err != nil {
					log.Printf("system1: unknown keysym for action %s: %v", action, err)
					continue
				}
				if err := conn.Tap(sym); err != nil {
					log.Printf("system1: send key: %v", err)
					continue
				}
			}
			prevPrevAction, prevAction = prevAction, action

			if n%20 == 0 {
				log.Printf("tick %d: diff=%.3f brightness=%.2f stuck=%d health=%s moving=%s hurt=%v -> %s",
					n, state.DiffScore, state.MeanBrightness, state.FramesSinceMove,
					intOrUnknown(state.Health), boolOrUnknown(state.MotionInView), state.TakingDamage, action)
			}
		}
	}
}

func runSystem2(conn *rfb.Conn, d dialer, s2 *system2.Client, interval time.Duration, tacticMu *sync.RWMutex, tactic *string) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	history := "(just started)"
	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		img, err := conn.Screenshot()
		if err != nil {
			log.Printf("system2: screenshot: %v", err)
			cancel()
			conn = d.redial("system2", conn)
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

func intOrUnknown(v *int) string {
	if v == nil {
		return "?"
	}
	return fmt.Sprintf("%d", *v)
}

func boolOrUnknown(v *bool) string {
	if v == nil {
		return "?"
	}
	return fmt.Sprintf("%v", *v)
}
