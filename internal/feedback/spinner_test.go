package feedback

import (
	"bytes"
	"strings"
	"testing"
)

// TestStep_ColorEnabledBetweenStartAndFinish_NoPanic guards the landmine where
// finish() re-read Animated() independently of start(): a Step begun in plain
// mode (nil stop channel) would panic on close(nil) if color turned on before
// it finished. The animated decision must be snapshotted at Start.
func TestStep_ColorEnabledBetweenStartAndFinish_NoPanic(t *testing.T) {
	var buf bytes.Buffer
	restore := SetTestWriter(&buf) // forces plain mode: no spinner, stop is nil
	defer restore()

	s := Start("doing thing")
	colorOn.Store(true) // color flips on mid-flight
	s.OK("done")        // must not panic on a nil stop channel
}

// TestStep_ColorDisabledBetweenStartAndFinish_NoLeak is the mirror: a Step begun
// animated must still close its spinner on finish even if color flips off, so
// the goroutine and ticker don't leak.
func TestStep_ColorDisabledBetweenStartAndFinish_NoLeak(t *testing.T) {
	var buf bytes.Buffer
	withAnimatedBuffer(t, &buf)

	s := Start("doing thing")
	colorOn.Store(false) // color flips off mid-flight
	done := make(chan struct{})
	go func() { s.OK("done"); close(done) }()
	<-done // finish must return (it joins the spinner goroutine), not block forever
}

// TestStep_WarnNotClobberedBySpinner verifies a Warn emitted while an animated
// Step spins is routed through the step's Interrupt, so the spinner's in-place
// redraw doesn't overwrite it.
func TestStep_WarnNotClobberedBySpinner(t *testing.T) {
	var buf bytes.Buffer
	withAnimatedBuffer(t, &buf)

	// Register the step as the active spinner without launching the real
	// goroutine, so reading buf doesn't race the spinner (mirrors the Live test).
	s := &Step{msg: "doing thing", animated: true}
	prev := pushActive(s)
	t.Cleanup(func() { popActive(prev) })

	buf.Reset()
	Warn("could not start %s: boom", "mysql")

	got := buf.String()
	if !strings.Contains(got, "could not start mysql") {
		t.Fatalf("warning text missing: %q", got)
	}
	if !strings.Contains(got, "\r\033[2K") {
		t.Fatalf("warn did not pause/clear the step line: %q", got)
	}
}
