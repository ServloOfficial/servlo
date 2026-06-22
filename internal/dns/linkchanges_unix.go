//go:build darwin || linux

package dns

// errUnlessDone returns nil when done is closed, since a requested shutdown
// makes the in-flight socket read fail and that failure is expected. Otherwise
// it returns err so the caller logs a one-shot warning and falls back to its
// time-based poll, rather than silently going deaf to host network changes.
func errUnlessDone(done <-chan struct{}, err error) error {
	select {
	case <-done:
		return nil
	default:
		return err
	}
}
