//go:build linux

package cli

import "testing"

// isolateState points the servlo state dirs at temp dirs for the duration of a
// test. teardownDNS deletes the servlo-dns quadlet, which without this lands on the
// developer's own install.
func isolateState(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// Disabling DNS must remove the resolver plumbing, not just stop the container.
// Leaving it behind pointed the dispatcher and the interface routes at a dnsmasq
// that is no longer running, and stranded the servlo0 offline link on the host with
// nothing maintaining it and no obvious way for the user to get rid of it. The
// macOS path already tore its /etc/resolver files down here; Linux did not.
func TestTeardownDNS_removesResolverPlumbing(t *testing.T) {
	isolateState(t)
	origTeardown, origConfigured := dnsTeardown, dnsResolverConfigured
	t.Cleanup(func() { dnsTeardown, dnsResolverConfigured = origTeardown, origConfigured })

	called := false
	dnsTeardown = func() { called = true }
	dnsResolverConfigured = func() bool { return true } // servlo did write resolver config

	teardownDNS()

	if !called {
		t.Error("teardownDNS must tear down the resolver config so servlo0 and the dispatcher don't outlive `servlo dns:disable`")
	}
}

// install.go calls teardownDNS on every run where DNS is off, not only on a
// true->false flip. Tearing down unconditionally reverts interfaces and restarts
// NetworkManager on every `servlo install` for someone who never let servlo manage
// DNS, so it has to be gated on servlo having actually written resolver config.
func TestTeardownDNS_skipsWhenServloNeverConfiguredTheResolver(t *testing.T) {
	isolateState(t)
	origTeardown, origConfigured := dnsTeardown, dnsResolverConfigured
	t.Cleanup(func() { dnsTeardown, dnsResolverConfigured = origTeardown, origConfigured })

	called := false
	dnsTeardown = func() { called = true }
	dnsResolverConfigured = func() bool { return false } // servlo never touched the resolver

	teardownDNS()

	if called {
		t.Error("teardownDNS must not revert interfaces and restart NetworkManager on a host where servlo never wrote resolver config")
	}
}
