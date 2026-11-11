package main

import (
	"os"
	"os/signal"
	"syscall"
)

// shutdownSignals stop the launcher. SIGINT is delivered by Ctrl-C in a
// terminal; SIGTERM lets process supervisors stop it cleanly as well.
var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// notifyShutdownSignals registers a buffered channel for shutdown signals.
// The returned stop function unregisters the channel.
func notifyShutdownSignals() (<-chan os.Signal, func()) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, shutdownSignals...)
	return sigCh, func() { signal.Stop(sigCh) }
}
