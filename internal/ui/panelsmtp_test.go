package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

func panelSMTPCall(t *testing.T, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	if path == "/api/settings/smtp/test" {
		handlePanelSMTPTest(rec, req)
	} else {
		handlePanelSMTP(rec, req)
	}
	out := map[string]any{}
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decoding %q: %v", rec.Body.String(), err)
		}
	}
	return rec, out
}

func panelSMTPHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

func TestPanelSMTP_reportsAnUnconfiguredPanel(t *testing.T) {
	panelSMTPHome(t)

	_, body := panelSMTPCall(t, http.MethodGet, "/api/settings/smtp", "")
	if body["configured"] != false {
		t.Errorf("configured = %v, want false", body["configured"])
	}
}

func TestPanelSMTP_savesAndNeverReturnsThePassword(t *testing.T) {
	panelSMTPHome(t)

	rec, _ := panelSMTPCall(t, http.MethodPost, "/api/settings/smtp",
		`{"host":"smtp.panel.example","port":587,"username":"ops","password":"hunter2",
		  "encryption":"starttls","from_address":"alerts@panel.example","from_name":"Servlo"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	rec, body := panelSMTPCall(t, http.MethodGet, "/api/settings/smtp", "")
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("the response carries the password: %s", rec.Body.String())
	}
	if body["configured"] != true {
		t.Errorf("configured = %v, want true", body["configured"])
	}
	settings, _ := body["settings"].(map[string]any)
	if settings["has_password"] != true {
		t.Error("the form cannot tell a password is stored")
	}
	if settings["from_address"] != "alerts@panel.example" {
		t.Errorf("from_address = %v", settings["from_address"])
	}
}

func TestPanelSMTP_refusesSettingsThatWouldNotSend(t *testing.T) {
	panelSMTPHome(t)

	rec, _ := panelSMTPCall(t, http.MethodPost, "/api/settings/smtp",
		`{"host":"smtp.panel.example","port":587,"encryption":"starttls","from_address":"not-an-address"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if _, ok, _ := config.PanelSMTP(); ok {
		t.Error("the refused settings were stored anyway")
	}
}

func TestPanelSMTP_deleteForgetsTheAccount(t *testing.T) {
	panelSMTPHome(t)
	panelSMTPCall(t, http.MethodPost, "/api/settings/smtp",
		`{"host":"h","port":587,"password":"hunter2","encryption":"starttls","from_address":"a@b.test"}`)

	rec, _ := panelSMTPCall(t, http.MethodDelete, "/api/settings/smtp", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if _, ok, _ := config.PanelSMTP(); ok {
		t.Error("the account survived its deletion")
	}
}

// Testing an account that does not exist is the mistake worth catching early:
// it would otherwise dial an empty hostname and report a confusing DNS error.
func TestPanelSMTPTest_refusesWithoutAnAccount(t *testing.T) {
	panelSMTPHome(t)

	rec, body := panelSMTPCall(t, http.MethodPost, "/api/settings/smtp/test", `{"to":"ops@acme.test"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	msg, _ := body["error"].(string)
	if !strings.Contains(msg, "no SMTP settings") {
		t.Errorf("error %q does not say the panel has no account", msg)
	}
}

// The alert path addresses the panel's own sender address, so a test with no
// recipient goes to the same place a real alert would.
func TestPanelSMTPTest_defaultsToTheSenderAddress(t *testing.T) {
	panelSMTPHome(t)
	host, port := refusingSMTP(t)
	panelSMTPCall(t, http.MethodPost, "/api/settings/smtp", fmt.Sprintf(
		`{"host":%q,"port":%d,"encryption":"none","from_address":"alerts@panel.example"}`, host, port))

	rec, body := panelSMTPCall(t, http.MethodPost, "/api/settings/smtp/test", `{}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("the refusing server reported success: %s", rec.Body.String())
	}
	// The server refuses the sender, which proves the exchange got as far as
	// naming it, and the reply comes back word for word.
	msg, _ := body["error"].(string)
	if !strings.Contains(msg, "alerts@panel.example") || !strings.Contains(msg, "Sender address not verified") {
		t.Errorf("error %q does not carry the sender and the server's reply", msg)
	}
}
