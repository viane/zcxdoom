package main

import (
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

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

func main() {
	log.Print("Create virtual display")
	startCmd("/usr/bin/Xvfb :99 -ac -screen 0 640x480x24")
	time.Sleep(time.Duration(2) * time.Second)

	startCmd("x11vnc -geometry 640x480 -forever -shared -usepw -display :99")
	log.Print("You can now connect to it with a VNC viewer at port 5900")

	log.Print("Starting DOOM ...")
	doom := exec.Command("/usr/local/games/psdoom", "-warp", "-E1M1", "-skill", "1", "-nomouse", "-nopsmon")
	doom.Env = append(os.Environ(), "DISPLAY=:99")
	doom.Stdout = os.Stdout
	doom.Stderr = os.Stderr
	doom.Stdin = os.Stdin
	if err := doom.Run(); err != nil {
		log.Printf("DOOM exited: %v", err)
	}
}
