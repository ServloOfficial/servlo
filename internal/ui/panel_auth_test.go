package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/authz"
)

func authTestGuard(t *testing.T) *authz.Guard {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	guard, err := authz.NewGuard()
	if err != nil {
		t.Fatalf("NewGuard: %v", err)
	}
	if _, err := guard.Accounts.Create("alice", "a long enough passphrase", authz.RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return guard
}

// The whole story in one test: no session, no API. Loopback included, because
// on a server a request from 127.0.0.1 is a reverse proxy or a container, not
// the operator sitting at the machine.
func TestPanelAuth_APIRequiresASession(t *testing.T) {
	guard := authTestGuard(t)
	var reached bool
	inner := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })
	handler := withPanelAuth(guard, inner)

	for _, addr := range []string{"127.0.0.1:54321", "203.0.113.9:54321", "192.168.1.42:54321"} {
		for _, path := range []string{"/api/sites", "/api/stats", "/api/servlo/stop", "/api/databases"} {
			reached = false
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.RemoteAddr = addr
			handler.ServeHTTP(rec, req)
			if reached {
				t.Errorf("%s from %s reached the panel with no session", path, addr)
			}
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("%s from %s: status = %d, want 401", path, addr, rec.Code)
			}
		}
	}
}

// The login page has to load before anyone can log in, so the assets that
// carry it are not behind the gate.
func TestPanelAuth_TheLoginPageItselfIsReachable(t *testing.T) {
	guard := authTestGuard(t)
	var reached bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	handler := withPanelAuth(guard, inner)

	for _, path := range []string{"/", "/assets/index.js", "/icons/icon.svg", "/sw.js", "/offline.html", "/manifest.webmanifest"} {
		reached = false
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "203.0.113.9:54321"
		handler.ServeHTTP(rec, req)
		if !reached {
			t.Errorf("%s was gated, so the login page cannot load: %d", path, rec.Code)
		}
	}
}

// The auth routes are the way in, so they answer without a session. They are
// the only /api routes that do.
func TestPanelAuth_AuthRoutesAnswerWithoutASession(t *testing.T) {
	guard := authTestGuard(t)
	handler := withPanelAuth(guard, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req.RemoteAddr = "203.0.113.9:54321"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("/api/auth/session: status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"alice","password":"a long enough passphrase"}`))
	req.RemoteAddr = "203.0.113.9:54321"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("/api/auth/login: status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}

// A signed-in request reaches the panel, and the handler can see who it is.
func TestPanelAuth_ASignedInRequestGetsThrough(t *testing.T) {
	guard := authTestGuard(t)
	var user string
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if session, ok := authz.SessionFrom(r.Context()); ok {
			user = session.User
		}
	})
	handler := withPanelAuth(guard, inner)

	token, err := guard.Sessions.Create("alice", authz.SessionMeta{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
	req.RemoteAddr = "203.0.113.9:54321"
	req.AddCookie(&http.Cookie{Name: authz.SessionCookie, Value: token})
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if user != "alice" {
		t.Errorf("the handler saw %q, want alice", user)
	}
}

// The websocket is a full API surface of its own. Gating the routes and
// leaving it open would be leaving the door beside the door.
func TestPanelAuth_TheWebsocketRequiresASession(t *testing.T) {
	guard := authTestGuard(t)
	var reached bool
	handler := withPanelAuth(guard, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ws", nil)
	req.RemoteAddr = "203.0.113.9:54321"
	handler.ServeHTTP(rec, req)

	if reached {
		t.Error("the websocket accepted a connection with no session")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// Before the first account exists, the panel has to be usable enough to create
// one, and that route has to close the moment it has.
func TestPanelAuth_SetupClosesOnceAnAccountExists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	guard, err := authz.NewGuard()
	if err != nil {
		t.Fatalf("NewGuard: %v", err)
	}
	handler := withPanelAuth(guard, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	// From the machine, which is the only place the first account is created:
	// the panel listens on every interface, and until there is an account
	// there is no session to gate the route with.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/setup",
		strings.NewReader(`{"username":"alice","password":"a long enough passphrase"}`))
	req.RemoteAddr = "127.0.0.1:54321"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first setup: status = %d (%s)", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/setup",
		strings.NewReader(`{"username":"mallory","password":"a long enough passphrase"}`))
	req.RemoteAddr = "198.51.100.4:54321"
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Error("a second account was created through the setup route, so anyone reaching the panel can make themselves an admin")
	}
}

// panelStack composes the handler the panel actually serves: session
// authentication in front, the remaining gate behind it. Tests that assert
// "this request must not get through" belong here rather than against either
// half, because either half alone answers a question the panel never asks.
func panelStack(t *testing.T, next http.Handler) http.Handler {
	t.Helper()
	guard, err := authz.NewGuard()
	if err != nil {
		t.Fatalf("NewGuard: %v", err)
	}
	return withPanelAuth(guard, withRemoteControlGate(next))
}
