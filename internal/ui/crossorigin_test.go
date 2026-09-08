package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
)

// setupConfigDir points config.LoadGlobal at a temp dir, optionally writing a
// config.yaml with the given UI credentials. Those are the credentials an
// install carried before session authentication; empty ones leave the config
// absent, which is what any install written since looks like.
func setupConfigDir(t *testing.T, username, plainPassword string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	cfg := map[string]any{}
	ui := map[string]any{}
	if username != "" || plainPassword != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(plainPassword), bcrypt.MinCost)
		if err != nil {
			t.Fatalf("bcrypt: %v", err)
		}
		ui["username"] = username
		ui["password_hash"] = string(hash)
	}
	if len(ui) > 0 {
		cfg["ui"] = ui
	}
	if len(cfg) == 0 {
		return
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	dir := filepath.Join(tmp, "servlo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// nextHandler is a stub downstream handler that records whether it was
// called and writes a 200 OK with a marker body.
type nextHandler struct {
	called bool
}

func (n *nextHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n.called = true
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func TestCrossOriginGate_loopbackBypassesEverything(t *testing.T) {
	setupConfigDir(t, "alice", "s3cret")

	next := &nextHandler{}
	gate := withCrossOriginGate(next)

	req := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Host = "localhost:7073"
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if !next.called {
		t.Error("loopback request did not reach next handler")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("loopback status = %d, want 200", rec.Code)
	}
}

// A state-changing request from off the machine, carrying valid credentials
// and the CSRF header, reaches the handler. This is the whole point of the
// gate: it is an authentication check, not a source-address check.
func TestCrossOriginGate_authenticatedRemoteRequestReachesTheHandler(t *testing.T) {
	setupConfigDir(t, "alice", "s3cret")

	next := &nextHandler{}
	gate := withCrossOriginGate(next)

	req := httptest.NewRequest(http.MethodPost, "/api/sites", http.NoBody)
	req.RemoteAddr = "192.168.1.42:54321"
	req.Host = "robotbox.example.net"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	req.SetBasicAuth("alice", "s3cret")
	req.Header.Set("X-Servlo-CSRF", "1")
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if !next.called {
		t.Fatalf("an authenticated remote POST was turned away with %d", rec.Code)
	}
}

// Every dashboard route is one a remote session is meant to drive, which is
// the whole of what this gate now has to say about where a request came from.
func TestCrossOriginGateAuthenticatedDashboardUsesOrdinaryRoutes(t *testing.T) {
	setupConfigDir(t, "alice", "s3cret")
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	gate := withCrossOriginGate(next)

	for _, path := range []string{
		"/api/servlo/start",
		"/api/sites/myapp.test/secure",
		"/api/sites/myapp.test/restart",
		"/api/services/mysql/restart",
		"/api/php-versions/8.4/rebuild",
	} {
		t.Run(path, func(t *testing.T) {
			called = false
			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.RemoteAddr = "192.168.1.42:54321"
			req.Host = "robotbox.example.net"
			req.Header.Set("X-Forwarded-For", "203.0.113.7")
			req.Header.Set("X-Forwarded-For", "203.0.113.7")
			req.SetBasicAuth("alice", "s3cret")
			req.Header.Set("X-Servlo-CSRF", "1")
			rec := httptest.NewRecorder()
			gate.ServeHTTP(rec, req)

			if !called || rec.Code != http.StatusOK {
				t.Fatalf("authenticated dashboard route %s: called=%v status=%d", path, called, rec.Code)
			}
		})
	}
}

func TestCrossOriginGate_optionsBypassesAuth(t *testing.T) {
	setupConfigDir(t, "alice", "s3cret")

	next := &nextHandler{}
	gate := withCrossOriginGate(next)

	req := httptest.NewRequest(http.MethodOptions, "/api/sites", nil)
	req.RemoteAddr = "192.168.1.42:54321" // LAN, no auth header
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if !next.called {
		t.Error("OPTIONS preflight blocked — CORS will fail")
	}
}

// Unix socket connections must be treated as loopback. The servlo.localhost
// nginx vhost reaches servlo-panel over the bind-mounted socket, and the request
// arrives with a non-IP RemoteAddr ("@"). Without the ctxKeyUnixSocket
// fast-path, the gate would 403 it the same as a LAN client and the
// dashboard would be unreachable via servlo.localhost. Regression test for
// the fix that replaced host.containers.internal:7073 with the unix socket.
func TestCrossOriginGate_unixSocketTreatedAsLoopback(t *testing.T) {
	setupConfigDir(t, "", "")

	next := &nextHandler{}
	gate := withCrossOriginGate(next)

	req := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
	req.RemoteAddr = "@" // typical for anonymous unix socket peer
	ctx := context.WithValue(req.Context(), ctxKeyUnixSocket{}, true)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if !next.called {
		t.Error("unix socket request blocked — servlo.localhost vhost will 403")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("unix socket status = %d, want 200", rec.Code)
	}
}

// TestCrossOriginGate_csrf covers the cross-origin gate that guards every
// state-changing request, loopback included. The RCE vector is a malicious
// page in the developer's own browser POSTing to 127.0.0.1:7073, so a
// loopback source IP is no longer a free pass for unsafe methods: the request
// must also prove it came from servlo's own dashboard.
func TestCrossOriginGate_csrf(t *testing.T) {
	const siteAction = "/api/sites/myapp.test/restart"

	t.Run("cross-site POST blocked", func(t *testing.T) {
		next := &nextHandler{}
		gate := withCrossOriginGate(next)
		req := httptest.NewRequest(http.MethodPost, siteAction, nil)
		req.RemoteAddr = "127.0.0.1:54321"
		req.Host = "localhost:7073"
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("Origin", "http://evil.example")
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, req)
		if next.called {
			t.Error("cross-site POST reached handler — CSRF/RCE vector open")
		}
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("same-origin POST allowed", func(t *testing.T) {
		next := &nextHandler{}
		gate := withCrossOriginGate(next)
		req := httptest.NewRequest(http.MethodPost, siteAction, nil)
		req.RemoteAddr = "127.0.0.1:54321"
		req.Host = "localhost:7073"
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, req)
		if !next.called {
			t.Errorf("same-origin POST blocked, status=%d", rec.Code)
		}
	})

	// servlo.localhost hitting localhost:7073 (apiBase rewrite) is labelled
	// cross-site by the browser, but the Origin is one of servlo's own, so the
	// dashboard's own requests must still pass.
	t.Run("split-origin dashboard allowed via Origin allowlist", func(t *testing.T) {
		next := &nextHandler{}
		gate := withCrossOriginGate(next)
		req := httptest.NewRequest(http.MethodPost, siteAction, nil)
		req.RemoteAddr = "127.0.0.1:54321"
		req.Host = "localhost:7073"
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("Origin", "http://servlo.localhost")
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, req)
		if !next.called {
			t.Errorf("split-origin dashboard POST blocked, status=%d", rec.Code)
		}
	})

	t.Run("no Sec-Fetch requires CSRF header", func(t *testing.T) {
		next := &nextHandler{}
		gate := withCrossOriginGate(next)
		req := httptest.NewRequest(http.MethodPost, siteAction, nil)
		req.RemoteAddr = "127.0.0.1:54321" // no Sec-Fetch, no X-Servlo-CSRF
		req.Host = "localhost:7073"
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, req)
		if next.called || rec.Code != http.StatusForbidden {
			t.Errorf("POST without Sec-Fetch or CSRF header allowed, status=%d", rec.Code)
		}

		next2 := &nextHandler{}
		gate2 := withCrossOriginGate(next2)
		req2 := httptest.NewRequest(http.MethodPost, siteAction, nil)
		req2.RemoteAddr = "127.0.0.1:54321"
		req2.Host = "localhost:7073"
		req2.Header.Set("X-Servlo-CSRF", "1")
		rec2 := httptest.NewRecorder()
		gate2.ServeHTTP(rec2, req2)
		if !next2.called {
			t.Errorf("POST with X-Servlo-CSRF blocked, status=%d", rec2.Code)
		}
	})

	t.Run("safe methods bypass the gate", func(t *testing.T) {
		for _, m := range []string{http.MethodGet, http.MethodHead} {
			next := &nextHandler{}
			gate := withCrossOriginGate(next)
			req := httptest.NewRequest(m, "/api/sites", nil)
			req.RemoteAddr = "127.0.0.1:54321"
			req.Host = "localhost:7073"
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			req.Header.Set("Origin", "http://evil.example")
			rec := httptest.NewRecorder()
			gate.ServeHTTP(rec, req)
			if !next.called {
				t.Errorf("%s blocked by CSRF gate, status=%d", m, rec.Code)
			}
		}
	})

	t.Run("unix socket exempt", func(t *testing.T) {
		next := &nextHandler{}
		gate := withCrossOriginGate(next)
		req := httptest.NewRequest(http.MethodPost, siteAction, nil)
		req.RemoteAddr = "@" // no Sec-Fetch, no header — trusted via the socket
		req = req.WithContext(context.WithValue(req.Context(), ctxKeyUnixSocket{}, true))
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, req)
		if !next.called {
			t.Errorf("unix socket POST blocked, status=%d", rec.Code)
		}
	})

	// These endpoints are reached by non-browser clients that can't carry the
	// header, and each keeps its own source gate: the internal notify bridge
	// is POSTed by out-of-process CLI commands over loopback.
	t.Run("exempt paths bypass the gate", func(t *testing.T) {
		for _, path := range []string{"/api/internal/notify"} {
			t.Run(path, func(t *testing.T) {
				next := &nextHandler{}
				gate := withCrossOriginGate(next)
				req := httptest.NewRequest(http.MethodPost, path, nil)
				req.RemoteAddr = "127.0.0.1:54321" // no Sec-Fetch, no X-Servlo-CSRF
				req.Host = "localhost:7073"
				rec := httptest.NewRecorder()
				gate.ServeHTTP(rec, req)
				if !next.called {
					t.Errorf("%s blocked by CSRF gate, status=%d", path, rec.Code)
				}
			})
		}
	})

	// Unpause was exempt for a button on the paused-site holding page that
	// POSTed here cross-origin. That page is served to whoever visits the
	// site, so the button is a link now and the exemption is gone.
	t.Run("unpause is no longer exempt", func(t *testing.T) {
		next := &nextHandler{}
		gate := withCrossOriginGate(next)
		req := httptest.NewRequest(http.MethodPost, "/api/sites/myapp.test/unpause", nil)
		req.RemoteAddr = "127.0.0.1:54321"
		req.Host = "localhost:7073"
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, req)
		if next.called {
			t.Error("an unpause with no CSRF proof reached the handler")
		}
	})

	t.Run("LAN cross-site rejected even with valid auth", func(t *testing.T) {
		setupConfigDir(t, "alice", "s3cret")
		next := &nextHandler{}
		gate := withCrossOriginGate(next)
		req := httptest.NewRequest(http.MethodPost, siteAction, nil)
		req.RemoteAddr = "192.168.1.42:54321"
		req.SetBasicAuth("alice", "s3cret")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("Origin", "http://evil.example")
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, req)
		if next.called || rec.Code != http.StatusForbidden {
			t.Errorf("LAN cross-site POST allowed, status=%d", rec.Code)
		}
	})
}

// silence unused-import lint when config is only used transitively.
var _ = config.DataDir
