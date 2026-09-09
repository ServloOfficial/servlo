package authz

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/auditlog"
)

// The audit middleware records at a chokepoint, which is what keeps seventy
// handlers from being seventy chances to forget. These six routes sit outside
// it, and for a reason: they run before there is a session for the middleware
// to read an actor from. That is a reason to record them specially, not a
// reason for the log to be silent about who signed in, who claimed the panel,
// and who turned a second factor off.
func auditActions(t *testing.T) []string {
	t.Helper()
	entries, err := auditlog.Recent(50)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Action)
	}
	return out
}

func hasAction(actions []string, want string) bool {
	for _, a := range actions {
		if a == want {
			return true
		}
	}
	return false
}

func postJSON(path, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.RemoteAddr = "203.0.113.9:54321"
	return req
}

// A sign-in is the entry an operator looks for first after something goes
// wrong, and a refused one is the entry the log exists for.
func TestAuthAudit_SignInIsRecordedWhetherItWorkedOrNot(t *testing.T) {
	guard := testGuard(t)

	guard.HandleLogin(httptest.NewRecorder(), postJSON("/api/auth/login",
		`{"username":"alice","password":"a long enough passphrase"}`))
	guard.HandleLogin(httptest.NewRecorder(), postJSON("/api/auth/login",
		`{"username":"alice","password":"not the passphrase"}`))

	entries, err := auditlog.Recent(50)
	if err != nil {
		t.Fatal(err)
	}
	var ok, failed int
	for _, e := range entries {
		if e.Action != "auth.login" {
			continue
		}
		if e.Actor != "alice" {
			t.Errorf("a sign-in was recorded without naming who tried: %+v", e)
		}
		if e.IP != "203.0.113.9" {
			t.Errorf("a sign-in was recorded without where it came from: %+v", e)
		}
		switch e.Result {
		case auditlog.ResultOK:
			ok++
		case auditlog.ResultFailed:
			failed++
		}
	}
	if ok != 1 {
		t.Errorf("recorded %d successful sign-ins, want 1", ok)
	}
	if failed != 1 {
		t.Errorf("recorded %d refused sign-ins, want 1: a password guess leaves no trace", failed)
	}
}

// Signing out ends the session the entries above are attributed to, so the log
// has to say when that happened.
func TestAuthAudit_SignOutIsRecorded(t *testing.T) {
	guard := testGuard(t)
	cookie := signIn(t, guard, "alice", "a long enough passphrase")

	req := postJSON("/api/auth/logout", "")
	req.AddCookie(cookie)
	// Signing out is state-changing, so the route wants the token like any
	// other write; without it the request is refused before it ends anything.
	session, _ := guard.Sessions.Lookup(cookie.Value)
	req.Header.Set(CSRFHeader, CSRFToken(session.ID))

	rec := httptest.NewRecorder()
	guard.HandleLogout(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout: status %d (%s)", rec.Code, rec.Body.String())
	}

	if !hasAction(auditActions(t), "auth.logout") {
		t.Error("signing out left no entry")
	}
}

// Claiming the panel creates the account that owns every site on the machine.
// It happens once, and it is the one action with nobody above it to authorise
// it, which is exactly why it belongs in the log.
func TestAuthAudit_ClaimingThePanelIsRecorded(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	guard, err := NewGuard()
	if err != nil {
		t.Fatal(err)
	}

	guard.HandleSetup(httptest.NewRecorder(), postJSON("/api/auth/setup",
		`{"username":"root-admin","password":"a long enough passphrase"}`))

	entries, err := auditlog.Recent(50)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Action == "auth.setup" && e.Subject == "root-admin" {
			return
		}
	}
	t.Errorf("claiming the panel left no entry naming the account: %+v", entries)
}

// Turning a second factor off is a downgrade somebody who already holds a
// session can perform. The CLI door records it as users.totp.disabled; the
// panel door has to say the same word, or a search for one misses the other.
func TestAuthAudit_TurningTOTPOffIsRecordedLikeTheCLIDoes(t *testing.T) {
	guard := testGuard(t)
	cookie := signIn(t, guard, "alice", "a long enough passphrase")

	req := postJSON("/api/auth/totp/disable", `{"password":"a long enough passphrase"}`)
	req.AddCookie(cookie)
	session, _ := guard.Sessions.Lookup(cookie.Value)
	req.Header.Set(CSRFHeader, CSRFToken(session.ID))
	guard.Require(http.HandlerFunc(guard.HandleTOTPDisable)).ServeHTTP(httptest.NewRecorder(), req)

	if !hasAction(auditActions(t), "users.totp.disabled") {
		t.Error("turning the second factor off through the panel left no entry")
	}
}

// The four above test the handlers that exist today. What actually went wrong
// is structural: panel_auth.go answers a handful of paths before the guarded
// chain and returns, so anything added to that switch skips the middleware
// silently. This reads the switch and requires every state-changing path in it
// to be accounted for, so a seventh route cannot join them unnoticed.
func TestAuthAudit_EveryRouteOutsideTheMiddlewareIsAccountedFor(t *testing.T) {
	// Reads change nothing, and the middleware does not record them either.
	reads := map[string]string{
		"/api/auth/session":    "answers who you are",
		"/api/auth/totp/qr":    "renders an enrolment URI already in hand",
		"/api/audit":           "reads the log",
		"/api/auth/totp/enrol": "hands back a secret not yet stored",
	}
	// Writes, each recorded by its own handler because the middleware cannot
	// name an actor this early.
	writes := map[string]string{
		"/api/auth/login":        "auth.login",
		"/api/auth/logout":       "auth.logout",
		"/api/auth/setup":        "auth.setup",
		"/api/auth/totp/confirm": "users.totp.enabled",
		"/api/auth/totp/disable": "users.totp.disabled",
	}

	src, err := os.ReadFile(filepath.Join("..", "ui", "panel_auth.go"))
	if err != nil {
		t.Fatal(err)
	}
	paths := casePaths(string(src))
	if len(paths) < len(reads)+len(writes) {
		t.Fatalf("found %d routes answered before the guarded chain, expected at least %d", len(paths), len(reads)+len(writes))
	}

	handlers, err := os.ReadFile("http.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if _, ok := reads[path]; ok {
			continue
		}
		action, ok := writes[path]
		if !ok {
			t.Errorf("%s is answered before the audit middleware and is not listed here: record it in its handler and add it to writes, or add it to reads with why it changes nothing", path)
			continue
		}
		if !strings.Contains(string(handlers), `"`+action+`"`) {
			t.Errorf("%s should record %s and no handler writes that action", path, action)
		}
	}
}

// casePaths returns the "/api/..." strings the pre-guard switches answer.
func casePaths(src string) []string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "case \"/api/") {
			continue
		}
		path := strings.TrimSuffix(strings.TrimPrefix(line, "case \""), "\":")
		out = append(out, path)
	}
	return out
}
