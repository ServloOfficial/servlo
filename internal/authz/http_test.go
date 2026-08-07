package authz

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testGuard(t *testing.T) *Guard {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	guard, err := NewGuard()
	if err != nil {
		t.Fatalf("NewGuard: %v", err)
	}
	if _, err := guard.Accounts.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return guard
}

// signIn drives the login route and returns the session cookie it set.
func signIn(t *testing.T, guard *Guard, name, password string) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"`+name+`","password":"`+password+`"}`))
	req.RemoteAddr = "203.0.113.9:54321"
	guard.HandleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status %d (%s)", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == SessionCookie {
			return cookie
		}
	}
	t.Fatal("login set no session cookie")
	return nil
}

// The cookie carries a session token, so every flag that stops it leaking has
// to be on: HttpOnly against a script reading it, Secure against it crossing a
// plain connection, SameSite=Strict against another origin sending it.
func TestLogin_SetsAHardenedCookie(t *testing.T) {
	guard := testGuard(t)
	cookie := signIn(t, guard, "alice", "a long enough passphrase")

	if !cookie.HttpOnly {
		t.Error("the session cookie is readable by scripts")
	}
	if !cookie.Secure {
		t.Error("the session cookie is not marked Secure, so it can cross a plain connection")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite = %v, want Strict", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("cookie path = %q, want /", cookie.Path)
	}
}

// A wrong password must not say which half was wrong, or the form becomes a
// way to enumerate account names.
func TestLogin_RejectsWithoutSayingWhichHalfWasWrong(t *testing.T) {
	guard := testGuard(t)

	var messages []string
	for _, attempt := range [][2]string{{"alice", "wrong password here"}, {"nobody", "a long enough passphrase"}} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
			strings.NewReader(`{"username":"`+attempt[0]+`","password":"`+attempt[1]+`"}`))
		req.RemoteAddr = "203.0.113.9:54321"
		guard.HandleLogin(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", attempt[0], rec.Code)
		}
		if len(rec.Result().Cookies()) != 0 {
			t.Errorf("%s: a failed login set a cookie", attempt[0])
		}
		messages = append(messages, rec.Body.String())
	}
	if messages[0] != messages[1] {
		t.Errorf("a wrong password and an unknown account answer differently:\n%q\n%q", messages[0], messages[1])
	}
}

// Repeated failures have to start costing time, or the form is a free oracle.
func TestLogin_LocksOutAfterRepeatedFailures(t *testing.T) {
	guard := testGuard(t)

	var last int
	for i := 0; i < freeAttempts+3; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
			strings.NewReader(`{"username":"alice","password":"wrong password here"}`))
		req.RemoteAddr = "203.0.113.9:54321"
		guard.HandleLogin(rec, req)
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("status after repeated failures = %d, want 429", last)
	}

	// And the lockout has to hold even against the right password, or it is
	// only slowing down an attacker who has already lost.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"alice","password":"a long enough passphrase"}`))
	req.RemoteAddr = "203.0.113.9:54321"
	guard.HandleLogin(rec, req)
	if rec.Code == http.StatusOK {
		t.Error("the lockout let the right password through, so it only delays a guesser who was going to fail anyway")
	}
}

