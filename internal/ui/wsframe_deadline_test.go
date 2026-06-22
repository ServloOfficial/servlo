package ui

import (
	"net"
	"testing"
	"time"
)

// A peer that stops reading must not wedge a frame write forever: writeFrame
// sets a write deadline so conn.Write fails instead of blocking, which is what
// lets the LSP/WS ping and stdout-pump goroutines observe cancel and exit.
func TestWriteFrame_WriteDeadlineUnblocksStuckPeer(t *testing.T) {
	prev := wsWriteTimeout
	wsWriteTimeout = 50 * time.Millisecond
	defer func() { wsWriteTimeout = prev }()

	// net.Pipe is unbuffered and synchronous, so a write blocks until the other
	// end reads. We never read, modelling a stuck/half-open peer.
	srv, cli := net.Pipe()
	defer cli.Close()
	ws := &wsConn{conn: srv}

	done := make(chan error, 1)
	go func() { done <- ws.WriteText([]byte("hello")) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a write-deadline error, got nil (write succeeded against a non-reading peer)")
		}
		if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
			t.Fatalf("expected a timeout net.Error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WriteText blocked past the write deadline; goroutine would leak")
	}
}
