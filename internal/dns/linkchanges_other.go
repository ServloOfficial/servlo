//go:build !linux

package dns

// LinkChanges is a no-op on non-Linux platforms. macOS containers receive
// DNS from the podman machine VM, so a host-side interface event would
// not change container resolution anyway, and the safety-net poll in
// each watcher covers any rare case we still want to react to. Returns
// nil after done closes so callers can use a single error-checked code
// path across platforms.
func LinkChanges(out chan<- struct{}, done <-chan struct{}) error {
	<-done
	return nil
}
