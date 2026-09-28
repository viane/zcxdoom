package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// display is the X display number Xvfb serves and everything else --
// x11vnc, DOOM -- connects to.
const display = 99

func startCmd(cmdstring string) {
	parts := strings.Split(cmdstring, " ")
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	err := cmd.Start()
	if err != nil {
		log.Fatalf("The following command failed: \"%v\"\n", cmdstring)
	}
}

// clearStaleXLocks removes the lock file and socket a previous Xvfb left
// behind.
//
// Both live in the container's writable layer, which survives `docker
// stop` followed by `docker start`. On such a restart Xvfb finds them,
// refuses the display with "Server is already active for display 99",
// and exits -- and because nothing here checks that, DOOM is started
// anyway with DISPLAY pointing at a server that no longer exists and
// dies with a segmentation fault, which says nothing about the actual
// cause. Recreating the container worked around it only because that
// throws the writable layer away.
//
// Removing them unconditionally is safe: this process is the only thing
// that ever starts an X server in this container, and at this point it
// has not started one yet, so anything still here is from a previous
// run of the container.
func clearStaleXLocks() {
	for _, path := range []string{
		fmt.Sprintf("/tmp/.X%d-lock", display),
		fmt.Sprintf("/tmp/.X11-unix/X%d", display),
	} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("could not remove stale %s: %v", path, err)
		}
	}
}

// waitForDisplay blocks until Xvfb's socket shows up, so DOOM is not
// started against a display that is not listening yet -- and, if Xvfb
// failed outright, so the reason is reported here rather than surfacing
// later as a segfault inside DOOM.
func waitForDisplay(timeout time.Duration) error {
	socket := fmt.Sprintf("/tmp/.X11-unix/X%d", display)
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(socket); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("X display :%d did not come up within %s (%s never appeared)", display, timeout, socket)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func main() {
	log.Print("Create virtual display")
	clearStaleXLocks()
	startCmd(fmt.Sprintf("/usr/bin/Xvfb :%d -ac -screen 0 640x480x24", display))
	if err := waitForDisplay(10 * time.Second); err != nil {
		log.Fatalf("%v", err)
	}

	startCmd(fmt.Sprintf("x11vnc -geometry 640x480 -forever -shared -usepw -display :%d", display))
	log.Print("You can now connect to it with a VNC viewer at port 5900")

	log.Print("Starting DOOM ...")
	doom := exec.Command("/usr/local/games/psdoom", "-warp", "-E1M1", "-skill", "1", "-nomouse", "-nopsmon")
	doom.Env = append(os.Environ(), fmt.Sprintf("DISPLAY=:%d", display))
	doom.Stdout = os.Stdout
	doom.Stderr = os.Stderr
	doom.Stdin = os.Stdin
	if err := doom.Run(); err != nil {
		log.Printf("DOOM exited: %v", err)
	}
}
