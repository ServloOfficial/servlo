package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/authz"
)

// The panel listens on every interface by design, and creating the first
// account is the one route a session cannot gate, because there is no account
// to hold one yet. Between `servlo install` finishing and the operator opening
// the dashboard, anybody who reached port 7073 could POST this and become the
// admin of every site, database and file on the machine.
//
// `servlo status` made it worse by reporting that remote clients get a 403,
// which had been true of a gate S5.5 removed.
func TestSetup_RefusesToClaimThePanelOverTheNetwork(t *testing.T) {
	setupConfigDir(t, "", "")
	guard, err := authz.NewGuard()
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/setup",
		strings.NewReader(`{"username":"attacker","password":"a-long-enough-password"}`))
	req.RemoteAddr = "203.0.113.9:41234"
	rec := httptest.NewRecorder()

	withPanelAuth(guard, http.NotFoundHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("a remote request claimed the panel: status %d", rec.Code)
	}
	if guard.Accounts.Any() {
		t.Error("an account was created by a request from the internet")
	}
	if !strings.Contains(rec.Body.String(), "servlo users add") {
		t.Errorf("the refusal should name the way in for an operator with only the public address, got %q", rec.Body.String())
	}
}

// The operator on the machine still has to be able to claim it, or a fresh
// install has no way into its own dashboard.
func TestSetup_LetsTheOperatorOnTheMachineClaimIt(t *testing.T) {
	setupConfigDir(t, "", "")
	guard, err := authz.NewGuard()
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/setup",
		strings.NewReader(`{"username":"operator","password":"a-long-enough-password"}`))
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()

	withPanelAuth(guard, http.NotFoundHandler()).ServeHTTP(rec, req)

	if rec.Code == http.StatusForbidden {
		t.Fatalf("the operator at the machine was refused: %s", rec.Body.String())
	}
	if !guard.Accounts.Any() {
		t.Error("no account was created for a local request")
	}
}
