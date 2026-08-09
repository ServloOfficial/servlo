package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const demoKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIH8kPzUFN0Nlj8Kk8Yc4LxMxbF6mYt3xI0DfKLPjXWmQ"

func securityEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, ".local", "share"))
	return dir
}

func postKey(t *testing.T, body string) KeyResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	handleSecurityKeys(rec, httptest.NewRequest(http.MethodPost, "/api/security/keys", strings.NewReader(body)))

	var got KeyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("the panel got something that is not JSON: %s", rec.Body.String())
	}
	return got
}

// The page has to say plainly which half is which, so the firewall plan and the
// commands come down with the audit rather than being assembled in the browser.
func TestHandleSecurity_SendsThePlanAndWhatIsActuallyListening(t *testing.T) {
	securityEnv(t)
	rec := httptest.NewRecorder()
	handleSecurity(rec, httptest.NewRequest(http.MethodGet, "/api/security", nil))

	var got SecurityResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Firewall.Commands) == 0 {
		t.Error("no firewall plan")
	}
	// SSH first, every time. Any other order can end the session running it.
	if !strings.Contains(got.Firewall.Commands[0], "22/tcp") {
		t.Errorf("the plan starts with %q rather than allowing SSH", got.Firewall.Commands[0])
	}
	if got.SSHPort != 22 {
		t.Errorf("the plan was built for port %d without saying so", got.SSHPort)
	}
	if len(got.Findings) == 0 {
		t.Error("the audit found nothing at all, not even a check that passed")
	}
}

// Lists the panel renders are lists, not null, or a card ends up showing
// "undefined" the first time a server has no keys.
func TestHandleSecurity_EmptyListsAreListsNotNull(t *testing.T) {
	securityEnv(t)
	rec := httptest.NewRecorder()
	handleSecurity(rec, httptest.NewRequest(http.MethodGet, "/api/security", nil))

	if !strings.Contains(rec.Body.String(), `"keys":[]`) {
		t.Errorf("an empty key list serialised as null:\n%s", rec.Body.String())
	}
	// The port lists depend on what the machine running the test happens to
	// have open, so this checks the shape rather than the contents: neither may
	// come back null.
	var got SecurityResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Firewall.Open == nil || got.Firewall.Unexpected == nil {
		t.Errorf("a port list came back null: %+v", got.Firewall)
	}
}

func TestHandleSecurityKeys_AuthorisesAndRemoves(t *testing.T) {
	dir := securityEnv(t)

	got := postKey(t, `{"action":"add","key":"`+demoKey+`","name":"alex"}`)
	if !got.OK || len(got.Keys) != 1 {
		t.Fatalf("adding a key returned %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".ssh", "authorized_keys")); err != nil {
		t.Fatalf("nothing was written: %v", err)
	}

	fingerprint := got.Keys[0].Fingerprint
	got = postKey(t, `{"action":"remove","fingerprint":"`+fingerprint+`"}`)
	if !got.OK || len(got.Keys) != 0 {
		t.Fatalf("removing the key returned %+v", got)
	}
}

// The reason a key was refused is the whole value of the error: somebody
// pasting a private key does not know they have done it.
func TestHandleSecurityKeys_ExplainsWhyAKeyWasRefused(t *testing.T) {
	securityEnv(t)

	got := postKey(t, `{"action":"add","key":"-----BEGIN OPENSSH PRIVATE KEY-----","name":"alex"}`)
	if got.OK {
		t.Fatal("a private key was authorised")
	}
	if !strings.Contains(got.Error, "private") {
		t.Errorf("the error does not say what is wrong: %q", got.Error)
	}
	// And the list still comes back, so the panel does not blank its own table
	// because one paste was wrong.
	if got.Keys == nil {
		t.Error("the key list came back null after a refused add")
	}
}

// Removing a key that is not there is reported rather than answered with a
// cheerful ok for work that did not happen.
func TestHandleSecurityKeys_SaysSoWhenThereIsNothingToRemove(t *testing.T) {
	securityEnv(t)

	got := postKey(t, `{"action":"remove","fingerprint":"SHA256:nothing"}`)
	if got.OK || got.Error == "" {
		t.Errorf("removing a key that is not there returned %+v", got)
	}
}

func TestHandleSecurityKeys_RefusesAnUnknownAction(t *testing.T) {
	securityEnv(t)
	if got := postKey(t, `{"action":"replace-everything"}`); got.OK || got.Error == "" {
		t.Errorf("an unknown action returned %+v", got)
	}
}
