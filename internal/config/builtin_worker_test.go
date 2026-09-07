package config

import "testing"

// IsBuiltinWorker must recognise the servlo-managed worker that lives outside any
// framework's worker definitions, so a validator does not flag it as undefined.
//
// "stripe" is in the negative set on purpose. It was a builtin until S0.6
// deleted the listener, and a site whose .servlo.yaml still lists it should now
// fail validation rather than be waved through as a worker servlo manages.
func TestIsBuiltinWorker(t *testing.T) {
	if !IsBuiltinWorker(HostProxyWorkerName) {
		t.Errorf("IsBuiltinWorker(%q) = false, want true", HostProxyWorkerName)
	}
	for _, name := range []string{"queue", "horizon", "schedule", "vite", "reverb", "stripe", ""} {
		if IsBuiltinWorker(name) {
			t.Errorf("IsBuiltinWorker(%q) = true, want false", name)
		}
	}
}
