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

func TestLANStatusRejectsUnauthenticatedRemoteToggle(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/lan/status", strings.NewReader(`{"action":"expose"}`))
	req.RemoteAddr = "192.0.2.10:12345"
	rec := httptest.NewRecorder()
	handleLANStatus(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestLANStatusRejectsUnauthenticatedLoopbackReverseProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/lan/status", strings.NewReader(`{"action":"expose"}`))
	req.RemoteAddr = "127.0.0.1:54321"
	req.Host = "robotbox.example.net"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	rec := httptest.NewRecorder()
	handleLANStatus(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestAccessModeRejectsUnauthenticatedLoopbackReverseProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/access-mode", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Host = "robotbox.example.net"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	rec := httptest.NewRecorder()
	handleAccessMode(rec, req)

	var body struct {
		LocalControl bool `json:"local_control"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.LocalControl {
		t.Fatalf("response = %+v, reverse proxy must not grant local control", body)
	}
}
