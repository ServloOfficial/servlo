package authz

import (
	"context"
	"net/http"
	"strings"
)

// Enforcing the roles.
//
// The rule is deny by default. A path this file does not recognise is refused
// to a Developer, because the alternative — unrecognised means allowed — would
// open every route added later until somebody remembered to classify it, and
// nothing would fail while they had not.
//
// That has a cost, and it is the right cost: a route added without a thought
// for roles breaks for developers, loudly, rather than working for everyone.

type ctxKeyScope struct{}

// developerPaths are the routes a Developer may reach that name no site: their
// own session and account, and the read-only overviews the panel needs to
// render at all. The list route returns their sites; filtering is the
// handler's job, and refusing it outright would leave them looking at an error
// page instead of their own work.
var developerPaths = map[string]bool{
	"/api/status":            true,
	"/api/sites":             true,
	"/api/ws":                true,
	"/api/version":           true,
	"/api/auth/session":      true,
	"/api/auth/logout":       true,
	"/api/auth/totp/enrol":   true,
	"/api/auth/totp/confirm": true,
	"/api/auth/totp/disable": true,
	"/api/auth/totp/qr":      true,
}

// ScopeFrom returns the authority a request was granted.
func ScopeFrom(ctx context.Context) (Scope, bool) {
	scope, ok := ctx.Value(ctxKeyScope{}).(Scope)
	return scope, ok
}

// ScopeSites attaches the request's scope and refuses what it does not cover.
// It sits behind Require, so the session is already known.
func (g *Guard) ScopeSites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := SessionFrom(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		scope := g.scopeFor(session.User)
		if !scope.allows(r.URL.Path) {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "Forbidden — your account does not have access to this.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyScope{}, scope)))
	})
}

// scopeFor reads an account's authority. An account that has gone missing
// between signing in and now gets nothing rather than a default.
func (g *Guard) scopeFor(user string) Scope {
	account, ok := g.Accounts.Lookup(user)
	if !ok {
		return Scope{}
	}
	return Scope{Role: account.Role, Sites: account.Sites}
}

// allows reports whether this scope may reach a path.
func (s Scope) allows(path string) bool {
	if s.MayAdminister() {
		return true
	}
	if s.Role != RoleDeveloper {
		return false
	}
	if developerPaths[path] {
		return true
	}
	if domain, ok := siteFromPath(path); ok {
		return s.MaySee(domain)
	}
	// Deny by default. See the note at the top of the file: this is what makes
	// a route added without a thought for roles fail loudly rather than open.
	return false
}

// siteFromPath pulls the domain out of /api/sites/<domain>/... and reports
// whether the path named one at all.
func siteFromPath(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, "/api/sites/")
	if !ok || rest == "" {
		return "", false
	}
	if slash := strings.Index(rest, "/"); slash >= 0 {
		rest = rest[:slash]
	}
	if rest == "" {
		return "", false
	}
	return rest, true
}
