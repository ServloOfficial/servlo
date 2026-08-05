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

func TestDoctorFixRun_NonLoopbackForbidden(t *testing.T) {
	registerSite(t, "acme", "acme.test")
	req := httptest.NewRequest(http.MethodPost, "/api/sites/acme.test/doctor/fix/composer_install/run", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	rec := httptest.NewRecorder()
	handleSiteAction(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("non-loopback fix should be forbidden, got %d", rec.Code)
	}
}
