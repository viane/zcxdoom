package main

import (
	"strings"
	"testing"
	"time"
)

func TestConnectWithRetryGivesUpAfterTimeout(t *testing.T) {
	start := time.Now()
	_, err := connectWithRetry("127.0.0.1:1", "irrelevant", 2*time.Second)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("connectWithRetry succeeded against a port nothing listens on, want an error")
	}
	if !strings.Contains(err.Error(), "giving up after") {
		t.Errorf("error = %q, want it to mention giving up after retrying", err)
	}
	if elapsed < 2*time.Second {
		t.Errorf("returned after %s, want it to have kept retrying for close to the 2s timeout", elapsed)
	}
	if elapsed > 4*time.Second {
		t.Errorf("returned after %s, want it close to the 2s timeout, not much longer", elapsed)
	}
}
