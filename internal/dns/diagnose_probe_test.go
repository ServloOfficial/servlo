package dns

import "testing"

// Regression for the macOS false "servlo-dns container not running" warning:
// defaultContainerRunning must treat the servlo-dns service unit as running
// when the service manager reports it active,
// systemd on linux), instead of only checking systemctl + podman ps.
func TestDefaultContainerRunning_serviceActiveShortCircuits(t *testing.T) {
	prev := serviceActive
	t.Cleanup(func() { serviceActive = prev })

	serviceActive = func(name string) bool { return name == "servlo-dns" }
	if !defaultContainerRunning() {
		t.Error("expected true when the service manager reports servlo-dns active")
	}
}
