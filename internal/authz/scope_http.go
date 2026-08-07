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
//
// It reads the permission registry rather than a second list of its own. Two
// lists is how they drift, and the one that drifts is always the one doing the
// enforcing.
func (s Scope) allows(path string) bool {
	if s.MayAdminister() {
		return true
	}
	if s.Role != RoleDeveloper {
		return false
	}
	permission, pattern, declared := Permissions().lookup(path)
	if !declared {
		// Deny by default. See the note at the top of the file: this is what
		// makes a route added without a thought for roles fail loudly rather
		// than open, and it is what the surface scan turns into a build error.
		return false
	}
	switch permission {
	case PermPublic, PermSelf, PermSiteList:
		// The list route filters to what this scope may see rather than
		// refusing outright; refusing would leave a developer looking at an
		// error page instead of their own work.
		return true
	case PermSite:
		domain, named := siteFromPattern(pattern, path)
		if !named {
			// A site route with no site in it. Refusing is the safe reading:
			// there is nothing to compare the scope against.
			return false
		}
		return s.MaySee(domain)
	default:
		return false
	}
}

// siteFromPattern pulls the domain out of a path, given the registry pattern
// that matched it. The domain is the first segment after the prefix, which is
// the shape every PermSite route has and the reason the others are not one.
func siteFromPattern(pattern, path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, pattern)
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
