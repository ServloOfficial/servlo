package dns

import "time"

// DebounceEvents collapses a burst on in into a single emit on out after
// wait of silence. The first event after silence starts the timer; every
// later event during the window resets it. done closes shut the goroutine
// down on caller exit. Used by both the lerd-watcher and lerd-ui processes
// to smooth the kernel's RTM_NEWLINK / RTM_NEWADDR burst that follows a
// single VPN connect or disconnect into one settled reaction.
func DebounceEvents(in <-chan struct{}, out chan<- struct{}, wait time.Duration, done <-chan struct{}) {
	var timer *time.Timer
	var timerC <-chan time.Time
	arm := func() {
		if timer == nil {
			timer = time.NewTimer(wait)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(wait)
		}
		timerC = timer.C
	}
	for {
		select {
		case <-done:
			if timer != nil {
				timer.Stop()
			}
			return
		case <-in:
			arm()
		case <-timerC:
			timerC = nil
			select {
			case out <- struct{}{}:
			default:
			}
		}
	}
}
