package main

import (
	"os"
	"testing"
	"time"
)

func TestNotifyShutdownSignalsAcceptsInterrupt(t *testing.T) {
	sigCh, stop := notifyShutdownSignals()
	defer stop()

	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}

	select {
	case sig := <-sigCh:
		if sig != os.Interrupt {
			t.Fatalf("received %v, want %v", sig, os.Interrupt)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SIGINT")
	}
}
