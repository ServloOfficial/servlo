package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

func tlsTestSite(t *testing.T, domains ...string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	// Declare the server's address rather than reading whatever the machine
	// running the tests happens to have. A CI runner has only private
	// addresses, and reading them would make the check bail before it looked at
	// a single domain, so the assertions below would pass or fail on the
	// network rather than on the handler.
	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	cfg.Certs.ServerAddresses = []string{"5.6.7.8"}
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	site := config.Site{Name: "myapp", Path: t.TempDir(), Domains: domains}
	if err := config.AddSite(site); err != nil {
		t.Fatalf("AddSite: %v", err)
	}
}

func getTLSStatus(t *testing.T, domain string) TLSStatus {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sites/"+domain+"/tls", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	if !tlsRoute(rec, req, domain, []string{"tls"}) {
		t.Fatal("the tls route did not claim its own path")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out TLSStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding the response: %v (%s)", err, rec.Body.String())
	}
	return out
}

// The panel needs the mismatch before the click, not after: that is the whole
// difference between a disabled button with an explanation and five spent
// validation attempts.
func TestTLSStatus_ReportsTheMismatchBeforeAnythingIsIssued(t *testing.T) {
	tlsTestSite(t, "example.invalid")

	got := getTLSStatus(t, "example.invalid")
	if got.Ready {
		t.Fatal("a domain that cannot resolve was reported ready")
	}
	if got.Message == "" {
		t.Error("an unready status carries no explanation")
	}
	if got.Issuer == "" {
		t.Error("the status does not name the authority, so an operator on staging has no warning")
	}
	if len(got.Domains) != 1 || got.Domains[0].Domain != "example.invalid" {
		t.Errorf("per-domain detail = %+v, want the site's one domain", got.Domains)
	}
}

// Every alias is checked, not just the primary: the certificate covers them
// all, so one stale record fails the whole order.
func TestTLSStatus_CoversEveryAlias(t *testing.T) {
	tlsTestSite(t, "example.invalid", "www.example.invalid", "old.example.invalid")

	got := getTLSStatus(t, "example.invalid")
	if len(got.Domains) != 3 {
		t.Fatalf("checked %d domains, want all three", len(got.Domains))
	}
	for _, d := range got.Domains {
		if !strings.HasSuffix(d.Domain, "example.invalid") {
			t.Errorf("unexpected domain in the report: %q", d.Domain)
		}
	}
}

// A live DNS lookup on caller-chosen names is not a free read, and it gates a
// state-changing action, so it sits behind the same authority as that action.
func TestTLSStatus_RequiresHostActionAuthority(t *testing.T) {
	tlsTestSite(t, "example.invalid")

	rec := httptest.NewRecorder()
	// A remote peer with no full-access grant: the same caller the other
	// host-reaching routes turn away.
	req := httptest.NewRequest(http.MethodGet, "/api/sites/example.invalid/tls", nil)
	req.RemoteAddr = "192.168.1.42:54321"
	if !tlsRoute(rec, req, "example.invalid", []string{"tls"}) {
		t.Fatal("the tls route did not claim its own path")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403 for a caller without host action authority", rec.Code)
	}
}

// Only GET. A POST here would read as "issue now" and there is already a route
// for that.
func TestTLSStatus_RefusesOtherMethods(t *testing.T) {
	tlsTestSite(t, "example.invalid")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sites/example.invalid/tls", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	if !tlsRoute(rec, req, "example.invalid", []string{"tls"}) {
		t.Fatal("the tls route did not claim its own path")
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404 for a POST", rec.Code)
	}
}

// The banner has to distinguish "renewal is failing" from "the certificate on
// disk is expiring". They have different causes and different fixes, and a
// machine restored from a backup can have the second with no record of the
// first.
func TestCertAlerts_SeparatesFailuresFromExpiry(t *testing.T) {
	tlsTestSite(t, "example.invalid")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/certs/alerts", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	handleCertAlerts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var alerts CertAlerts
	if err := json.Unmarshal(rec.Body.Bytes(), &alerts); err != nil {
		t.Fatalf("decoding: %v (%s)", err, rec.Body.String())
	}
	// The site is registered but not secured, so nothing is wrong yet.
	if alerts.Any() {
		t.Errorf("an unsecured site raised a certificate alert: %+v", alerts)
	}
	// Both lists must serialise as arrays rather than null, or the panel has to
	// guard every iteration.
	if !strings.Contains(rec.Body.String(), `"failures":[]`) {
		t.Errorf("failures serialised as null rather than an empty list: %s", rec.Body.String())
	}
}

func TestCertAlerts_RefusesOtherMethods(t *testing.T) {
	tlsTestSite(t, "example.invalid")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/certs/alerts", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	handleCertAlerts(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404 for a POST", rec.Code)
	}
}
