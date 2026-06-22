//go:build darwin || linux

package dns

import (
	"errors"
	"testing"
)

// errUnlessDone surfaces a genuine read error so the watcher logs once and
// falls back to its poll, but stays silent when the error is just the in-flight
// read being interrupted by a requested shutdown (done closed).
func TestErrUnlessDone(t *testing.T) {
	boom := errors.New("boom")

	open := make(chan struct{})
	if got := errUnlessDone(open, boom); got != boom {
		t.Errorf("open done: got %v, want the error surfaced", got)
	}

	closed := make(chan struct{})
	close(closed)
	if got := errUnlessDone(closed, boom); got != nil {
		t.Errorf("closed done: got %v, want nil (clean shutdown)", got)
	}
}
