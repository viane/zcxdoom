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

	// The previous action, so forward progress can be attributed to the
	// press that caused it, and lastHealth so a drop can be spotted.
	var prevAction doompolicy.Action
	var motionInView *bool
	var forwardDiffs []float64
	var keys heldKeys
	var motion perception.Motion
	motionAt := 0
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
				keys.forget()
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
			// Report the last probe's answer, but only while it is still
			// about roughly the present. Held indefinitely it goes stale
			// and starts asserting a monster that has long since gone,
			// or missing one that arrived after the last look.
			if motionInView != nil && n-motionAt <= motionMaxAge {
				state.MotionInView = motionInView
				if motion.Seen {
					state.MotionDirection = motion.Direction
				}
			} else {
				motionInView, motion = nil, perception.Motion{}
			}

			// How far the last few forward presses actually got us. Kept
			// here rather than in perception because only the loop knows
			// which action produced which frame.
			if prevAction == doompolicy.Forward {
				forwardDiffs = append(forwardDiffs, state.DiffScore)
				if len(forwardDiffs) > perception.ForwardSamples {
					forwardDiffs = forwardDiffs[1:]
				}
			}
			if blocked, known := perception.ForwardBlocked(forwardDiffs); known {
				state.WallAhead = &blocked
			}

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

			if err := keys.apply(conn, action); err != nil {
				log.Printf("system1: send key: %v", err)
				conn = d.redial("system1", conn)
				keys.forget()
				prev = nil
				continue
			}

			// A probe tick: the keys are up and the view has settled, so
			// take a pair of frames of our own and see whether anything
			// in them moved by itself.
			if action == doompolicy.Wait {
				if m, err := probeMotion(conn); err != nil {
					log.Printf("system1: motion probe: %v", err)
				} else {
					seen := m.Seen
					motion, motionInView, motionAt = m, &seen, n
				}
			}
			// Turning or backing off changes what is in front of the
			// player, so everything measured about the old direction is
			// now about somewhere else. Without this the verdict sticks:
			// it only refreshes on a forward press, and once "blocked"
			// makes the model turn instead of going forward, nothing
			// ever updates it and the player turns on the spot forever.
			switch action {
			case doompolicy.TurnLeft, doompolicy.TurnRight, doompolicy.Back:
				forwardDiffs = forwardDiffs[:0]
			}

			prevAction = action

			if n%20 == 0 {
				log.Printf("tick %d: diff=%.3f stuck=%d health=%s moving=%s%s hurt=%v wall=%s -> %s",
					n, state.DiffScore, state.FramesSinceMove,
					intOrUnknown(state.Health), boolOrUnknown(state.MotionInView), dirSuffix(state.MotionDirection),
					state.TakingDamage, boolOrUnknown(state.WallAhead), action)

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

// motionMaxAge is how many ticks a motion reading stays worth reporting.
// One probe interval plus a little slack: past that it is describing a
// moment that has gone, and a stale "something is moving" is worse than
// admitting we have not looked recently.
const motionMaxAge = doompolicy.ProbeInterval + 1

// Probe timings. settleDelay is how long to wait after releasing the
// keys before looking: measured on a live game, the view is already
// identical 100ms after they come up. probeGap is how far apart the two
// frames are taken -- long enough for a monster to visibly move, short
// enough to fit inside one tick.
const (
	settleDelay = 120 * time.Millisecond
	probeGap    = 150 * time.Millisecond
)

// turnPulse is how long a turn key is held. At Doom's ~123 degrees a
// second this is roughly a 30 degree step: enough to bring something at
// the edge of view into the middle in a couple of decisions, small
// enough that a run of turns does not become a pirouette.
const turnPulse = 250 * time.Millisecond

// probeMotion takes two frames a moment apart with the keys up, so the
// only thing that can differ between them is something that moved on its
// own. Must be called with no keys held.
func probeMotion(conn *rfb.Conn) (perception.Motion, error) {
	time.Sleep(settleDelay)
	a, err := conn.Screenshot()
	if err != nil {
		return perception.Motion{}, err
	}
	time.Sleep(probeGap)
	b, err := conn.Screenshot()
	if err != nil {
		return perception.Motion{}, err
	}
	return perception.ProbeMotion(a, b), nil
}

// heldKeys keeps the current movement key down between decisions.
//
// Tapping a key for 80ms is about three of Doom's own tics, so a decision
// every few hundred milliseconds moved the player a fraction of what
// walking should cover, and the result looked less like bad judgement
// than like wading through treacle. Holding the key until the decision
// changes gives an ordinary walking pace, and matches how the game is
// actually played.
type heldKeys struct {
	down uint32 // currently held keysym, 0 for none
}

// forget drops the record of what is held without sending anything, for
// use after the connection was replaced and the server knows nothing
// about the old key state.
func (h *heldKeys) forget() { h.down = 0 }

func (h *heldKeys) release(conn *rfb.Conn) error {
	if h.down == 0 {
		return nil
	}
	sym := h.down
	h.down = 0
	return conn.SendKey(sym, false)
}

func (h *heldKeys) apply(conn *rfb.Conn, action doompolicy.Action) error {
	switch action {
	case doompolicy.Wait:
		return h.release(conn)

	case doompolicy.TurnLeft, doompolicy.TurnRight:
		// Turning is pulsed rather than held. Doom turns at
		// angleturn[0] = 640 per tic, shifted left 16, so a full circle
		// takes 2^32 / (640<<16) = 102.4 tics -- about 2.9 seconds, or
		// ~123 degrees a second. Held for a whole decision that is ~67
		// degrees, and two or three turn decisions in a row spin the
		// player through a full 360, which is what it looked like in
		// play. A fixed pulse turns by a usable step instead and leaves
		// the key up, so repeated turns step around rather than spin.
		if err := h.release(conn); err != nil {
			return err
		}
		sym, err := rfb.KeysymFor(action.Keysym())
		if err != nil {
			return err
		}
		if err := conn.SendKey(sym, true); err != nil {
			return err
		}
		time.Sleep(turnPulse)
		return conn.SendKey(sym, false)

	case doompolicy.Use:
		// Doors and switches respond to the press, not to how long it is
		// held, and holding it just re-triggers whatever is in front.
		if err := h.release(conn); err != nil {
			return err
		}
		sym, err := rfb.KeysymFor(action.Keysym())
		if err != nil {
			return err
		}
		return conn.Tap(sym)
	}

	sym, err := rfb.KeysymFor(action.Keysym())
	if err != nil {
		return err
	}
	if h.down == sym {
		return nil // already held; leave it down
	}
	if err := h.release(conn); err != nil {
		return err
	}
	h.down = sym
	return conn.SendKey(sym, true)
}

func dirSuffix(dir string) string {
	if dir == "" {
		return ""
	}
	return "(" + dir + ")"
}
