package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A browser on the servlo host may reach the dashboard by a name that is not
// "localhost": Debian and Ubuntu map the machine's hostname to 127.0.1.1, and
// /etc/hosts aliases are common. Those requests must keep working, or the local
// user is locked out of their own dashboard with no credential that helps.
func TestLocalControlAcceptsLoopbackHostnames(t *testing.T) {
	setupConfigDir(t, "", "") // no credentials: only local works

	for _, tc := range []struct{ name, peer, host string }{
		{"localhost", "127.0.0.1:54321", "localhost:7073"},
		{"loopback ip", "127.0.0.1:54321", "127.0.0.1:7073"},
		{"nginx vhost", "127.0.0.1:54321", "servlo.localhost"},
		{"machine hostname", "127.0.1.1:54321", "workstation:7073"},
		{"hosts alias", "127.0.0.1:54321", "dev.internal"},
		{"ipv6 loopback", "[::1]:54321", "[::1]:7073"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := &nextHandler{}
			req := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
			req.RemoteAddr = tc.peer
			req.Host = tc.host
			rec := httptest.NewRecorder()
			withCrossOriginGate(next).ServeHTTP(rec, req)

			if !next.called {
				t.Errorf("local request with Host %q was blocked (status %d)", tc.host, rec.Code)
			}
		})
	}
}

// A reverse proxy relaying a remote browser also connects from 127.0.0.1. The
// forwarding headers it adds are what separates it from a local browser, and
// neither one gets in without a session.
func TestLocalControlRejectsForwardedRequests(t *testing.T) {
	for _, header := range proxyHeaders {
		t.Run(header, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_DATA_HOME", t.TempDir())

			next := &nextHandler{}
			req := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
			req.RemoteAddr = "127.0.0.1:54321"
			req.Host = "dashboard.example.ts.net"
			req.Header.Set(header, "203.0.113.7")
			rec := httptest.NewRecorder()
			panelStack(t, next).ServeHTTP(rec, req)

			if next.called {
				t.Errorf("%s did not stop a proxied request from reaching the panel", header)
			}
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
			// The distinction itself has to survive, because S5.4 still needs
			// it to tell a local operator from a proxied browser.
			if isLocalControlRequest(req) {
				t.Errorf("%s still counts as local control", header)
			}
		})
	}
}

// The unix socket carries no peer address and no forwarding headers; it is
// reached only by host processes, so it stays authoritative.
func TestLocalControlAcceptsUnixSocketWithForeignHost(t *testing.T) {
	setupConfigDir(t, "", "")

	next := &nextHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
	req.RemoteAddr = ""
	req.Host = "servlo.localhost"
	req = req.WithContext(context.WithValue(req.Context(), ctxKeyUnixSocket{}, true))
	rec := httptest.NewRecorder()
	withCrossOriginGate(next).ServeHTTP(rec, req)

	if !next.called {
		t.Fatalf("unix-socket request was blocked (status %d)", rec.Code)
	}
}

// The panel listens on 0.0.0.0:7073, so anything that makes a request count as
// local is reachable from wherever that port is. An X-Servlo-Trust header
// matching a per-install token used to be one, for a vhost that reached the
// panel over the podman bridge and whose requests therefore arrived from a
// non-loopback address that needed something to vouch for them. No vhost servlo
// writes has injected the header since, so what was left was a way in from
// anywhere for whoever learned one file.
func TestLocalControl_TrustHeaderBuysNothing(t *testing.T) {
	setupConfigDir(t, "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/internal/notify", nil)
	req.RemoteAddr = "203.0.113.9:41234"
	req.Host = "panel.example:7073"
	req.Header.Set("X-Servlo-Trust", "any-value-at-all")

	if isLoopbackRequest(req) {
		t.Error("a trust header still makes a remote request count as loopback")
	}
	if isLocalControlRequest(req) {
		t.Error("a trust header still buys control of the host")
	}
}
