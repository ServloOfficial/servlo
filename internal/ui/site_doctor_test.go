package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDoctorFixRun_RejectsUnknownKey(t *testing.T) {
	registerSite(t, "acme", "acme.test")
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/doctor/fix/rm-rf/run", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if !strings.Contains(rec.Body.String(), "unknown doctor fix") {
		t.Errorf("expected unknown-fix error, got %q", rec.Body.String())
	}
}

func TestDoctorFixRun_StreamsAllowlistedCommand(t *testing.T) {
	registerSite(t, "acme", "acme.test")
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/doctor/fix/composer_install/run", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	// composer isn't on PATH in the test env, so the run exits non-zero, but it
	// must still stream a done frame rather than erroring out the endpoint.
	if !strings.Contains(rec.Body.String(), "event: done") {
		t.Errorf("expected a done event from the streamed fix, got %q", rec.Body.String())
	}
}

// Doctor is what an operator opens when a site is misbehaving, and they open it
// from a browser rather than from the droplet. Its fixes belong to the site,
// and the site permission is the gate.
func TestDoctorFixRun_RunsForARemoteCaller(t *testing.T) {
	registerSite(t, "acme", "acme.test")
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/doctor/fix/composer_install/run", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if !strings.Contains(rec.Body.String(), "event: done") {
		t.Errorf("the fix did not run for a remote caller: %d %s", rec.Code, rec.Body.String())
	}
}
