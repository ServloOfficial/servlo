package authz

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func scopedGuard(t *testing.T) *Guard {
	t.Helper()
	guard := testGuard(t)
	if _, err := guard.Accounts.Create("dev", "a long enough passphrase", RoleDeveloper); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := guard.Accounts.SetSites("dev", []string{"example.com"}); err != nil {
		t.Fatalf("SetSites: %v", err)
	}
	return guard
}

// signedInAs returns a request carrying a session for the named account.
func signedInAs(t *testing.T, guard *Guard, name, method, path string) *http.Request {
	t.Helper()
	token, err := guard.Sessions.Create(name, SessionMeta{})
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "203.0.113.9:54321"
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: token})
	session, _ := guard.Sessions.Lookup(token)
	if method != http.MethodGet {
		req.Header.Set(CSRFHeader, CSRFToken(session.ID))
	}
	return req
}

// The route carries the domain, so the check is on the route. A developer
// reaching a site that is not theirs is refused whatever the handler would
// have done with it.
func TestScopedRoutes_DeveloperIsHeldToTheirSites(t *testing.T) {
	guard := scopedGuard(t)
	var reached bool
	handler := guard.Require(guard.ScopeSites(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })))

	for _, path := range []string{
		"/api/sites/other.com",
		"/api/sites/other.com/deploy",
		"/api/sites/other.com/logs",
		"/api/sites/other.com/env",
		"/api/sites/other.com/cron/anything",
	} {
		reached = false
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, signedInAs(t, guard, "dev", http.MethodGet, path))
		if reached {
			t.Errorf("%s reached the handler for a site the developer is not assigned", path)
		}
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", path, rec.Code)
		}
	}

	// And their own site works, or the role is just a way to break the panel.
	for _, path := range []string{
		"/api/sites/example.com",
		"/api/sites/example.com/deploy",
		"/api/sites/example.com/logs",
	} {
		reached = false
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, signedInAs(t, guard, "dev", http.MethodGet, path))
		if !reached {
			t.Errorf("%s was refused for the developer's own site: %d", path, rec.Code)
		}
	}
}

func TestScopedRoutes_AdminReachesEverySite(t *testing.T) {
	guard := scopedGuard(t)
	var reached bool
	handler := guard.Require(guard.ScopeSites(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })))

	for _, path := range []string{"/api/sites/other.com", "/api/sites/example.com/deploy"} {
		reached = false
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, signedInAs(t, guard, "alice", http.MethodGet, path))
		if !reached {
			t.Errorf("an admin was refused %s: %d", path, rec.Code)
		}
	}
}

// Everything that is not a site belongs to the admin: services, backups,
// accounts, the panel's own settings. A developer with a site assigned must not
// reach them by virtue of having any access at all.
func TestScopedRoutes_DeveloperCannotAdminister(t *testing.T) {
	guard := scopedGuard(t)
	var reached bool
	handler := guard.Require(guard.ScopeSites(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })))

	for _, path := range []string{
		"/api/services",
		"/api/services/mysql/start",
		"/api/servlo/stop",
		"/api/users",
		"/api/tools",
	} {
		reached = false
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, signedInAs(t, guard, "dev", http.MethodPost, path))
		if reached {
			t.Errorf("%s reached the handler for a developer", path)
		}
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", path, rec.Code)
		}
	}
}

// A developer still needs the routes that are about themselves, or they cannot
// sign out or turn on their own second factor.
func TestScopedRoutes_DeveloperKeepsTheirOwnAccountRoutes(t *testing.T) {
	guard := scopedGuard(t)
	var reached bool
	handler := guard.Require(guard.ScopeSites(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })))

	for _, path := range []string{"/api/auth/totp/enrol", "/api/status", "/api/sites"} {
		reached = false
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, signedInAs(t, guard, "dev", http.MethodGet, path))
		if !reached {
			t.Errorf("a developer was refused %s, which is about their own session: %d", path, rec.Code)
		}
	}
}

// A path that names no site is not a path a developer may act on by default.
// The safe direction for anything unrecognised is refusal, or every route
// added later is open to developers until someone remembers to list it.
func TestScopedRoutes_UnrecognisedRoutesAreRefusedToDevelopers(t *testing.T) {
	guard := scopedGuard(t)
	var reached bool
	handler := guard.Require(guard.ScopeSites(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, signedInAs(t, guard, "dev", http.MethodPost, "/api/something-invented-later"))
	if reached {
		t.Error("a route nobody has classified was open to a developer")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

// The handler needs the scope, because a list route has to filter rather than
// refuse: a developer asking for the sites gets theirs, not a 403.
func TestScopedRoutes_HandlerSeesTheScope(t *testing.T) {
	guard := scopedGuard(t)
	var seen Scope
	handler := guard.Require(guard.ScopeSites(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen, _ = ScopeFrom(r.Context())
	})))

	handler.ServeHTTP(httptest.NewRecorder(), signedInAs(t, guard, "dev", http.MethodGet, "/api/sites"))
	if seen.Role != RoleDeveloper {
		t.Errorf("handler saw role %q, want developer", seen.Role)
	}
	if !seen.MaySee("example.com") || seen.MaySee("other.com") {
		t.Errorf("handler saw the wrong site list: %v", seen.Sites)
	}
}

// Assigning sites is an admin action, and a developer must not be able to
// widen their own.
func TestAccounts_SetSites(t *testing.T) {
	store := testAccounts(t)
	if _, err := store.Create("dev", "a long enough passphrase", RoleDeveloper); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.SetSites("dev", []string{"example.com", "shop.example.com"}); err != nil {
		t.Fatalf("SetSites: %v", err)
	}
	account, _ := store.Lookup("dev")
	if len(account.Sites) != 2 {
		t.Fatalf("sites = %v, want two", account.Sites)
	}
	if err := store.SetSites("nobody", []string{"example.com"}); err == nil {
		t.Error("SetSites accepted an account that does not exist")
	}
}
