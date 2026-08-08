package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

func TestLANStatusReportsExposure(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := &config.GlobalConfig{}
	cfg.LAN.Exposed = true
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/lan/status", nil)
	rec := httptest.NewRecorder()
	handleLANStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["exposed"] != true {
		t.Fatalf("response = %+v, want exposed", body)
	}
	// The dashboard reads this to decide what to show. A field claiming a
	// service is reachable off the machine would be reporting something that
	// can no longer happen.
	for _, gone := range []string{"services_enabled", "services_reachable"} {
		if _, present := body[gone]; present {
			t.Errorf("response still carries %q: %+v", gone, body)
		}
	}
}

// The action still has to be one this route knows, whoever is asking. What is
// gone is the second question underneath it, which used to turn a remote
// caller away before it got this far.
func TestLANStatusValidatesTheActionForARemoteCaller(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	req := httptest.NewRequest(http.MethodPost, "/api/lan/status", strings.NewReader(`{"action":"sideways"}`))
	req.RemoteAddr = "203.0.113.10:12345"
	rec := httptest.NewRecorder()
	handleLANStatus(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// Where the request came from is not something the panel reports any more,
// because nothing is decided by it. A dashboard still reading the old field
// would hide half of itself from the operator who owns the machine.
func TestAccessModeNoLongerReportsLocalControl(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/api/access-mode", nil)
	rec := httptest.NewRecorder()
	handleAccessMode(rec, req)

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, present := body["local_control"]; present {
		t.Errorf("response still carries local_control: %+v", body)
	}
}
