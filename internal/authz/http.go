package authz

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/realrashid/servlo/internal/auditlog"
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
	// Issuer is what an authenticator app lists this panel under, normally its
	// domain. Empty is fine: the enrolment URI falls back to a name rather
	// than an empty label.
	Issuer string
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

// WithSession attaches a session to a context. The middleware's own way of
// doing it, exported so a handler test can stand in a signed-in user without a
// second copy of the context key.
func WithSession(ctx context.Context, session Session) context.Context {
	return context.WithValue(ctx, ctxKeySession{}, session)
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

		next.ServeHTTP(w, r.WithContext(WithSession(r.Context(), session)))
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
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	account, outcome := g.Accounts.AuthenticateWithOutcome(body.Username, body.Password, body.Code)
	if outcome != AuthOK {
		// A wrong code counts against the limiter like a wrong password, or
		// the second factor is six digits an attacker can try a million times.
		// A missing one does too: it is an attempt that did not sign in.
		g.Limiter.Failed(addr)
		if outcome == AuthCodeRequired {
			// Only ever reached with a correct password, so this says nothing
			// to anyone who does not already hold it.
			http.Error(w, "That account needs a code from its authenticator app.", http.StatusUnauthorized)
			return
		}
		// One message for a wrong password and an unknown account, because
		// telling them apart turns the form into a way to enumerate names.
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
	totpEnabled := false
	recoveryLeft := 0
	if account, found := g.Accounts.Lookup(session.User); found {
		role = account.Role
		totpEnabled = account.TOTPEnabled
		recoveryLeft = account.RecoveryCodesLeft()
	}
	writeJSON(w, map[string]any{
		"authenticated":  true,
		"user":           session.User,
		"role":           role,
		"csrf":           CSRFToken(session.ID),
		"totp_enabled":   totpEnabled,
		"recovery_codes": recoveryLeft,
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

// HandleTOTPEnrol starts enrolment: it mints a secret and returns it with the
// otpauth URI to render as a QR code. Nothing is stored until the operator
// proves their app produced a matching code, so an app that failed to scan
// leaves the account exactly as it was.
func (g *Guard) HandleTOTPEnrol(w http.ResponseWriter, r *http.Request) {
	session, ok := SessionFrom(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	secret, err := NewTOTPSecret()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"secret": secret,
		"uri":    TOTPEnrolmentURI(session.User, g.Issuer, secret),
	})
}

// HandleTOTPConfirm finishes enrolment and returns the recovery codes, once.
func (g *Guard) HandleTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	session, ok := SessionFrom(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	var body struct {
		Secret string `json:"secret"`
		Code   string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !VerifyTOTP(body.Secret, body.Code) {
		http.Error(w, "That code does not match. Check your phone's clock, then try the next one.", http.StatusBadRequest)
		return
	}
	codes, err := g.Accounts.EnableTOTP(session.User, body.Secret)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "recovery_codes": codes})
}

// HandleTOTPDisable turns the second factor off for the signed-in account.
//
// It asks for the password again. Turning off a factor on a session someone
// walked away from is exactly the case this protects against, and the person
// doing it legitimately knows their own password.
func (g *Guard) HandleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	session, ok := SessionFrom(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	addr := sourceAddress(r)
	if wait := g.Limiter.Retry(addr); wait > 0 {
		http.Error(w, "Too many attempts. Try again shortly.", http.StatusTooManyRequests)
		return
	}
	// Password only: asking for a code as well would make a lost phone
	// impossible to recover from in the browser, which is what the recovery
	// codes and the CLI reset are for.
	if !g.Accounts.PasswordMatches(session.User, body.Password) {
		g.Limiter.Failed(addr)
		http.Error(w, "That password does not match.", http.StatusUnauthorized)
		return
	}
	g.Limiter.Succeeded(addr)
	if err := g.Accounts.DisableTOTP(session.User); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// HandleTOTPQR renders an enrolment URI as a PNG.
//
// Server-side so the dashboard does not carry a QR encoder for one screen. The
// URI arrives in the query string rather than being looked up, because the
// secret it contains is never stored until enrolment is confirmed, and storing
// it early would leave a half-enrolled secret behind on every abandoned
// attempt.
func (g *Guard) HandleTOTPQR(w http.ResponseWriter, r *http.Request) {
	if _, ok := SessionFrom(r.Context()); !ok {
		unauthorized(w)
		return
	}
	uri := r.URL.Query().Get("uri")
	// Only ever an otpauth URI. Encoding whatever arrives would turn the panel
	// into a QR generator for anything an attacker wanted a signed-in operator
	// to scan.
	if !strings.HasPrefix(uri, "otpauth://totp/") || len(uri) > 512 {
		http.Error(w, "not an enrolment URI", http.StatusBadRequest)
		return
	}
	png, err := qrcode.Encode(uri, qrcode.Medium, 360)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	// It carries the secret, so it is never cached anywhere.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

// HandleAuditLog serves the recent audit entries.
//
// Admin only, and declared as such in the registry: the log records who did
// what from where across every account, which is not a developer's to read.
func (g *Guard) HandleAuditLog(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFrom(r.Context())
	if !ok || !scope.MayAdminister() {
		http.Error(w, "Forbidden — the audit log is for administrators.", http.StatusForbidden)
		return
	}
	limit := 200
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	entries, err := auditlog.Recent(limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entries == nil {
		// An empty log is a normal state, and a null would have the dashboard
		// rendering "no entries" as an error.
		entries = []auditlog.Entry{}
	}
	writeJSON(w, map[string]any{"entries": entries})
}