// Signing out ends the session on the server, not just in the browser.
func TestLogout_EndsTheSessionServerSide(t *testing.T) {
	guard := testGuard(t)
	cookie := signIn(t, guard, "alice", "a long enough passphrase")

	session, ok := guard.Sessions.Lookup(cookie.Value)
	if !ok {
		t.Fatal("the session does not exist")
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(cookie)
	req.Header.Set(CSRFHeader, CSRFToken(session.ID))
	guard.HandleLogout(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("logout: status %d (%s)", rec.Code, rec.Body.String())
	}
	if _, ok := guard.Sessions.Lookup(cookie.Value); ok {
		t.Error("the session survived a sign-out, so a copied cookie still works")
	}
}

// The gate is the point of the story: without a session, nothing behind it is
// reachable, whatever the request looks like.
func TestRequire_RefusesWithoutASession(t *testing.T) {
	guard := testGuard(t)
	var reached bool
	handler := guard.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/sites", nil),
		requestWithCookie(http.MethodGet, "/api/sites", "not-a-real-token"),
		loopbackRequest(http.MethodGet, "/api/sites"),
	} {
		reached = false
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if reached {
			t.Errorf("%s reached the handler with no session", req.RemoteAddr)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	}
}

func TestRequire_AdmitsASession(t *testing.T) {
	guard := testGuard(t)
	cookie := signIn(t, guard, "alice", "a long enough passphrase")

	var seen Session
	handler := guard.Require(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen, _ = SessionFrom(r.Context())
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if seen.User != "alice" {
		t.Errorf("the handler saw session user %q, want alice", seen.User)
	}
}

// A state-changing request needs the CSRF token as well as the cookie. Reads
// do not: a forged GET returns data to servlo, not to the attacker.
func TestRequire_StateChangingRequestsNeedTheCSRFToken(t *testing.T) {
	guard := testGuard(t)
	cookie := signIn(t, guard, "alice", "a long enough passphrase")
	session, _ := guard.Sessions.Lookup(cookie.Value)

	var reached bool
	handler := guard.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		reached = false
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/sites/example.com/deploy", nil)
		req.AddCookie(cookie)
		handler.ServeHTTP(rec, req)
		if reached {
			t.Errorf("%s reached the handler with no CSRF token", method)
		}
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", method, rec.Code)
		}

		reached = false
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(method, "/api/sites/example.com/deploy", nil)
		req.AddCookie(cookie)
		req.Header.Set(CSRFHeader, CSRFToken(session.ID))
		handler.ServeHTTP(rec, req)
		if !reached {
			t.Errorf("%s with a valid CSRF token was refused: %d (%s)", method, rec.Code, rec.Body.String())
		}
	}
}

// One session's token must not work for another, or it is a token an attacker
// obtains legitimately and then replays.
func TestRequire_RefusesAnotherSessionsCSRFToken(t *testing.T) {
	guard := testGuard(t)
	if _, err := guard.Accounts.Create("bob", "a long enough passphrase", RoleDeveloper); err != nil {
		t.Fatalf("Create: %v", err)
	}
	aliceCookie := signIn(t, guard, "alice", "a long enough passphrase")
	bobCookie := signIn(t, guard, "bob", "a long enough passphrase")
	bobSession, _ := guard.Sessions.Lookup(bobCookie.Value)

	var reached bool
	handler := guard.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sites/example.com/deploy", nil)
	req.AddCookie(aliceCookie)
	req.Header.Set(CSRFHeader, CSRFToken(bobSession.ID))
	handler.ServeHTTP(rec, req)

	if reached {
		t.Error("one session's CSRF token was accepted for another")
	}
}

// A revoked session stops working immediately, including one revoked from a
// shell while the browser still holds the cookie.
func TestRequire_RefusesARevokedSession(t *testing.T) {
	guard := testGuard(t)
	cookie := signIn(t, guard, "alice", "a long enough passphrase")
	session, _ := guard.Sessions.Lookup(cookie.Value)
	if err := guard.Sessions.Revoke(session.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	var reached bool
	handler := guard.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sites", nil)
	req.AddCookie(cookie)
	handler.ServeHTTP(rec, req)

	if reached {
		t.Error("a revoked session still reaches the panel")
	}
}

// Before the first account exists there is nobody to sign in as, so the panel
// has to be able to tell the browser to show a setup form rather than a login
// form nothing can satisfy.
func TestSetupNeeded(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	guard, err := NewGuard()
	if err != nil {
		t.Fatalf("NewGuard: %v", err)
	}
	if !guard.SetupNeeded() {
		t.Fatal("a fresh install does not report that it needs setting up")
	}
	if _, err := guard.Accounts.Create("alice", "a long enough passphrase", RoleAdmin); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if guard.SetupNeeded() {
		t.Error("an install with an account still reports that it needs setting up")
	}
}

func requestWithCookie(method, path, token string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "203.0.113.9:54321"
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: token})
	return req
}

// A loopback request is the one upstream trusted absolutely. On a server it is
// a reverse proxy, a container, or anything else that reached 127.0.0.1, so it
// gets no more than anyone else.
func loopbackRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "127.0.0.1:54321"
	return req
}
