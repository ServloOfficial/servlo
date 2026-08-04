//go:build linux

package cli

// installAutostart is a no-op on Linux — autostart is opt-in via
// `servlo autostart enable` or the web UI toggle.
func installAutostart() {}
