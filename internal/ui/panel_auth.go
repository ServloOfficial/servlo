package ui

import (
	"net/http"
	"strings"

	"github.com/realrashid/servlo/internal/authz"
)

// The panel's gate.
//
// One rule: every API request carries a session or it goes no further. Not
// "unless it came from loopback", which on a server means a reverse proxy or a
// container; not "unless LAN exposure is off", which was upstream's way of
// saying the panel was only ever reachable from one machine.
//
// Three things are outside it, and each has to be:
//
//   - The page itself and its assets, because the login form has to load
//     before anyone can log in.
//   - The auth routes, which are the way in.
//   - The mailpit webhook and the internal notify bridge, which are POSTed by
//     things that hold no cookie and have their own source gates.

// publicPrefixes are the built asset subtrees. They serve files, never state,
// and their names are hashed at build time so listing them individually would
// be listing something that changes on every build.
var publicPrefixes = []string{"/assets/", "/icons/"}

// isPublicPath reports whether a request may be served without a session.
//
// It reads the permission registry rather than a list of its own. Two lists of
// what is public is how they drift, and the one that drifts is the one deciding
// whether to ask for a password.
func isPublicPath(path string) bool {
	if permission, ok := authz.Permissions().For(path); ok {
		return permission == authz.PermPublic
	}
	for _, prefix := range publicPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	// Everything under /api is gated. Anything else is the Svelte app's own
	// routing, which serves the same HTML shell whatever the path, and that
	// shell is what renders the login form.
	return !strings.HasPrefix(path, "/api")
}

// withPanelAuth wraps the panel mux so nothing behind it is reachable without
// a session.
func withPanelAuth(guard *authz.Guard, next http.Handler) http.Handler {
	// Require establishes who the request is, Audit records what they tried,
	// and ScopeSites decides whether they may. Audit sits outside the decision
	// so a refusal is recorded too, which is the entry somebody is most often
	// looking for.
	guarded := guard.Require(guard.Audit(guard.ScopeSites(next)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A preflight carries no cookie by design, so gating it would break
		// every cross-origin request from servlo's own split-origin dev server.
		// It reaches no handler and changes nothing.
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		switch r.URL.Path {
		case "/api/auth/session":
			guard.HandleSession(w, r)
			return
		case "/api/auth/login":
			guard.HandleLogin(w, r)
			return
		case "/api/auth/logout":
			guard.HandleLogout(w, r)
			return
		case "/api/auth/setup":
			guard.HandleSetup(w, r)
			return
		}
		// The TOTP routes need a session, so they go behind the guard rather
		// than beside the login routes: enrolling a second factor is something
		// you do while signed in, not instead of signing in.
		switch r.URL.Path {
		case "/api/auth/totp/enrol":
			guard.Require(http.HandlerFunc(guard.HandleTOTPEnrol)).ServeHTTP(w, r)
			return
		case "/api/auth/totp/confirm":
			guard.Require(http.HandlerFunc(guard.HandleTOTPConfirm)).ServeHTTP(w, r)
			return
		case "/api/audit":
			guard.Require(guard.ScopeSites(http.HandlerFunc(guard.HandleAuditLog))).ServeHTTP(w, r)
			return
		case "/api/auth/totp/qr":
			guard.Require(http.HandlerFunc(guard.HandleTOTPQR)).ServeHTTP(w, r)
			return
		case "/api/auth/totp/disable":
			guard.Require(http.HandlerFunc(guard.HandleTOTPDisable)).ServeHTTP(w, r)
			return
		}
		if isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		guarded.ServeHTTP(w, r)
	})
}
