//go:build linux

package unitlog

// IsContainerUnit returns true on Linux — all lerd units run as Podman
// containers, so their logs come from `podman logs` / the journal.
func IsContainerUnit(_ string) bool { return true }
