package authz

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// The gate.
//
// Upstream's rule was "loopback is trusted, everything else presents HTTP Basic
// credentials". On a server the first half is wrong — a request from 127.0.0.1
// is a reverse proxy, a container, or anything else that reached the port — and
// the second half sends the password on every request with no way to sign out.
//
// Here every request carries a session or it goes no further, whatever address
// it arrived from.

const (
	// SessionCookie carries the session token. The __Host- prefix is a promise
	// the browser enforces: a cookie with it must be Secure, path /, and carry
	// no Domain, which means no other host in the registrable domain can set or
	// overwrite it. On a panel sharing a parent domain with the sites it hosts,
	// that is the difference between a site's own JavaScript being able to
	// plant a session cookie and not.
	SessionCookie = "__Host-servlo_session"

	// CSRFHeader is where the dashboard sends the token the panel handed it.
	CSRFHeader = "X-Servlo-CSRF"
)

type ctxKeySession struct{}

// Guard holds the stores and the limiter, and is what the panel wraps its mux
// with.
type Guard struct {
	Accounts *AccountStore
	Sessions *SessionStore
	Limiter  *Limiter
}

// NewGuard opens the stores.
func NewGuard() (*Guard, error) {
	accounts, err := OpenAccounts()
	if err != nil {
		return nil, err
	}
	sessions, err := OpenSessions()
	if err != nil {
		return nil, err
	}
	return &Guard{Accounts: accounts, Sessions: sessions, Limiter: NewLimiter()}, nil
}

// SetupNeeded reports whether no account exists yet, so the dashboard can offer
// to create the first rather than a login form nothing can satisfy.
func (g *Guard) SetupNeeded() bool { return !g.Accounts.Any() }

// SessionFrom returns the session a request authenticated as.
func SessionFrom(ctx context.Context) (Session, bool) {
	session, ok := ctx.Value(ctxKeySession{}).(Session)
	return session, ok
}

// Require is the middleware every panel route sits behind.
func (g *Guard) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookie)
		if err != nil || cookie.Value == "" {
			unauthorized(w)
			return
		}
		session, ok := g.Sessions.Lookup(cookie.Value)
		if !ok {
			// Clear the cookie on the way out. A browser holding one that no
			// longer works will otherwise send it forever, and the operator
			// sees a login form that seems to do nothing.
			clearSessionCookie(w)
			unauthorized(w)
			return
		}

		// Reads do not need the CSRF token: a forged GET returns data to
		// servlo, not to whoever forged it. Everything that changes state does.
		if stateChanging(r.Method) && !VerifyCSRF(session.ID, r.Header.Get(CSRFHeader)) {
			http.Error(w, "Forbidden — this request carries no valid CSRF token. Use the Servlo dashboard itself.", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeySession{}, session)))
	})
}

// HandleLogin authenticates a name and password and issues a session.
func (g *Guard) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	addr := sourceAddress(r)
	if wait := g.Limiter.Retry(addr); wait > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(wait.Seconds())+1))
		http.Error(w, fmt.Sprintf("Too many attempts. Try again in %s.", wait.Round(time.Second)), http.StatusTooManyRequests)
		return
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	account, ok := g.Accounts.Authenticate(body.Username, body.Password)
	if !ok {
		g.Limiter.Failed(addr)
		// One message for both halves. Saying which was wrong turns the form
		// into a way to enumerate account names.
		http.Error(w, "That username and password do not match.", http.StatusUnauthorized)
		return
	}
	g.Limiter.Succeeded(addr)

	token, err := g.Sessions.Create(account.Name, SessionMeta{
		IP:        addr,
		UserAgent: truncate(r.UserAgent(), 200),
	})
	if err != nil {
		http.Error(w, "could not start a session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	session, _ := g.Sessions.Lookup(token)
	setSessionCookie(w, token)
	writeJSON(w, map[string]any{
		"ok":   true,
		"user": account.Name,
		"role": account.Role,
		"csrf": CSRFToken(session.ID),
	})
}

// HandleLogout ends the session the request carries.
func (g *Guard) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cookie, err := r.Cookie(SessionCookie)
	if err != nil || cookie.Value == "" {
		// Nothing to end. Answering OK keeps a sign-out button from failing
		// for someone whose session already expired.
		clearSessionCookie(w)
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	session, ok := g.Sessions.Lookup(cookie.Value)
	if ok {
		// Signing out is state-changing, so it needs the token like anything
		// else: without this, another origin could sign the operator out.
		if !VerifyCSRF(session.ID, r.Header.Get(CSRFHeader)) {
			http.Error(w, "Forbidden — this request carries no valid CSRF token.", http.StatusForbidden)
			return
		}
		if err := g.Sessions.Revoke(session.ID); err != nil {
			http.Error(w, "could not end the session: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	clearSessionCookie(w)
	writeJSON(w, map[string]any{"ok": true})
}

// HandleSession reports who the request is, which is what the dashboard asks
// on load to decide between the app, a login form and the setup form.
func (g *Guard) HandleSession(w http.ResponseWriter, r *http.Request) {
	if g.SetupNeeded() {
		writeJSON(w, map[string]any{"setup_needed": true})
		return
	}
	cookie, err := r.Cookie(SessionCookie)
	if err != nil || cookie.Value == "" {
		writeJSON(w, map[string]any{"authenticated": false})
		return
	}
	session, ok := g.Sessions.Lookup(cookie.Value)
	if !ok {
		clearSessionCookie(w)
		writeJSON(w, map[string]any{"authenticated": false})
		return
	}
	role := RoleDeveloper
	if account, found := g.Accounts.Lookup(session.User); found {
		role = account.Role
	}
	writeJSON(w, map[string]any{
		"authenticated": true,
		"user":          session.User,
		"role":          role,
		"csrf":          CSRFToken(session.ID),
	})
}

// HandleSetup creates the first account. It answers only while there is none,
// so it cannot be used to add an admin to a panel that already has one.
func (g *Guard) HandleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !g.SetupNeeded() {
		http.Error(w, "This panel already has an account. Sign in, or add another with: servlo users add", http.StatusConflict)
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	// The first account is an admin: it is the only one, and a panel whose only
	// account cannot administer it is a panel nobody can administer.
	account, err := g.Accounts.Create(body.Username, body.Password, RoleAdmin)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	token, err := g.Sessions.Create(account.Name, SessionMeta{
		IP:        sourceAddress(r),
		UserAgent: truncate(r.UserAgent(), 200),
	})
	if err != nil {
		http.Error(w, "could not start a session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	session, _ := g.Sessions.Lookup(token)
	setSessionCookie(w, token)
	writeJSON(w, map[string]any{
		"ok":   true,
		"user": account.Name,
		"role": account.Role,
		"csrf": CSRFToken(session.ID),
	})
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(SessionTTL / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

func stateChanging(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// sourceAddress is what the limiter counts against. It is the peer address,
// not a forwarded header: a header is set by whoever is calling, so counting
// against it lets an attacker reset their own budget on every request.
func sourceAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "Unauthorized — sign in to the Servlo panel.", http.StatusUnauthorized)
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(payload)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n])
}
